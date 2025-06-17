package types

import (
	"net/http"
	"time"
)

// HTTPMessage 表示HTTP消息的基础结构（请求和响应的共同部分）
type HTTPMessage struct {
	// 协议信息
	Proto      string // 协议版本字符串
	ProtoMajor int    // 主版本号
	ProtoMinor int    // 次版本号

	// 头部和内容
	Headers       http.Header // 头部
	Body          []byte      // 消息体
	ContentLength int64       // 内容长度

	// 元数据
	Metadata  *TransactionMetadata // 事务元数据
	Timestamp time.Time            // 时间戳
	RawData   []byte               // 原始数据

	// 网络信息
	TCPTuple *TCPTuple // TCP四元组信息

	// 进程信息
	PID         uint32 // 进程ID
	TID         uint32 // 线程ID
	ProcessName string // 进程名称

	// HTTP/2特有字段
	StreamID *uint32 // 流ID（HTTP/2）

	// 解析状态
	Complete   bool  // 是否解析完成
	ParseError error // 解析错误
}

// Fragment 表示数据片段的通用结构
type Fragment struct {
	SequenceNum uint64    // 序列号
	Data        []byte    // 片段数据
	Offset      int64     // 在完整消息中的偏移
	Length      int       // 片段长度
	Timestamp   time.Time // 接收时间
	Direction   Direction // 数据方向
	Reassembled bool      // 是否已重组
}

// MessageBuilder 消息构建器接口
type MessageBuilder interface {
	AddFragment(fragment *Fragment) error
	IsComplete() bool
	HasError() bool
	GetRawMessage() *HTTPMessage
}

// BaseMessageBuilder 基础消息构建器
type BaseMessageBuilder struct {
	message   *HTTPMessage
	buffer    []byte
	fragments map[uint64]*Fragment
	lastSeq   uint64
}

// NewBaseMessageBuilder 创建基础消息构建器
func NewBaseMessageBuilder() *BaseMessageBuilder {
	return &BaseMessageBuilder{
		message: &HTTPMessage{
			Headers:   make(http.Header),
			Timestamp: time.Now(),
		},
		fragments: make(map[uint64]*Fragment),
	}
}

// SetProcessInfo 设置进程信息
func (bmb *BaseMessageBuilder) SetProcessInfo(pid uint32, tid uint32, processName string) {
	bmb.message.PID = pid
	bmb.message.TID = tid
	bmb.message.ProcessName = processName
}

// AddFragment 添加数据片段
func (bmb *BaseMessageBuilder) AddFragment(fragment *Fragment) error {
	// 检查片段是否已存在
	if _, exists := bmb.fragments[fragment.SequenceNum]; exists {
		return nil // 重复片段，忽略
	}

	// 存储片段
	bmb.fragments[fragment.SequenceNum] = fragment

	// 尝试重组
	return bmb.tryReassemble()
}

// tryReassemble 尝试重组数据
func (bmb *BaseMessageBuilder) tryReassemble() error {
	// 按序列号排序片段
	var sortedFragments []*Fragment
	for seq := bmb.lastSeq + 1; ; seq++ {
		frag, exists := bmb.fragments[seq]
		if !exists {
			break
		}
		sortedFragments = append(sortedFragments, frag)
		bmb.lastSeq = seq
	}

	// 拼接数据
	for _, frag := range sortedFragments {
		bmb.buffer = append(bmb.buffer, frag.Data...)
		frag.Reassembled = true
	}

	return nil
}

// GetBuffer 获取缓冲区数据
func (bmb *BaseMessageBuilder) GetBuffer() []byte {
	return bmb.buffer
}

// SetBuffer 设置缓冲区数据
func (bmb *BaseMessageBuilder) SetBuffer(data []byte) {
	bmb.buffer = data
}

// GetRawMessage 获取基础消息
func (bmb *BaseMessageBuilder) GetRawMessage() *HTTPMessage {
	return bmb.message
}

// IsComplete 检查消息是否完整
func (bmb *BaseMessageBuilder) IsComplete() bool {
	return bmb.message.Complete
}

// HasError 检查是否有解析错误
func (bmb *BaseMessageBuilder) HasError() bool {
	return bmb.message.ParseError != nil
}

// SetComplete 设置完成状态
func (bmb *BaseMessageBuilder) SetComplete(complete bool) {
	bmb.message.Complete = complete
}

// SetError 设置解析错误
func (bmb *BaseMessageBuilder) SetError(err error) {
	bmb.message.ParseError = err
}

// GetError 获取解析错误
func (bmb *BaseMessageBuilder) GetError() error {
	return bmb.message.ParseError
}

// ParseProtocolVersion 解析协议版本
func (bmb *BaseMessageBuilder) ParseProtocolVersion(proto string) {
	bmb.message.Proto = proto
	if proto == "HTTP/1.0" {
		bmb.message.ProtoMajor, bmb.message.ProtoMinor = 1, 0
	} else if proto == "HTTP/1.1" {
		bmb.message.ProtoMajor, bmb.message.ProtoMinor = 1, 1
	} else if proto == "HTTP/2" || proto == "HTTP/2.0" {
		bmb.message.ProtoMajor, bmb.message.ProtoMinor = 2, 0
	}
}

// ParseHeaders 解析头部
func (bmb *BaseMessageBuilder) ParseHeaders(lines [][]byte) {
	for _, line := range lines {
		if len(line) == 0 {
			break
		}

		colonIdx := findByte(line, ':')
		if colonIdx == -1 {
			continue
		}

		key := string(trimSpace(line[:colonIdx]))
		value := string(trimSpace(line[colonIdx+1:]))
		bmb.message.Headers.Add(key, value)
	}

	// 获取Content-Length
	if contentLength := bmb.message.Headers.Get("Content-Length"); contentLength != "" {
		if length, err := parseInt64(contentLength); err == nil {
			bmb.message.ContentLength = length
		}
	}
}
