package types

import (
	"net/http"
	"time"
)

// HTTPResponse 表示完整的HTTP响应
type HTTPResponse struct {
	// 基本信息
	StatusCode    int         // 状态码
	Status        string      // 状态文本
	Proto         string      // 协议版本字符串
	ProtoMajor    int         // 主版本号
	ProtoMinor    int         // 次版本号
	Headers       http.Header // 响应头
	Body          []byte      // 响应体
	ContentLength int64       // 内容长度

	// 元数据
	Metadata  *TransactionMetadata // 事务元数据
	Timestamp time.Time            // 响应时间戳
	RawData   []byte               // 原始数据

	// HTTP/2特有字段
	StreamID *uint32     // 流ID（HTTP/2）
	Trailers http.Header // 尾部头（HTTP/2）

	// 解析状态
	Complete   bool                // 是否解析完成
	ParseError error               // 解析错误
	Fragments  []*ResponseFragment // 数据片段

	// 传输编码相关
	TransferEncoding []string     // 传输编码
	Chunked          bool         // 是否分块传输
	Chunks           []*ChunkInfo // 分块信息
}

// ResponseFragment 响应数据片段
type ResponseFragment struct {
	SequenceNum uint64    // 序列号
	Data        []byte    // 片段数据
	Offset      int64     // 在完整响应中的偏移
	Length      int       // 片段长度
	Timestamp   time.Time // 接收时间
	Direction   Direction // 数据方向
	Reassembled bool      // 是否已重组
}

// ChunkInfo 分块传输信息
type ChunkInfo struct {
	Size      int64     // 块大小
	Data      []byte    // 块数据
	Extension string    // 块扩展
	Timestamp time.Time // 接收时间
}

// ResponseBuilder 响应构建器，用于逐步构建完整响应
type ResponseBuilder struct {
	response    *HTTPResponse
	buffer      []byte
	headersDone bool
	bodyLength  int64
	bodyRead    int64
	fragments   map[uint64]*ResponseFragment
	lastSeq     uint64
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
	return &ResponseBuilder{
		response: &HTTPResponse{
			Headers:   make(http.Header),
			Trailers:  make(http.Header),
			Fragments: make([]*ResponseFragment, 0),
			Chunks:    make([]*ChunkInfo, 0),
			Timestamp: time.Now(),
		},
		fragments:  make(map[uint64]*ResponseFragment),
		chunkState: ChunkStateSize,
	}
}

// AddFragment 添加数据片段
func (rb *ResponseBuilder) AddFragment(fragment *ResponseFragment) error {
	// 检查片段是否已存在
	if _, exists := rb.fragments[fragment.SequenceNum]; exists {
		return nil // 重复片段，忽略
	}

	// 存储片段
	rb.fragments[fragment.SequenceNum] = fragment
	rb.response.Fragments = append(rb.response.Fragments, fragment)

	// 尝试重组
	return rb.tryReassemble()
}

// tryReassemble 尝试重组数据
func (rb *ResponseBuilder) tryReassemble() error {
	// 按序列号排序片段
	var sortedFragments []*ResponseFragment
	for seq := rb.lastSeq + 1; ; seq++ {
		frag, exists := rb.fragments[seq]
		if !exists {
			break
		}
		sortedFragments = append(sortedFragments, frag)
		rb.lastSeq = seq
	}

	// 拼接数据
	for _, frag := range sortedFragments {
		rb.buffer = append(rb.buffer, frag.Data...)
		frag.Reassembled = true
	}

	// 尝试解析HTTP响应
	return rb.parseHTTP()
}

// parseHTTP 解析HTTP响应
func (rb *ResponseBuilder) parseHTTP() error {
	if len(rb.buffer) == 0 {
		return nil
	}

	// 如果还没有解析响应头
	if !rb.headersDone {
		// 查找响应头结束标记
		headerEnd := findHeaderEnd(rb.buffer)
		if headerEnd == -1 {
			return nil // 响应头还不完整
		}

		// 解析状态行和响应头
		if err := rb.parseStatusLine(rb.buffer[:headerEnd]); err != nil {
			rb.response.ParseError = err
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
		rb.buffer = rb.buffer[headerEnd+4:] // +4 for \r\n\r\n
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

	rb.response.Proto = string(parts[0])
	if statusCode, err := parseInt(string(parts[1])); err == nil {
		rb.response.StatusCode = statusCode
	} else {
		return err
	}

	// 状态文本
	if len(parts) > 2 {
		rb.response.Status = string(joinBytes(parts[2:], ' '))
	}

	// 解析协议版本
	if rb.response.Proto == "HTTP/1.0" {
		rb.response.ProtoMajor, rb.response.ProtoMinor = 1, 0
	} else if rb.response.Proto == "HTTP/1.1" {
		rb.response.ProtoMajor, rb.response.ProtoMinor = 1, 1
	}

	// 解析响应头
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if len(line) == 0 {
			break
		}

		colonIdx := findByte(line, ':')
		if colonIdx == -1 {
			continue
		}

		key := string(trimSpace(line[:colonIdx]))
		value := string(trimSpace(line[colonIdx+1:]))
		rb.response.Headers.Add(key, value)
	}

	// 获取Content-Length
	if contentLength := rb.response.Headers.Get("Content-Length"); contentLength != "" {
		if length, err := parseInt64(contentLength); err == nil {
			rb.response.ContentLength = length
		}
	}

	return nil
}

// parseRegularBody 解析普通响应体
func (rb *ResponseBuilder) parseRegularBody() error {
	if rb.bodyLength > 0 {
		if int64(len(rb.buffer)) >= rb.bodyLength {
			rb.response.Body = rb.buffer[:rb.bodyLength]
			rb.response.Complete = true
		}
	} else {
		// 没有Content-Length，读取所有可用数据
		// 这种情况下需要依赖连接关闭来确定响应结束
		rb.response.Body = rb.buffer
		// 注意：这里不能直接设置Complete=true，需要外部判断连接状态
	}

	return nil
}

// parseChunkedBody 解析分块传输响应体
func (rb *ResponseBuilder) parseChunkedBody() error {
	for len(rb.buffer) > 0 {
		switch rb.chunkState {
		case ChunkStateSize:
			// 读取块大小
			crlfIdx := findCRLF(rb.buffer)
			if crlfIdx == -1 {
				return nil // 等待更多数据
			}

			sizeLine := rb.buffer[:crlfIdx]
			size, err := parseHexInt64(string(sizeLine))
			if err != nil {
				return err
			}

			rb.chunkSize = size
			rb.chunkRead = 0
			rb.buffer = rb.buffer[crlfIdx+2:]

			if size == 0 {
				// 最后一个块
				rb.chunkState = ChunkStateTrailer
			} else {
				rb.chunkState = ChunkStateData
			}

		case ChunkStateData:
			// 读取块数据
			remaining := rb.chunkSize - rb.chunkRead
			available := int64(len(rb.buffer))

			if available >= remaining {
				// 可以读取完整的块
				chunkData := rb.buffer[:remaining]
				chunk := &ChunkInfo{
					Size:      rb.chunkSize,
					Data:      make([]byte, len(chunkData)),
					Timestamp: time.Now(),
				}
				copy(chunk.Data, chunkData)
				rb.response.Chunks = append(rb.response.Chunks, chunk)
				rb.response.Body = append(rb.response.Body, chunkData...)

				rb.buffer = rb.buffer[remaining:]
				rb.chunkState = ChunkStateCRLF
			} else {
				// 部分数据
				rb.chunkRead += available
				rb.response.Body = append(rb.response.Body, rb.buffer...)
				rb.buffer = rb.buffer[:0]
				return nil
			}

		case ChunkStateCRLF:
			// 读取块结束的CRLF
			if len(rb.buffer) < 2 {
				return nil // 等待更多数据
			}

			rb.buffer = rb.buffer[2:] // 跳过CRLF
			rb.chunkState = ChunkStateSize

		case ChunkStateTrailer:
			// 读取尾部头
			trailerEnd := findHeaderEnd(rb.buffer)
			if trailerEnd == -1 {
				return nil // 等待更多数据
			}

			// 解析尾部头（如果有的话）
			if trailerEnd > 0 {
				trailerLines := splitLines(rb.buffer[:trailerEnd])
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
			rb.chunkState = ChunkStateDone
			return nil

		case ChunkStateDone:
			return nil
		}
	}

	return nil
}

// GetResponse 获取构建的响应
func (rb *ResponseBuilder) GetResponse() *HTTPResponse {
	return rb.response
}

// IsComplete 检查响应是否完整
func (rb *ResponseBuilder) IsComplete() bool {
	return rb.response.Complete
}

// HasError 检查是否有解析错误
func (rb *ResponseBuilder) HasError() bool {
	return rb.response.ParseError != nil
}
