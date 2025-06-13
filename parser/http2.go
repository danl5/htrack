package parser

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/danl5/htrack/parser/http2"
	"github.com/danl5/htrack/types"
	"golang.org/x/net/http2/hpack"
)

// HTTP2Parser HTTP/2解析器
type HTTP2Parser struct {
	version        types.HTTPVersion
	connections    map[string]*HTTP2Connection
	connectionsMux sync.RWMutex
	settings       http2.SettingsMap
}

// HTTP2Connection HTTP/2连接状态
type HTTP2Connection struct {
	ID           string
	streams      map[uint32]*HTTP2Stream
	streamsMux   sync.RWMutex
	Settings     http2.SettingsMap
	PeerSettings http2.SettingsMap
	LastStreamID uint32
	GoAway       bool
	hpackDecoder *hpack.Decoder
	hpackMux     sync.Mutex // 保护hpackDecoder的互斥锁
}

// GetConnection 安全地获取连接
func (p *HTTP2Parser) GetConnection(connectionID string) (*HTTP2Connection, bool) {
	p.connectionsMux.RLock()
	defer p.connectionsMux.RUnlock()
	conn, ok := p.connections[connectionID]
	return conn, ok
}

// SetConnection 安全地设置连接
func (p *HTTP2Parser) SetConnection(connectionID string, conn *HTTP2Connection) {
	p.connectionsMux.Lock()
	defer p.connectionsMux.Unlock()
	p.connections[connectionID] = conn
}

// DeleteConnection 安全地删除连接
func (p *HTTP2Parser) DeleteConnection(connectionID string) {
	p.connectionsMux.Lock()
	defer p.connectionsMux.Unlock()
	delete(p.connections, connectionID)
}

// GetStream 安全地获取流
func (c *HTTP2Connection) GetStream(streamID uint32) (*HTTP2Stream, bool) {
	c.streamsMux.RLock()
	defer c.streamsMux.RUnlock()
	stream, ok := c.streams[streamID]
	return stream, ok
}

// SetStream 安全地设置流
func (c *HTTP2Connection) SetStream(streamID uint32, stream *HTTP2Stream) {
	c.streamsMux.Lock()
	defer c.streamsMux.Unlock()
	c.streams[streamID] = stream
}

// DeleteStream 安全地删除流
func (c *HTTP2Connection) DeleteStream(streamID uint32) {
	c.streamsMux.Lock()
	defer c.streamsMux.Unlock()
	delete(c.streams, streamID)
}

// HTTP2Stream HTTP/2流状态
type HTTP2Stream struct {
	ID           uint32
	State        types.StreamState
	Headers      http.Header
	Data         []byte
	EndStream    bool
	EndHeaders   bool
	Request      *types.HTTPRequest
	Response     *types.HTTPResponse
	HeaderBlocks [][]byte
	CreatedAt    time.Time
	// 头部分片重组状态
	HeadersStarted       bool
	AwaitingContinuation bool
	// 数据累积 - 区分请求和响应
	RequestDataFragments  [][]byte
	ResponseDataFragments [][]byte
	RequestDataLength     int64
	ResponseDataLength    int64
	// 流状态标记
	RequestComplete  bool
	ResponseComplete bool
	IsResponse       bool // 标记当前是否在处理响应
}

// NewHTTP2Parser 创建HTTP/2解析器
func NewHTTP2Parser() *HTTP2Parser {
	return &HTTP2Parser{
		version:     types.HTTP2,
		connections: make(map[string]*HTTP2Connection),
		settings:    http2.NewDefaultSettings(),
	}
}

// getOrCreateStream 获取或创建流
func (p *HTTP2Parser) getOrCreateStream(connID string, streamID uint32) *HTTP2Stream {
	conn, exists := p.GetConnection(connID)
	if !exists {
		conn = &HTTP2Connection{
			ID:           connID,
			streams:      make(map[uint32]*HTTP2Stream),
			hpackDecoder: hpack.NewDecoder(4096, nil),
		}
		p.SetConnection(connID, conn)
	}

	stream, exists := conn.GetStream(streamID)
	if !exists {
		stream = &HTTP2Stream{
			ID:        streamID,
			CreatedAt: time.Now(),
			Headers:   make(http.Header),
			// 确保新流的状态正确初始化
			IsResponse:       false,
			RequestComplete:  false,
			ResponseComplete: false,
		}
		conn.SetStream(streamID, stream)
	}

	return stream
}

// ParseRequest 解析HTTP/2请求
func (p *HTTP2Parser) ParseRequest(connectionID string, data []byte, packetInfo *types.PacketInfo) ([]*types.HTTPRequest, error) {
	var requests []*types.HTTPRequest
	offset := 0

	// 检查连接前导
	if len(data) >= 24 && bytes.HasPrefix(data, []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")) {
		offset = 24
		// 跳过HTTP/2前言，不创建PRI请求对象
		// 如果只有连接前导，返回空数组
		if len(data) == 24 {
			return requests, nil
		}
	}

	// 循环解析所有帧
	for offset < len(data) {
		// 检查是否有足够的数据解析帧头
		if len(data[offset:]) < 9 {
			break // 不完整的帧头，等待更多数据
		}

		header, err := http2.ParseFrameHeader(data[offset:])
		if err != nil {
			return requests, fmt.Errorf("failed to parse frame header at offset %d: %w", offset, err)
		}

		// 检查帧长度是否合理（HTTP/2规范限制）
		const maxFrameSize = 1<<24 - 1 // 16MB - 1
		if header.Length > maxFrameSize {
			return requests, fmt.Errorf("frame too large: %d bytes (max %d)", header.Length, maxFrameSize)
		}

		// 安全的整数转换，防止溢出
		if header.Length > math.MaxInt32-uint32(offset)-9 {
			return requests, fmt.Errorf("frame size would cause integer overflow")
		}

		frameEnd := offset + 9 + int(header.Length)
		if frameEnd > len(data) {
			break // 不完整的帧数据，等待更多数据
		}

		frameData := data[offset+9 : frameEnd]
		req, err := p.parseFrame(connectionID, header, frameData, packetInfo)
		if err != nil {
			return requests, fmt.Errorf("failed to parse frame (stream %d, type %d): %w", header.StreamID, header.Type, err)
		}

		if req != nil {
			requests = append(requests, req)
		}

		offset = frameEnd
	}

	return requests, nil
}

// ParseResponse 解析HTTP/2响应 - 统一接口，返回所有解析到的响应
func (p *HTTP2Parser) ParseResponse(connectionID string, data []byte, packetInfo *types.PacketInfo) ([]*types.HTTPResponse, error) {
	var responses []*types.HTTPResponse
	offset := 0

	// 循环解析所有帧
	for offset < len(data) {
		// 检查是否有足够的数据解析帧头
		if len(data[offset:]) < 9 {
			break // 不完整的帧头，等待更多数据
		}

		header, err := http2.ParseFrameHeader(data[offset:])
		if err != nil {
			return responses, fmt.Errorf("failed to parse frame header at offset %d: %w", offset, err)
		}

		// 检查帧长度是否合理（HTTP/2规范限制）
		const maxFrameSize = 1<<24 - 1 // 16MB - 1
		if header.Length > maxFrameSize {
			return responses, fmt.Errorf("frame too large: %d bytes (max %d)", header.Length, maxFrameSize)
		}

		// 安全的整数转换，防止溢出
		if header.Length > math.MaxInt32-uint32(offset)-9 {
			return responses, fmt.Errorf("frame size would cause integer overflow")
		}

		frameEnd := offset + 9 + int(header.Length)
		if frameEnd > len(data) {
			break // 不完整的帧数据，等待更多数据
		}

		frameData := data[offset+9 : frameEnd]
		resp, err := p.parseResponseFrame(connectionID, header, frameData, packetInfo)
		if err != nil {
			return responses, fmt.Errorf("failed to parse response frame (stream %d, type %d): %w", header.StreamID, header.Type, err)
		}

		if resp != nil {
			responses = append(responses, resp)
		}

		offset = frameEnd
	}

	return responses, nil
}

// IsComplete 检查数据是否完整
func (p *HTTP2Parser) IsComplete(data []byte) bool {
	if len(data) < 9 {
		return false
	}

	// 解析帧头
	header, err := http2.ParseFrameHeader(data)
	if err != nil {
		return false
	}

	// 检查是否有完整的帧
	return len(data) >= int(9+header.Length)
}

// GetRequiredBytes 获取所需字节数
func (p *HTTP2Parser) GetRequiredBytes(data []byte) int {
	if len(data) < 9 {
		return 9
	}

	header, err := http2.ParseFrameHeader(data)
	if err != nil {
		return 9
	}

	return int(9 + header.Length)
}

// parseFrame 解析单个帧
func (p *HTTP2Parser) parseFrame(connectionID string, header http2.FrameHeader, data []byte, packetInfo *types.PacketInfo) (*types.HTTPRequest, error) {
	switch header.Type {
	case http2.FrameHeaders:
		return p.parseHeadersFrame(connectionID, header, data, packetInfo)
	case http2.FrameData:
		return p.parseDataFrame(connectionID, header, data, packetInfo)
	case http2.FrameSettings:
		return p.parseSettingsFrame(header, data, packetInfo)
	case http2.FrameContinuation:
		return p.parseContinuationFrame(connectionID, header, data, packetInfo)
	default:
		// 其他帧类型暂时忽略
		return nil, nil
	}
}

// parseHeadersFrame 解析HEADERS帧
func (p *HTTP2Parser) parseHeadersFrame(connectionID string, header http2.FrameHeader, data []byte, packetInfo *types.PacketInfo) (*types.HTTPRequest, error) {
	if header.StreamID == 0 {
		return nil, errors.New("HEADERS frame with stream ID 0")
	}

	// 获取或创建流
	stream := p.getOrCreateStream(connectionID, header.StreamID)

	// 检查流状态
	if stream.AwaitingContinuation {
		return nil, errors.New("received HEADERS frame while awaiting CONTINUATION")
	}

	offset := 0

	// 处理优先级信息
	if header.Flags&http2.FlagHeadersPriority != 0 {
		if len(data) < 5 {
			return nil, errors.New("invalid HEADERS frame with priority")
		}
		offset += 5
	}

	// 处理填充
	if header.Flags&http2.FlagHeadersPadded != 0 {
		if len(data) < 1 {
			return nil, errors.New("invalid padded HEADERS frame")
		}
		padLen := data[0]
		offset += 1
		if int(padLen) >= len(data)-offset {
			return nil, errors.New("invalid padding length")
		}
		data = data[offset : len(data)-int(padLen)]
	} else {
		data = data[offset:]
	}

	// 初始化头部块累积
	stream.HeaderBlocks = [][]byte{data}
	stream.HeadersStarted = true
	stream.EndHeaders = header.Flags&http2.FlagHeadersEndHeaders != 0
	stream.EndStream = header.Flags&http2.FlagHeadersEndStream != 0

	// 如果没有END_HEADERS标志，等待CONTINUATION帧
	if !stream.EndHeaders {
		stream.AwaitingContinuation = true
		return nil, nil // 等待CONTINUATION帧
	}

	// 头部完整，进行解码
	return p.decodeCompleteHeaders(connectionID, stream, packetInfo)
}

// parseDataFrame 解析DATA帧
func (p *HTTP2Parser) parseDataFrame(connectionID string, header http2.FrameHeader, data []byte, packetInfo *types.PacketInfo) (*types.HTTPRequest, error) {
	if header.StreamID == 0 {
		return nil, errors.New("DATA frame with stream ID 0")
	}

	offset := 0

	// 处理填充
	if header.Flags&http2.FlagDataPadded != 0 {
		if len(data) < 1 {
			return nil, errors.New("invalid padded DATA frame")
		}
		padLen := data[0]
		offset += 1
		if int(padLen) >= len(data)-offset {
			return nil, errors.New("invalid padding length")
		}
		data = data[offset : len(data)-int(padLen)]
	} else {
		data = data[offset:]
	}

	// 获取或创建流
	stream := p.getOrCreateStream(connectionID, header.StreamID)

	// 根据流状态累积数据片段
	if len(data) > 0 {
		if stream.IsResponse {
			// 响应数据
			stream.ResponseDataFragments = append(stream.ResponseDataFragments, data)
			stream.ResponseDataLength += int64(len(data))
		} else {
			// 请求数据
			stream.RequestDataFragments = append(stream.RequestDataFragments, data)
			stream.RequestDataLength += int64(len(data))
		}
	}

	// 检查是否结束流
	endStream := header.Flags&http2.FlagDataEndStream != 0

	// 根据当前状态更新完成标记
	if stream.IsResponse {
		stream.ResponseComplete = endStream
	} else {
		stream.RequestComplete = endStream
	}

	// 如果请求阶段结束，构建完整的请求
	if stream.RequestComplete && !stream.IsResponse {
		request, err := p.buildDataRequest(stream, packetInfo)
		// 请求完成后，标记为响应阶段
		if endStream {
			stream.IsResponse = true
		}
		// 只有当buildDataRequest返回非nil请求时才返回
		if request != nil {
			return request, err
		}
	}

	return nil, nil
}

// parseSettingsFrame 解析SETTINGS帧
func (p *HTTP2Parser) parseSettingsFrame(header http2.FrameHeader, data []byte, packetInfo *types.PacketInfo) (*types.HTTPRequest, error) {
	if header.StreamID != 0 {
		return nil, errors.New("SETTINGS frame with non-zero stream ID")
	}

	if header.Flags&http2.FlagSettingsAck != 0 {
		// SETTINGS ACK帧
		if len(data) != 0 {
			return nil, errors.New("SETTINGS ACK frame with payload")
		}
		return nil, nil
	}

	if len(data)%6 != 0 {
		return nil, errors.New("invalid SETTINGS frame length")
	}

	// 解析设置参数
	for i := 0; i < len(data); i += 6 {
		id := http2.SettingID(data[i])<<8 | http2.SettingID(data[i+1])
		val := uint32(data[i+2])<<24 | uint32(data[i+3])<<16 | uint32(data[i+4])<<8 | uint32(data[i+5])
		p.settings.Set(id, val)
	}

	return nil, nil
}

// parseContinuationFrame 解析CONTINUATION帧
func (p *HTTP2Parser) parseContinuationFrame(connectionID string, header http2.FrameHeader, data []byte, packetInfo *types.PacketInfo) (*types.HTTPRequest, error) {
	if header.StreamID == 0 {
		return nil, errors.New("CONTINUATION frame with stream ID 0")
	}

	// 获取流
	stream := p.getOrCreateStream(connectionID, header.StreamID)

	// 检查流状态
	if !stream.AwaitingContinuation {
		return nil, errors.New("received CONTINUATION frame without preceding HEADERS")
	}

	// 累积头部块
	stream.HeaderBlocks = append(stream.HeaderBlocks, data)
	stream.EndHeaders = header.Flags&http2.FlagContinuationEndHeaders != 0

	// 如果还没有END_HEADERS标志，继续等待
	if !stream.EndHeaders {
		return nil, nil // 继续等待更多CONTINUATION帧
	}

	// 头部完整，进行解码
	stream.AwaitingContinuation = false
	return p.decodeCompleteHeaders(connectionID, stream, packetInfo)
}

// decodeCompleteHeaders 解码完整的头部块序列
func (p *HTTP2Parser) decodeCompleteHeaders(connectionID string, stream *HTTP2Stream, packetInfo *types.PacketInfo) (*types.HTTPRequest, error) {
	// 获取连接以访问其hpack decoder
	conn, exists := p.GetConnection(connectionID)
	if !exists {
		return nil, errors.New("connection not found")
	}

	// 合并所有头部块
	var completeHeaderBlock []byte
	for _, block := range stream.HeaderBlocks {
		completeHeaderBlock = append(completeHeaderBlock, block...)
	}

	// 解码头部块 - 使用互斥锁保护hpackDecoder
	conn.hpackMux.Lock()
	headerFields, err := conn.hpackDecoder.DecodeFull(completeHeaderBlock)
	conn.hpackMux.Unlock()
	if err != nil {
		return nil, fmt.Errorf("HPACK decode error: %v", err)
	}

	// 转换为http.Header
	headers := make(http.Header)
	for _, hf := range headerFields {
		headers.Add(hf.Name, hf.Value)
	}

	// 检查是否为响应头部（包含:status伪头部）
	if headers.Get(":status") != "" {
		// 这是响应头部，标记流为响应阶段
		stream.IsResponse = true
		return nil, nil // 响应头部不返回请求对象
	}

	// 构建HTTP请求
	request := &types.HTTPRequest{
		HTTPMessage: types.HTTPMessage{
			Headers:     headers,
			Proto:       "HTTP/2.0",
			ProtoMajor:  2,
			ProtoMinor:  0,
			Timestamp:   time.Now(),
			StreamID:    &stream.ID,
			RawData:     completeHeaderBlock,
			TCPTuple:    packetInfo.TCPTuple,
			PID:         packetInfo.PID,
			ProcessName: packetInfo.ProcessName,
		},
	}

	// 提取伪头部
	if method := headers.Get(":method"); method != "" {
		request.Method = method
		headers.Del(":method")
	}

	if path := headers.Get(":path"); path != "" {
		parsedURL, err := url.Parse(path)
		if err != nil {
			return nil, err
		}
		request.URL = parsedURL
		headers.Del(":path")
	}

	if scheme := headers.Get(":scheme"); scheme != "" {
		if request.URL != nil {
			request.URL.Scheme = scheme
		}
		headers.Del(":scheme")
	}

	if authority := headers.Get(":authority"); authority != "" {
		if request.URL != nil {
			request.URL.Host = authority
		}
		headers.Del(":authority")
	}

	// 检查内容长度
	if contentLengthStr := headers.Get("content-length"); contentLengthStr != "" {
		contentLength, err := strconv.ParseInt(contentLengthStr, 10, 64)
		if err == nil {
			request.ContentLength = contentLength
		}
	}

	// 检查是否结束流
	request.Complete = stream.EndStream

	// 保存到流中
	stream.Request = request
	stream.Headers = headers

	// 对于HTTP/2协议，只有在请求真正完成时才返回请求对象
	// 如果HEADERS帧带有END_STREAM标志，说明没有DATA帧，请求完成
	// 否则需要等待DATA帧来完成请求
	if request.Complete {
		return request, nil
	}

	// 请求未完成，返回nil等待DATA帧
	return nil, nil
}

// parseResponseFrame 解析响应帧
func (p *HTTP2Parser) parseResponseFrame(connectionID string, header http2.FrameHeader, data []byte, packetInfo *types.PacketInfo) (*types.HTTPResponse, error) {
	switch header.Type {
	case http2.FrameHeaders:
		return p.parseResponseHeadersFrame(connectionID, header, data, packetInfo)
	case http2.FrameData:
		return p.parseResponseDataFrame(connectionID, header, data, packetInfo)
	default:
		return nil, nil
	}
}

// parseResponseHeadersFrame 解析响应HEADERS帧（带连接ID）
func (p *HTTP2Parser) parseResponseHeadersFrame(connectionID string, header http2.FrameHeader, data []byte, packetInfo *types.PacketInfo) (*types.HTTPResponse, error) {
	if header.StreamID == 0 {
		return nil, errors.New("response HEADERS frame with stream ID 0")
	}

	offset := 0

	// 处理优先级和填充（与请求相同）
	if header.Flags&http2.FlagHeadersPriority != 0 {
		if len(data) < 5 {
			return nil, errors.New("invalid response HEADERS frame with priority")
		}
		offset += 5
	}

	if header.Flags&http2.FlagHeadersPadded != 0 {
		if len(data) < 1 {
			return nil, errors.New("invalid padded response HEADERS frame")
		}
		padLen := data[0]
		offset += 1
		if int(padLen) >= len(data)-offset {
			return nil, errors.New("invalid padding length")
		}
		data = data[offset : len(data)-int(padLen)]
	} else {
		data = data[offset:]
	}

	// 解码头部块
	// 获取或创建连接
	connection, ok := p.GetConnection(connectionID)
	if !ok {
		// 创建新连接
		connection = &HTTP2Connection{
			ID:           connectionID,
			streams:      make(map[uint32]*HTTP2Stream),
			hpackDecoder: hpack.NewDecoder(4096, nil),
		}
		p.SetConnection(connectionID, connection)
	}
	// 使用互斥锁保护hpackDecoder
	connection.hpackMux.Lock()
	headerFields, err := connection.hpackDecoder.DecodeFull(data)
	connection.hpackMux.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to decode headers: %v", err)
	}

	// 转换为http.Header
	headers := make(http.Header)
	for _, hf := range headerFields {
		headers.Add(hf.Name, hf.Value)
	}

	// 构建HTTP响应
	response := &types.HTTPResponse{
		HTTPMessage: types.HTTPMessage{
			Headers:     headers,
			Proto:       "HTTP/2.0",
			ProtoMajor:  2,
			ProtoMinor:  0,
			Timestamp:   time.Now(),
			StreamID:    &header.StreamID,
			RawData:     data,
			TCPTuple:    packetInfo.TCPTuple,
			PID:         packetInfo.PID,
			ProcessName: packetInfo.ProcessName,
		},
	}

	// 提取状态码
	if status := headers.Get(":status"); status != "" {
		statusCode, err := strconv.Atoi(status)
		if err == nil {
			response.StatusCode = statusCode
			response.Status = fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode))
		}
		headers.Del(":status")
	}

	// 检查内容长度
	if contentLengthStr := headers.Get("content-length"); contentLengthStr != "" {
		contentLength, err := strconv.ParseInt(contentLengthStr, 10, 64)
		if err == nil {
			response.ContentLength = contentLength
		}
	}

	// 检查是否结束流
	response.Complete = header.Flags&http2.FlagHeadersEndStream != 0

	// 获取或创建流，并保存响应对象
	stream := p.getOrCreateStream(connectionID, header.StreamID)
	stream.Response = response

	// 只有在响应完成时才返回响应对象
	if response.Complete {
		return response, nil
	}
	return nil, nil
}

// parseResponseDataFrame 解析响应DATA帧
func (p *HTTP2Parser) parseResponseDataFrame(connectionID string, header http2.FrameHeader, data []byte, packetInfo *types.PacketInfo) (*types.HTTPResponse, error) {
	if header.StreamID == 0 {
		return nil, errors.New("response DATA frame with stream ID 0")
	}

	offset := 0

	// 处理填充
	if header.Flags&http2.FlagDataPadded != 0 {
		if len(data) < 1 {
			return nil, errors.New("invalid padded response DATA frame")
		}
		padLen := data[0]
		offset += 1
		if int(padLen) >= len(data)-offset {
			return nil, errors.New("invalid padding length")
		}
		data = data[offset : len(data)-int(padLen)]
	} else {
		data = data[offset:]
	}

	// 获取或创建流
	stream := p.getOrCreateStream(connectionID, header.StreamID)

	// 累积响应数据片段
	if len(data) > 0 {
		stream.ResponseDataFragments = append(stream.ResponseDataFragments, data)
		stream.ResponseDataLength += int64(len(data))
	}

	// 检查是否结束流
	endStream := header.Flags&http2.FlagDataEndStream != 0
	stream.ResponseComplete = endStream

	// 如果流中已有响应对象（来自HEADERS帧），使用它；否则创建新的响应对象
	var response *types.HTTPResponse
	if stream.Response != nil {
		// 使用已有的响应对象，包含头部信息
		response = stream.Response
		// 更新数据相关字段
		response.Body = data
		response.Complete = endStream
		response.RawData = data
	} else {
		// 创建新的响应对象（仅包含数据）
		response = &types.HTTPResponse{
			HTTPMessage: types.HTTPMessage{
				Body:        data,
				Timestamp:   time.Now(),
				StreamID:    &header.StreamID,
				Complete:    endStream,
				RawData:     data,
				TCPTuple:    packetInfo.TCPTuple,
				PID:         packetInfo.PID,
				ProcessName: packetInfo.ProcessName,
			},
		}
		stream.Response = response
	}

	// 如果流结束，合并所有响应数据片段
	if endStream && len(stream.ResponseDataFragments) > 1 {
		// 计算总长度
		totalLen := 0
		for _, fragment := range stream.ResponseDataFragments {
			totalLen += len(fragment)
		}

		// 合并所有片段
		combinedData := make([]byte, 0, totalLen)
		for _, fragment := range stream.ResponseDataFragments {
			combinedData = append(combinedData, fragment...)
		}

		// 更新响应体为完整数据
		response.Body = combinedData
		response.RawData = combinedData
	}

	// 只有在响应完成时才返回响应对象
	if response.Complete {
		return response, nil
	}
	return nil, nil
}

func (p *HTTP2Parser) DetectVersion(data []byte) types.HTTPVersion {
	// 1. 检查HTTP/2连接前导（24字节）
	http2Preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	if len(data) >= len(http2Preface) && bytes.Equal(data[:len(http2Preface)], http2Preface) {
		return types.HTTP2
	}

	// 2. 检查HTTP/2帧格式（更严格的验证）
	if len(data) >= 9 {
		return validateHTTP2Frame(data)
	}

	return types.Unknown
}

func validateHTTP2Frame(data []byte) types.HTTPVersion {
	// 解析帧头（9字节）
	length := binary.BigEndian.Uint32(append([]byte{0}, data[:3]...))
	frameType := data[3]
	flags := data[4]
	streamID := binary.BigEndian.Uint32(data[5:9]) & 0x7FFFFFFF // 清除保留位

	// 验证帧长度（RFC 7540: 最大16KB，除非SETTINGS_MAX_FRAME_SIZE修改）
	if length > 16384 {
		return types.Unknown
	}

	// 验证帧类型（RFC 7540定义的帧类型）
	validFrameTypes := map[byte]bool{
		0: true, // DATA
		1: true, // HEADERS
		2: true, // PRIORITY
		3: true, // RST_STREAM
		4: true, // SETTINGS
		5: true, // PUSH_PROMISE
		6: true, // PING
		7: true, // GOAWAY
		8: true, // WINDOW_UPDATE
		9: true, // CONTINUATION
	}

	if !validFrameTypes[frameType] {
		return types.Unknown
	}

	// 验证流ID的合法性
	switch frameType {
	case 4, 6, 7, 8: // SETTINGS, PING, GOAWAY, WINDOW_UPDATE
		if frameType == 4 || frameType == 6 || frameType == 7 {
			if streamID != 0 {
				return types.Unknown // 这些帧必须在连接级别
			}
		}
	case 0, 1, 2, 3, 5, 9: // DATA, HEADERS, PRIORITY, RST_STREAM, PUSH_PROMISE, CONTINUATION
		if streamID == 0 {
			return types.Unknown // 这些帧必须关联到流
		}
	}

	// 验证标志位的合法性
	if !validateFrameFlags(frameType, flags) {
		return types.Unknown
	}

	// 检查是否有完整的帧数据
	if len(data) < int(9+length) {
		return types.Unknown // 数据不完整
	}

	// 检查HTTP/2帧格式
	if len(data) >= 9 {
		header, err := http2.ParseFrameHeader(data)
		if err == nil && validateFrameLength(header) == nil {
			return types.HTTP2
		}
	}

	return types.HTTP2
}

// buildDataRequest 构建包含累积数据的请求
func (p *HTTP2Parser) buildDataRequest(stream *HTTP2Stream, packetInfo *types.PacketInfo) (*types.HTTPRequest, error) {
	// 合并请求阶段的数据片段
	var completeData []byte
	for _, fragment := range stream.RequestDataFragments {
		completeData = append(completeData, fragment...)
	}

	// 如果流已有请求（来自HEADERS），更新其数据
	if stream.Request != nil {
		stream.Request.Body = completeData
		stream.Request.Complete = stream.RequestComplete
		stream.Request.RawData = completeData
		// 只有在请求完成时才返回，避免重复返回未完成的请求
		if stream.RequestComplete {
			return stream.Request, nil
		}
		return nil, nil
	}

	// 否则创建新的数据请求（仅数据帧的情况）
	request := &types.HTTPRequest{
		HTTPMessage: types.HTTPMessage{
			Body:        completeData,
			Timestamp:   time.Now(),
			StreamID:    &stream.ID,
			Complete:    stream.RequestComplete,
			RawData:     completeData,
			TCPTuple:    packetInfo.TCPTuple,
			PID:         packetInfo.PID,
			ProcessName: packetInfo.ProcessName,
		},
	}

	stream.Request = request
	return request, nil
}

func validateFrameFlags(frameType, flags byte) bool {
	validFlags := map[byte]byte{
		0: 0x01 | 0x08,               // DATA: END_STREAM | PADDED
		1: 0x01 | 0x04 | 0x08 | 0x20, // HEADERS: END_STREAM | END_HEADERS | PADDED | PRIORITY
		2: 0x00,                      // PRIORITY: 无标志
		3: 0x00,                      // RST_STREAM: 无标志
		4: 0x01,                      // SETTINGS: ACK
		5: 0x04 | 0x08,               // PUSH_PROMISE: END_HEADERS | PADDED
		6: 0x01,                      // PING: ACK
		7: 0x00,                      // GOAWAY: 无标志
		8: 0x00,                      // WINDOW_UPDATE: 无标志
		9: 0x04,                      // CONTINUATION: END_HEADERS
	}

	validMask, exists := validFlags[frameType]
	if !exists {
		return false
	}

	return (flags & ^validMask) == 0
}

func validateFrameLength(header http2.FrameHeader) error {
	switch header.Type {
	case http2.FrameSettings:
		// SETTINGS帧长度必须是6的倍数
		if header.Length%6 != 0 {
			return errors.New("invalid SETTINGS frame length")
		}
	case http2.FramePing:
		// PING帧必须是8字节
		if header.Length != 8 {
			return errors.New("invalid PING frame length")
		}
	case http2.FrameWindowUpdate:
		// WINDOW_UPDATE帧必须是4字节
		if header.Length != 4 {
			return errors.New("invalid WINDOW_UPDATE frame length")
		}
	case http2.FrameRSTStream:
		// RST_STREAM帧必须是4字节
		if header.Length != 4 {
			return errors.New("invalid RST_STREAM frame length")
		}
	case http2.FramePriority:
		// PRIORITY帧必须是5字节
		if header.Length != 5 {
			return errors.New("invalid PRIORITY frame length")
		}
	case http2.FrameGoAway:
		// GOAWAY帧至少8字节
		if header.Length < 8 {
			return errors.New("invalid GOAWAY frame length")
		}
	}
	return nil
}
