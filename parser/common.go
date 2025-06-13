package parser

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danl5/htrack/types"
)

// Parser HTTP解析器接口
type Parser interface {
	ParseRequest(connectionID string, data []byte, packetInfo *types.PacketInfo) ([]*types.HTTPRequest, error)
	ParseResponse(connectionID string, data []byte, packetInfo *types.PacketInfo) ([]*types.HTTPResponse, error)
	DetectVersion(data []byte) types.HTTPVersion
	IsComplete(data []byte) bool
	GetRequiredBytes(data []byte) int
}

// PacketProcessor 数据包处理器
type PacketProcessor struct {
	parsers map[types.HTTPVersion]Parser
	buffers map[string]*PacketBuffer // 连接ID -> 缓冲区
}

// PacketBuffer 数据包缓冲区
type PacketBuffer struct {
	requestData     []byte
	responseData    []byte
	requestSeqMap   map[uint64][]byte // 请求序列号 -> 数据
	responseSeqMap  map[uint64][]byte // 响应序列号 -> 数据
	lastReqSeq      uint64
	lastRespSeq     uint64
	reqMissing      []uint64
	respMissing     []uint64
	reqReassembled  bool
	respReassembled bool
	lastUpdate      time.Time
	mu              sync.RWMutex
}

// NewPacketProcessor 创建新的数据包处理器
func NewPacketProcessor() *PacketProcessor {
	return &PacketProcessor{
		parsers: make(map[types.HTTPVersion]Parser),
		buffers: make(map[string]*PacketBuffer),
	}
}

// RegisterParser 注册解析器
func (pp *PacketProcessor) RegisterParser(version types.HTTPVersion, parser Parser) {
	pp.parsers[version] = parser
}

// ProcessPacket 处理数据包
func (pp *PacketProcessor) ProcessPacket(connectionID string, data []byte, sequenceNum uint64, direction types.Direction, packetInfo *types.PacketInfo) (*ProcessResult, error) {
	// 获取或创建缓冲区
	buffer := pp.getOrCreateBuffer(connectionID)

	// 添加数据到缓冲区
	if err := buffer.addDataWithDirection(data, sequenceNum, direction); err != nil {
		return nil, err
	}

	// 尝试重组数据
	if err := buffer.reassembleByDirection(direction); err != nil {
		return nil, err
	}

	// 检查数据是否完整
	var reassembled bool
	if direction == types.DirectionClientToServer || direction == types.DirectionRequest {
		reassembled = buffer.reqReassembled
	} else {
		reassembled = buffer.respReassembled
	}

	// 如果数据还不完整，返回等待更多数据
	if !reassembled {
		return &ProcessResult{
			Status:    StatusIncomplete,
			Direction: direction,
		}, nil
	}

	// 获取相应方向的数据
	var targetData []byte
	if direction == types.DirectionClientToServer || direction == types.DirectionRequest {
		targetData = buffer.GetRequestData()
	} else {
		targetData = buffer.GetResponseData()
	}

	// 检测HTTP版本
	version := pp.detectHTTPVersion(targetData)
	parser, exists := pp.parsers[version]
	if !exists {
		return nil, errors.New("unsupported HTTP version")
	}

	// 解析HTTP消息
	result := &ProcessResult{
		Version:   version,
		Direction: direction,
		Status:    StatusComplete,
	}

	if direction == types.DirectionClientToServer || direction == types.DirectionRequest {
		// 解析请求
		reqs, err := parser.ParseRequest(connectionID, targetData, packetInfo)
		if err != nil {
			return nil, err
		}
		if len(reqs) > 0 {
			result.Request = reqs[0] // 返回第一个请求以保持兼容性
		}
	} else {
		// 解析响应
		resps, err := parser.ParseResponse(connectionID, targetData, packetInfo)
		if err != nil {
			return nil, err
		}
		if len(resps) > 0 {
			result.Response = resps[0] // 返回第一个响应以保持兼容性
		}
	}

	// 清理缓冲区
	delete(pp.buffers, connectionID)

	return result, nil
}

// ProcessResult 处理结果
type ProcessResult struct {
	Status    ProcessStatus
	Version   types.HTTPVersion
	Direction types.Direction
	Request   *types.HTTPRequest
	Response  *types.HTTPResponse
	Error     error
}

// ProcessStatus 处理状态
type ProcessStatus int

const (
	StatusIncomplete ProcessStatus = iota
	StatusComplete
	StatusError
)

// NewPacketBuffer 创建新的数据包缓冲区
func NewPacketBuffer(size int) *PacketBuffer {
	return &PacketBuffer{
		requestSeqMap:  make(map[uint64][]byte),
		responseSeqMap: make(map[uint64][]byte),
		lastUpdate:     time.Now(),
		requestData:    make([]byte, 0, size/2),
		responseData:   make([]byte, 0, size/2),
	}
}

// getOrCreateBuffer 获取或创建缓冲区
func (pp *PacketProcessor) getOrCreateBuffer(connectionID string) *PacketBuffer {
	buffer, exists := pp.buffers[connectionID]
	if !exists {
		buffer = &PacketBuffer{
			requestSeqMap:  make(map[uint64][]byte),
			responseSeqMap: make(map[uint64][]byte),
			lastUpdate:     time.Now(),
		}
		pp.buffers[connectionID] = buffer
	}
	return buffer
}

// detectHTTPVersion 检测HTTP版本
func (pp *PacketProcessor) detectHTTPVersion(data []byte) types.HTTPVersion {
	if len(data) < 8 {
		return types.HTTP11 // 默认
	}

	// HTTP/2的魔术字符串
	http2Preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	if bytes.HasPrefix(data, http2Preface) {
		return types.HTTP2
	}

	// 查找第一行
	lineEnd := bytes.Index(data, []byte("\r\n"))
	if lineEnd == -1 {
		lineEnd = bytes.Index(data, []byte("\n"))
		if lineEnd == -1 {
			return types.HTTP11
		}
	}

	firstLine := string(data[:lineEnd])

	// 检查HTTP版本
	if strings.Contains(firstLine, "HTTP/1.0") {
		return types.HTTP10
	} else if strings.Contains(firstLine, "HTTP/1.1") {
		return types.HTTP11
	} else if strings.Contains(firstLine, "HTTP/2") {
		return types.HTTP2
	}

	return types.HTTP11 // 默认
}

// PacketBuffer 方法

// addDataWithDirection 按方向添加数据
func (pb *PacketBuffer) addDataWithDirection(data []byte, sequenceNum uint64, direction types.Direction) error {
	pb.lastUpdate = time.Now()

	if direction == types.DirectionClientToServer || direction == types.DirectionRequest {
		// 检查是否已存在
		if _, exists := pb.requestSeqMap[sequenceNum]; exists {
			return nil // 重复数据，忽略
		}
		// 存储请求数据
		pb.requestSeqMap[sequenceNum] = make([]byte, len(data))
		copy(pb.requestSeqMap[sequenceNum], data)
	} else {
		// 检查是否已存在
		if _, exists := pb.responseSeqMap[sequenceNum]; exists {
			return nil // 重复数据，忽略
		}
		// 存储响应数据
		pb.responseSeqMap[sequenceNum] = make([]byte, len(data))
		copy(pb.responseSeqMap[sequenceNum], data)
	}

	return nil
}

// AddPacket 添加数据包（简化版本，直接追加数据）
func (pb *PacketBuffer) AddPacket(data []byte, direction types.Direction) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	pb.lastUpdate = time.Now()
	if direction == types.DirectionRequest {
		pb.requestData = append(pb.requestData, data...)
	} else if direction == types.DirectionResponse {
		pb.responseData = append(pb.responseData, data...)
	}
	return nil
}

// GetReassembledData 获取重组后的数据（向后兼容，返回请求和响应数据的合并）
func (pb *PacketBuffer) GetReassembledData() []byte {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	// 为了向后兼容，返回请求和响应数据的合并
	var combined []byte
	combined = append(combined, pb.requestData...)
	combined = append(combined, pb.responseData...)
	return combined
}

// GetRequestData 获取请求数据
func (pb *PacketBuffer) GetRequestData() []byte {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	return pb.requestData
}

// GetResponseData 获取响应数据
func (pb *PacketBuffer) GetResponseData() []byte {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	return pb.responseData
}

// reassembleByDirection 按方向重组数据
func (pb *PacketBuffer) reassembleByDirection(direction types.Direction) error {
	if direction == types.DirectionClientToServer || direction == types.DirectionRequest {
		if pb.reqReassembled {
			return nil
		}
		return pb.reassembleRequest()
	} else {
		if pb.respReassembled {
			return nil
		}
		return pb.reassembleResponse()
	}
}

// reassembleRequest 重组请求数据
func (pb *PacketBuffer) reassembleRequest() error {
	if len(pb.requestSeqMap) == 0 {
		return nil
	}

	// 找到最小序列号
	var minSeq uint64 = ^uint64(0)
	for seq := range pb.requestSeqMap {
		if seq < minSeq {
			minSeq = seq
		}
	}

	// 按序列号重组数据
	var buffer bytes.Buffer
	currentSeq := minSeq

	for {
		data, exists := pb.requestSeqMap[currentSeq]
		if !exists {
			// 缺少数据包
			pb.reqMissing = append(pb.reqMissing, currentSeq)
			return nil // 等待更多数据
		}

		buffer.Write(data)
		currentSeq++

		// 检查是否有更多连续数据
		if _, exists := pb.requestSeqMap[currentSeq]; !exists {
			break
		}
	}

	pb.requestData = buffer.Bytes()
	pb.lastReqSeq = currentSeq - 1
	pb.reqReassembled = true

	return nil
}

// reassembleResponse 重组响应数据
func (pb *PacketBuffer) reassembleResponse() error {
	if len(pb.responseSeqMap) == 0 {
		return nil
	}

	// 找到最小序列号
	var minSeq uint64 = ^uint64(0)
	for seq := range pb.responseSeqMap {
		if seq < minSeq {
			minSeq = seq
		}
	}

	// 按序列号重组数据
	var buffer bytes.Buffer
	currentSeq := minSeq

	for {
		data, exists := pb.responseSeqMap[currentSeq]
		if !exists {
			// 缺少数据包
			pb.respMissing = append(pb.respMissing, currentSeq)
			return nil // 等待更多数据
		}

		buffer.Write(data)
		currentSeq++

		// 检查是否有更多连续数据
		if _, exists := pb.responseSeqMap[currentSeq]; !exists {
			break
		}
	}

	pb.responseData = buffer.Bytes()
	pb.lastRespSeq = currentSeq - 1
	pb.respReassembled = true

	return nil
}

// 通用解析函数

// ParseHTTPHeaders 解析HTTP头部
func ParseHTTPHeaders(data []byte) (http.Header, int, error) {
	headers := make(http.Header)

	// 查找头部结束位置，支持多种换行符
	headerEnd := findHeaderEnd(data)
	if headerEnd == -1 {
		return nil, 0, errors.New("incomplete headers")
	}

	// 安全检查：限制头部大小
	if headerEnd > 64*1024 { // 64KB头部限制
		return nil, 0, errors.New("headers too large")
	}

	// 分割头部行
	headerData := data[:headerEnd]
	lines := splitHeaderLines(headerData)

	// 安全检查：限制头部字段数量
	if len(lines) > 100 {
		return nil, 0, errors.New("too many header lines")
	}

	headerCount := 0
	// 跳过第一行（请求行或状态行）
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if len(line) == 0 {
			continue
		}

		// 安全检查：限制单个头部字段长度
		if len(line) > 8192 { // 8KB单个头部限制
			return nil, 0, errors.New("header field too long")
		}

		// 查找冒号
		colonIdx := bytes.IndexByte(line, ':')
		if colonIdx == -1 {
			continue
		}

		key := string(bytes.TrimSpace(line[:colonIdx]))
		value := string(bytes.TrimSpace(line[colonIdx+1:]))

		// 验证头部字段名
		if !isValidHeaderName(key) {
			continue
		}

		headers.Add(key, value)
		headerCount++

		// 安全检查：限制实际头部字段数量
		if headerCount > 50 {
			return nil, 0, errors.New("too many valid header fields")
		}
	}

	// 计算头部结束后的偏移量
	headerEndSize := 4 // 默认\r\n\r\n
	if bytes.Contains(data[:headerEnd+4], []byte("\n\n")) && !bytes.Contains(data[:headerEnd+4], []byte("\r\n\r\n")) {
		headerEndSize = 2 // \n\n
	}

	return headers, headerEnd + headerEndSize, nil
}

// ParseRequestLine 解析请求行
func ParseRequestLine(line []byte) (method, path, proto string, err error) {
	parts := bytes.Fields(line)
	if len(parts) != 3 {
		return "", "", "", errors.New("invalid request line")
	}

	method = string(parts[0])
	path = string(parts[1])
	proto = string(parts[2])

	// 验证方法
	if !isValidHTTPMethod(method) {
		return "", "", "", errors.New("invalid HTTP method")
	}

	// 验证协议
	if !strings.HasPrefix(proto, "HTTP/") {
		return "", "", "", errors.New("invalid HTTP protocol")
	}

	return method, path, proto, nil
}

// ParseStatusLine 解析状态行
func ParseStatusLine(line []byte) (proto string, statusCode int, status string, err error) {
	parts := bytes.Fields(line)
	if len(parts) < 2 {
		return "", 0, "", errors.New("invalid status line")
	}

	proto = string(parts[0])
	code, err := strconv.Atoi(string(parts[1]))
	if err != nil {
		return "", 0, "", errors.New("invalid status code")
	}

	statusCode = code

	// 状态文本（可选）
	if len(parts) > 2 {
		statusBytes := bytes.Join(parts[2:], []byte(" "))
		status = string(statusBytes)
	}

	// 验证协议
	if !strings.HasPrefix(proto, "HTTP/") {
		return "", 0, "", errors.New("invalid HTTP protocol")
	}

	// 验证状态码
	if statusCode < 100 || statusCode >= 600 {
		return "", 0, "", errors.New("invalid status code")
	}

	return proto, statusCode, status, nil
}

// isValidHTTPMethod 检查是否是有效的HTTP方法
func isValidHTTPMethod(method string) bool {
	// 标准HTTP方法和常见扩展方法（包括WebDAV）
	validMethods := []string{
		"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS",
		"PATCH", "TRACE", "CONNECT",
		// WebDAV方法
		"PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK",
	}

	for _, valid := range validMethods {
		if method == valid {
			return true
		}
	}
	return false
}

// GetContentLength 获取内容长度
func GetContentLength(headers http.Header) int64 {
	contentLength := headers.Get("Content-Length")
	if contentLength == "" {
		return -1
	}

	length, err := strconv.ParseInt(contentLength, 10, 64)
	if err != nil {
		return -1
	}

	return length
}

// IsChunkedEncoding 检查是否使用分块编码
func IsChunkedEncoding(headers http.Header) bool {
	transferEncoding := headers.Get("Transfer-Encoding")
	return strings.ToLower(transferEncoding) == "chunked"
}

// ParseChunkSize 解析分块大小
func ParseChunkSize(line []byte) (int64, error) {
	// 移除可能的扩展信息
	if idx := bytes.IndexByte(line, ';'); idx != -1 {
		line = line[:idx]
	}

	line = bytes.TrimSpace(line)
	size, err := strconv.ParseInt(string(line), 16, 64)
	if err != nil {
		return 0, errors.New("invalid chunk size")
	}

	return size, nil
}

// ClearRequestData 清理请求数据
func (pb *PacketBuffer) ClearRequestData() {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	
	pb.requestData = nil
	pb.requestSeqMap = make(map[uint64][]byte)
	pb.lastReqSeq = 0
	pb.reqMissing = nil
	pb.reqReassembled = false
}

// ClearResponseData 清理响应数据
func (pb *PacketBuffer) ClearResponseData() {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	
	pb.responseData = nil
	pb.responseSeqMap = make(map[uint64][]byte)
	pb.lastRespSeq = 0
	pb.respMissing = nil
	pb.respReassembled = false
}

// ClearAllData 清理所有数据
func (pb *PacketBuffer) ClearAllData() {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	
	pb.requestData = nil
	pb.responseData = nil
	pb.requestSeqMap = make(map[uint64][]byte)
	pb.responseSeqMap = make(map[uint64][]byte)
	pb.lastReqSeq = 0
	pb.lastRespSeq = 0
	pb.reqMissing = nil
	pb.respMissing = nil
	pb.reqReassembled = false
	pb.respReassembled = false
}

// CleanupBuffers 清理过期缓冲区
func (pp *PacketProcessor) CleanupBuffers(maxAge time.Duration) {
	now := time.Now()
	for connectionID, buffer := range pp.buffers {
		if now.Sub(buffer.lastUpdate) > maxAge {
			delete(pp.buffers, connectionID)
		}
	}
}

// findHeaderEnd 查找头部结束位置，支持多种换行符
func findHeaderEnd(data []byte) int {
	// 首先尝试标准的\r\n\r\n
	if idx := bytes.Index(data, []byte("\r\n\r\n")); idx != -1 {
		return idx
	}
	// 尝试\n\n（某些非标准实现）
	if idx := bytes.Index(data, []byte("\n\n")); idx != -1 {
		return idx
	}
	return -1
}

// splitHeaderLines 分割头部行，支持多种换行符
func splitHeaderLines(data []byte) [][]byte {
	// 首先尝试\r\n分割
	if bytes.Contains(data, []byte("\r\n")) {
		return bytes.Split(data, []byte("\r\n"))
	}
	// 回退到\n分割
	return bytes.Split(data, []byte("\n"))
}

// isValidHeaderName 验证头部字段名是否有效
func isValidHeaderName(name string) bool {
	if len(name) == 0 {
		return false
	}
	// HTTP头部字段名只能包含可见ASCII字符，不能包含分隔符
	for _, c := range []byte(name) {
		if c <= 32 || c >= 127 ||
			c == '(' || c == ')' || c == '<' || c == '>' || c == '@' ||
			c == ',' || c == ';' || c == ':' || c == '\\' || c == '"' ||
			c == '/' || c == '[' || c == ']' || c == '?' || c == '=' ||
			c == '{' || c == '}' || c == ' ' || c == '\t' {
			return false
		}
	}
	return true
}
