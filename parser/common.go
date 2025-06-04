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
	ParseRequest(connectionID string, data []byte) (*types.HTTPRequest, error)
	ParseResponse(connectionID string, data []byte) (*types.HTTPResponse, error)
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
	data         []byte
	requestData  []byte
	responseData []byte
	sequenceMap  map[uint64][]byte // 序列号 -> 数据
	lastSequence uint64
	missing      []uint64
	reassembled  bool
	lastUpdate   time.Time
	mu           sync.RWMutex
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
func (pp *PacketProcessor) ProcessPacket(connectionID string, data []byte, sequenceNum uint64, direction types.Direction) (*ProcessResult, error) {
	// 获取或创建缓冲区
	buffer := pp.getOrCreateBuffer(connectionID)

	// 添加数据到缓冲区
	if err := buffer.addData(data, sequenceNum); err != nil {
		return nil, err
	}

	// 尝试重组数据
	if err := buffer.reassemble(); err != nil {
		return nil, err
	}

	// 如果数据还不完整，返回等待更多数据
	if !buffer.reassembled {
		return &ProcessResult{
			Status:    StatusIncomplete,
			Direction: direction,
		}, nil
	}

	// 检测HTTP版本
	version := pp.detectHTTPVersion(buffer.data)
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

	if direction == types.DirectionClientToServer {
		// 解析请求
		req, err := parser.ParseRequest(connectionID, buffer.data)
		if err != nil {
			return nil, err
		}
		result.Request = req
	} else {
		// 解析响应
		resp, err := parser.ParseResponse(connectionID, buffer.data)
		if err != nil {
			return nil, err
		}
		result.Response = resp
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
		sequenceMap: make(map[uint64][]byte),
		lastUpdate:  time.Now(),
		data:        make([]byte, 0, size),
	}
}

// getOrCreateBuffer 获取或创建缓冲区
func (pp *PacketProcessor) getOrCreateBuffer(connectionID string) *PacketBuffer {
	buffer, exists := pp.buffers[connectionID]
	if !exists {
		buffer = &PacketBuffer{
			sequenceMap: make(map[uint64][]byte),
			lastUpdate:  time.Now(),
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

// addData 添加数据
func (pb *PacketBuffer) addData(data []byte, sequenceNum uint64) error {
	pb.lastUpdate = time.Now()

	// 检查是否已存在
	if _, exists := pb.sequenceMap[sequenceNum]; exists {
		return nil // 重复数据，忽略
	}

	// 存储数据
	pb.sequenceMap[sequenceNum] = make([]byte, len(data))
	copy(pb.sequenceMap[sequenceNum], data)

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
	// Keep the old behavior for backward compatibility
	pb.data = append(pb.data, data...)
	return nil
}

// GetReassembledData 获取重组后的数据
func (pb *PacketBuffer) GetReassembledData() []byte {
	return pb.data
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

// reassemble 重组数据
func (pb *PacketBuffer) reassemble() error {
	if pb.reassembled {
		return nil
	}

	// 找到最小序列号
	var minSeq uint64 = ^uint64(0)
	for seq := range pb.sequenceMap {
		if seq < minSeq {
			minSeq = seq
		}
	}

	// 按序列号重组数据
	var buffer bytes.Buffer
	currentSeq := minSeq

	for {
		data, exists := pb.sequenceMap[currentSeq]
		if !exists {
			// 缺少数据包
			pb.missing = append(pb.missing, currentSeq)
			return nil // 等待更多数据
		}

		buffer.Write(data)
		currentSeq++

		// 检查是否有更多连续数据
		if _, exists := pb.sequenceMap[currentSeq]; !exists {
			break
		}
	}

	pb.data = buffer.Bytes()
	pb.lastSequence = currentSeq - 1
	pb.reassembled = true

	return nil
}

// 通用解析函数

// ParseHTTPHeaders 解析HTTP头部
func ParseHTTPHeaders(data []byte) (http.Header, int, error) {
	headers := make(http.Header)

	// 查找头部结束位置
	headerEnd := bytes.Index(data, []byte("\r\n\r\n"))
	if headerEnd == -1 {
		return nil, 0, errors.New("incomplete headers")
	}

	// 分割头部行
	headerData := data[:headerEnd]
	lines := bytes.Split(headerData, []byte("\r\n"))

	// 跳过第一行（请求行或状态行）
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if len(line) == 0 {
			continue
		}

		// 查找冒号
		colonIdx := bytes.IndexByte(line, ':')
		if colonIdx == -1 {
			continue
		}

		key := string(bytes.TrimSpace(line[:colonIdx]))
		value := string(bytes.TrimSpace(line[colonIdx+1:]))

		headers.Add(key, value)
	}

	return headers, headerEnd + 4, nil
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
	validMethods := []string{
		"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS",
		"PATCH", "TRACE", "CONNECT",
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

// CleanupBuffers 清理过期缓冲区
func (pp *PacketProcessor) CleanupBuffers(maxAge time.Duration) {
	now := time.Now()
	for connectionID, buffer := range pp.buffers {
		if now.Sub(buffer.lastUpdate) > maxAge {
			delete(pp.buffers, connectionID)
		}
	}
}
