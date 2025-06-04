package types

import (
	"net/http"
	"net/url"
	"time"
)

// HTTPRequest 表示完整的HTTP请求
type HTTPRequest struct {
	// 基本信息
	Method        string      // HTTP方法
	URL           *url.URL    // 请求URL
	Proto         string      // 协议版本字符串
	ProtoMajor    int         // 主版本号
	ProtoMinor    int         // 次版本号
	Headers       http.Header // 请求头
	Body          []byte      // 请求体
	ContentLength int64       // 内容长度

	// 元数据
	Metadata  *TransactionMetadata // 事务元数据
	Timestamp time.Time            // 请求时间戳
	RawData   []byte               // 原始数据

	// HTTP/2特有字段
	StreamID *uint32         // 流ID（HTTP/2）
	Priority *StreamPriority // 流优先级（HTTP/2）

	// 解析状态
	Complete   bool               // 是否解析完成
	ParseError error              // 解析错误
	Fragments  []*RequestFragment // 数据片段
}

// StreamPriority HTTP/2流优先级
type StreamPriority struct {
	StreamDependency uint32 // 依赖的流ID
	Weight           uint8  // 权重 (1-256)
	Exclusive        bool   // 是否独占依赖
}

// RequestFragment 请求数据片段
type RequestFragment struct {
	SequenceNum uint64    // 序列号
	Data        []byte    // 片段数据
	Offset      int64     // 在完整请求中的偏移
	Length      int       // 片段长度
	Timestamp   time.Time // 接收时间
	Direction   Direction // 数据方向
	Reassembled bool      // 是否已重组
}

// RequestBuilder 请求构建器，用于逐步构建完整请求
type RequestBuilder struct {
	request     *HTTPRequest
	buffer      []byte
	headersDone bool
	bodyLength  int64
	bodyRead    int64
	fragments   map[uint64]*RequestFragment
	lastSeq     uint64
}

// NewRequestBuilder 创建新的请求构建器
func NewRequestBuilder() *RequestBuilder {
	return &RequestBuilder{
		request: &HTTPRequest{
			Headers:   make(http.Header),
			Fragments: make([]*RequestFragment, 0),
			Timestamp: time.Now(),
		},
		fragments: make(map[uint64]*RequestFragment),
	}
}

// AddFragment 添加数据片段
func (rb *RequestBuilder) AddFragment(fragment *RequestFragment) error {
	// 检查片段是否已存在
	if _, exists := rb.fragments[fragment.SequenceNum]; exists {
		return nil // 重复片段，忽略
	}

	// 存储片段
	rb.fragments[fragment.SequenceNum] = fragment
	rb.request.Fragments = append(rb.request.Fragments, fragment)

	// 尝试重组
	return rb.tryReassemble()
}

// tryReassemble 尝试重组数据
func (rb *RequestBuilder) tryReassemble() error {
	// 按序列号排序片段
	var sortedFragments []*RequestFragment
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

	// 尝试解析HTTP请求
	return rb.parseHTTP()
}

// parseHTTP 解析HTTP请求
func (rb *RequestBuilder) parseHTTP() error {
	if len(rb.buffer) == 0 {
		return nil
	}

	// 如果还没有解析请求头
	if !rb.headersDone {
		// 查找请求头结束标记
		headerEnd := findHeaderEnd(rb.buffer)
		if headerEnd == -1 {
			return nil // 请求头还不完整
		}

		// 解析请求行和请求头
		if err := rb.parseRequestLine(rb.buffer[:headerEnd]); err != nil {
			rb.request.ParseError = err
			return err
		}

		rb.headersDone = true
		rb.bodyLength = rb.request.ContentLength

		// 移除已解析的请求头部分
		rb.buffer = rb.buffer[headerEnd+4:] // +4 for \r\n\r\n
	}

	// 解析请求体
	if rb.bodyLength > 0 {
		if int64(len(rb.buffer)) >= rb.bodyLength {
			rb.request.Body = rb.buffer[:rb.bodyLength]
			rb.request.Complete = true
		}
	} else {
		// 没有请求体或者是GET请求
		rb.request.Complete = true
	}

	return nil
}

// parseRequestLine 解析请求行和请求头
func (rb *RequestBuilder) parseRequestLine(data []byte) error {
	// 这里应该实现完整的HTTP请求解析逻辑
	// 为了简化，这里只做基本解析
	lines := splitLines(data)
	if len(lines) == 0 {
		return ErrInvalidRequest
	}

	// 解析请求行
	parts := splitBySpace(lines[0])
	if len(parts) != 3 {
		return ErrInvalidRequestLine
	}

	rb.request.Method = string(parts[0])
	var err error
	rb.request.URL, err = url.Parse(string(parts[1]))
	if err != nil {
		return err
	}
	rb.request.Proto = string(parts[2])

	// 解析协议版本
	if rb.request.Proto == "HTTP/1.0" {
		rb.request.ProtoMajor, rb.request.ProtoMinor = 1, 0
	} else if rb.request.Proto == "HTTP/1.1" {
		rb.request.ProtoMajor, rb.request.ProtoMinor = 1, 1
	}

	// 解析请求头
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
		rb.request.Headers.Add(key, value)
	}

	// 获取Content-Length
	if contentLength := rb.request.Headers.Get("Content-Length"); contentLength != "" {
		if length, err := parseInt64(contentLength); err == nil {
			rb.request.ContentLength = length
		}
	}

	return nil
}

// GetRequest 获取构建的请求
func (rb *RequestBuilder) GetRequest() *HTTPRequest {
	return rb.request
}

// IsComplete 检查请求是否完整
func (rb *RequestBuilder) IsComplete() bool {
	return rb.request.Complete
}

// HasError 检查是否有解析错误
func (rb *RequestBuilder) HasError() bool {
	return rb.request.ParseError != nil
}
