package types

import (
	"net/http"
	"time"
)

// HTTPResponse 表示完整的HTTP响应
type HTTPResponse struct {
	HTTPMessage // 嵌入基础消息结构

	// 响应特有字段
	StatusCode int    // 状态码
	Status     string // 状态文本

	// HTTP/2特有字段
	Trailers http.Header // 尾部头（HTTP/2）

	// 传输编码相关
	TransferEncoding []string     // 传输编码
	Chunked          bool         // 是否分块传输
	Chunks           []*ChunkInfo // 分块信息

	// 兼容性字段（保持向后兼容）
	Fragments []*ResponseFragment // 数据片段
}

// ResponseFragment 响应数据片段（为了向后兼容保留）
type ResponseFragment = Fragment

// ChunkInfo 分块传输信息
type ChunkInfo struct {
	Size      int64     // 块大小
	Data      []byte    // 块数据
	Extension string    // 块扩展
	Timestamp time.Time // 接收时间
}

// ResponseBuilder 响应构建器，用于逐步构建完整响应
type ResponseBuilder struct {
	*BaseMessageBuilder
	response    *HTTPResponse
	headersDone bool
	bodyLength  int64
	bodyRead    int64
	chunkState  ChunkState
	chunkSize   int64
	chunkRead   int64
}

// ChunkState 分块传输状态
type ChunkState int

const (
	ChunkStateSize    ChunkState = iota // 读取块大小
	ChunkStateData                      // 读取块数据
	ChunkStateCRLF                      // 读取块结束的CRLF
	ChunkStateTrailer                   // 读取尾部头
	ChunkStateDone                      // 分块传输完成
)

// NewResponseBuilder 创建新的响应构建器
func NewResponseBuilder() *ResponseBuilder {
	baseBuilder := NewBaseMessageBuilder()
	response := &HTTPResponse{
		HTTPMessage: *baseBuilder.GetRawMessage(),
		Trailers:    make(http.Header),
		Fragments:   make([]*ResponseFragment, 0),
		Chunks:      make([]*ChunkInfo, 0),
	}
	return &ResponseBuilder{
		BaseMessageBuilder: baseBuilder,
		response:           response,
		chunkState:         ChunkStateSize,
	}
}

// AddFragment 添加数据片段
func (rb *ResponseBuilder) AddFragment(fragment *ResponseFragment) error {
	// 添加到基础构建器
	if err := rb.BaseMessageBuilder.AddFragment(fragment); err != nil {
		return err
	}

	// 保持向后兼容，添加到响应的片段列表
	rb.response.Fragments = append(rb.response.Fragments, fragment)

	// 尝试解析HTTP
	return rb.parseHTTP()
}



// parseHTTP 解析HTTP响应
func (rb *ResponseBuilder) parseHTTP() error {
	buffer := rb.GetBuffer()
	if len(buffer) == 0 {
		return nil
	}

	// 如果还没有解析响应头
	if !rb.headersDone {
		// 查找响应头结束标记
		headerEnd := findHeaderEnd(buffer)
		if headerEnd == -1 {
			return nil // 响应头还不完整
		}

		// 解析状态行和响应头
		if err := rb.parseStatusLine(buffer[:headerEnd]); err != nil {
			rb.SetError(err)
			return err
		}

		rb.headersDone = true
		rb.bodyLength = rb.response.ContentLength

		// 检查是否是分块传输
		if transferEncoding := rb.response.Headers.Get("Transfer-Encoding"); transferEncoding != "" {
			rb.response.TransferEncoding = []string{transferEncoding}
			if transferEncoding == "chunked" {
				rb.response.Chunked = true
			}
		}

		// 移除已解析的响应头部分
		rb.SetBuffer(buffer[headerEnd+4:]) // +4 for \r\n\r\n
	}

	// 解析响应体
	if rb.response.Chunked {
		return rb.parseChunkedBody()
	} else {
		return rb.parseRegularBody()
	}
}

// parseStatusLine 解析状态行和响应头
func (rb *ResponseBuilder) parseStatusLine(data []byte) error {
	lines := splitLines(data)
	if len(lines) == 0 {
		return ErrInvalidResponse
	}

	// 解析状态行
	parts := splitBySpace(lines[0])
	if len(parts) < 2 {
		return ErrInvalidStatusLine
	}

	// 使用基础构建器解析协议版本
	rb.ParseProtocolVersion(string(parts[0]))

	// 解析状态码
	if statusCode, err := parseInt(string(parts[1])); err == nil {
		rb.response.StatusCode = statusCode
	} else {
		return err
	}

	// 状态文本
	if len(parts) > 2 {
		rb.response.Status = string(joinBytes(parts[2:], ' '))
	}

	// 使用基础构建器解析头部
	rb.ParseHeaders(lines[1:])

	// 同步到响应对象
	baseMsg := rb.GetRawMessage()
	rb.response.Proto = baseMsg.Proto
	rb.response.ProtoMajor = baseMsg.ProtoMajor
	rb.response.ProtoMinor = baseMsg.ProtoMinor
	rb.response.Headers = baseMsg.Headers
	rb.response.ContentLength = baseMsg.ContentLength

	return nil
}

// parseRegularBody 解析普通响应体
func (rb *ResponseBuilder) parseRegularBody() error {
	buffer := rb.GetBuffer()
	if rb.bodyLength > 0 {
		if int64(len(buffer)) >= rb.bodyLength {
			rb.response.Body = buffer[:rb.bodyLength]
			rb.response.Complete = true
			rb.SetComplete(true)
		}
	} else {
		// 没有Content-Length，读取所有可用数据
		// 这种情况下需要依赖连接关闭来确定响应结束
		rb.response.Body = buffer
		// 注意：这里不能直接设置Complete=true，需要外部判断连接状态
	}

	return nil
}

// parseChunkedBody 解析分块传输响应体
func (rb *ResponseBuilder) parseChunkedBody() error {
	buffer := rb.GetBuffer()
	for len(buffer) > 0 {
		switch rb.chunkState {
		case ChunkStateSize:
			// 读取块大小
			crlfIdx := findCRLF(buffer)
			if crlfIdx == -1 {
				return nil // 等待更多数据
			}

			sizeLine := buffer[:crlfIdx]
			size, err := parseHexInt64(string(sizeLine))
			if err != nil {
				return err
			}

			rb.chunkSize = size
			rb.chunkRead = 0
			buffer = buffer[crlfIdx+2:]
			rb.SetBuffer(buffer)

			if size == 0 {
				// 最后一个块
				rb.chunkState = ChunkStateTrailer
			} else {
				rb.chunkState = ChunkStateData
			}

		case ChunkStateData:
			// 读取块数据
			remaining := rb.chunkSize - rb.chunkRead
			available := int64(len(buffer))

			if available >= remaining {
				// 可以读取完整的块
				chunkData := buffer[:remaining]
				chunk := &ChunkInfo{
					Size:      rb.chunkSize,
					Data:      make([]byte, len(chunkData)),
					Timestamp: time.Now(),
				}
				copy(chunk.Data, chunkData)
				rb.response.Chunks = append(rb.response.Chunks, chunk)
				rb.response.Body = append(rb.response.Body, chunkData...)

				buffer = buffer[remaining:]
				rb.SetBuffer(buffer)
				rb.chunkState = ChunkStateCRLF
			} else {
				// 部分数据
				rb.chunkRead += available
				rb.response.Body = append(rb.response.Body, buffer...)
				rb.SetBuffer([]byte{})
				return nil
			}

		case ChunkStateCRLF:
			// 读取块结束的CRLF
			if len(buffer) < 2 {
				return nil // 等待更多数据
			}

			buffer = buffer[2:] // 跳过CRLF
			rb.SetBuffer(buffer)
			rb.chunkState = ChunkStateSize

		case ChunkStateTrailer:
			// 读取尾部头
			trailerEnd := findHeaderEnd(buffer)
			if trailerEnd == -1 {
				return nil // 等待更多数据
			}

			// 解析尾部头（如果有的话）
			if trailerEnd > 0 {
				trailerLines := splitLines(buffer[:trailerEnd])
				for _, line := range trailerLines {
					if len(line) == 0 {
						continue
					}

					colonIdx := findByte(line, ':')
					if colonIdx == -1 {
						continue
					}

					key := string(trimSpace(line[:colonIdx]))
					value := string(trimSpace(line[colonIdx+1:]))
					rb.response.Trailers.Add(key, value)
				}
			}

			rb.response.Complete = true
			rb.SetComplete(true)
			rb.chunkState = ChunkStateDone
			return nil

		case ChunkStateDone:
			return nil
		}
		buffer = rb.GetBuffer()
	}

	return nil
}

// GetResponse 获取构建的响应
func (rb *ResponseBuilder) GetResponse() *HTTPResponse {
	// 同步基础消息状态到响应对象
	rb.response.Complete = rb.BaseMessageBuilder.IsComplete()
	rb.response.ParseError = rb.BaseMessageBuilder.GetError()
	return rb.response
}

// IsComplete 检查响应是否完整
func (rb *ResponseBuilder) IsComplete() bool {
	return rb.BaseMessageBuilder.IsComplete()
}

// HasError 检查是否有错误
func (rb *ResponseBuilder) HasError() bool {
	return rb.BaseMessageBuilder.HasError()
}
