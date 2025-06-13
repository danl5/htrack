package types

import (
	"net/url"
)

// HTTPRequest 表示完整的HTTP请求
type HTTPRequest struct {
	HTTPMessage // 嵌入基础消息结构

	// 请求特有字段
	Method   string          // HTTP方法
	URL      *url.URL        // 请求URL
	Priority *StreamPriority // 流优先级（HTTP/2）

	// 兼容性字段（保持向后兼容）
	Fragments []*RequestFragment // 数据片段
}

// StreamPriority HTTP/2流优先级
type StreamPriority struct {
	StreamDependency uint32 // 依赖的流ID
	Weight           uint8  // 权重 (1-256)
	Exclusive        bool   // 是否独占依赖
}

// RequestFragment 请求数据片段（为了向后兼容保留）
type RequestFragment = Fragment

// RequestBuilder 请求构建器，用于逐步构建完整请求
type RequestBuilder struct {
	*BaseMessageBuilder
	request     *HTTPRequest
	headersDone bool
	bodyLength  int64
	bodyRead    int64
}

// NewRequestBuilder 创建新的请求构建器
func NewRequestBuilder() *RequestBuilder {
	baseBuilder := NewBaseMessageBuilder()
	request := &HTTPRequest{
		HTTPMessage: *baseBuilder.GetRawMessage(),
		Fragments:   make([]*RequestFragment, 0),
	}
	return &RequestBuilder{
		BaseMessageBuilder: baseBuilder,
		request:            request,
	}
}

// AddFragment 添加数据片段
func (rb *RequestBuilder) AddFragment(fragment *RequestFragment) error {
	// 添加到基础构建器
	if err := rb.BaseMessageBuilder.AddFragment(fragment); err != nil {
		return err
	}

	// 保持向后兼容，添加到请求的片段列表
	rb.request.Fragments = append(rb.request.Fragments, fragment)

	// 尝试解析HTTP
	return rb.parseHTTP()
}

// parseHTTP 解析HTTP请求
func (rb *RequestBuilder) parseHTTP() error {
	buffer := rb.GetBuffer()
	if len(buffer) == 0 {
		return nil
	}

	// 如果还没有解析请求头
	if !rb.headersDone {
		// 查找请求头结束标记
		headerEnd := findHeaderEnd(buffer)
		if headerEnd == -1 {
			return nil // 请求头还不完整
		}

		// 解析请求行和请求头
		if err := rb.parseRequestLine(buffer[:headerEnd]); err != nil {
			rb.SetError(err)
			return err
		}

		rb.headersDone = true
		rb.bodyLength = rb.request.ContentLength

		// 移除已解析的请求头部分
		rb.SetBuffer(buffer[headerEnd+4:]) // +4 for \r\n\r\n
		buffer = rb.GetBuffer()
	}

	// 解析请求体
	if rb.bodyLength > 0 {
		if int64(len(buffer)) >= rb.bodyLength {
			rb.request.Body = buffer[:rb.bodyLength]
			rb.request.Complete = true
			rb.SetComplete(true)
		}
	} else {
		// 没有请求体或者是GET请求
		rb.request.Complete = true
		rb.SetComplete(true)
	}

	return nil
}

// parseRequestLine 解析请求行和请求头
func (rb *RequestBuilder) parseRequestLine(data []byte) error {
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

	// 使用基础构建器解析协议版本
	rb.ParseProtocolVersion(string(parts[2]))
	// 同步到请求对象
	rb.request.Proto = rb.GetRawMessage().Proto
	rb.request.ProtoMajor = rb.GetRawMessage().ProtoMajor
	rb.request.ProtoMinor = rb.GetRawMessage().ProtoMinor

	// 使用基础构建器解析头部
	rb.ParseHeaders(lines[1:])
	// 同步到请求对象
	rb.request.Headers = rb.GetRawMessage().Headers
	rb.request.ContentLength = rb.GetRawMessage().ContentLength

	return nil
}

// GetRequest 获取构建的请求
func (rb *RequestBuilder) GetRequest() *HTTPRequest {
	// 同步基础消息状态到请求对象
	baseMsg := rb.GetRawMessage()
	rb.request.HTTPMessage = *baseMsg
	return rb.request
}

// IsComplete 检查请求是否完整
func (rb *RequestBuilder) IsComplete() bool {
	return rb.BaseMessageBuilder.IsComplete()
}

// HasError 检查是否有解析错误
func (rb *RequestBuilder) HasError() bool {
	return rb.BaseMessageBuilder.HasError()
}
