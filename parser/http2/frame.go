package http2

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// FrameType HTTP/2帧类型
type FrameType uint8

const (
	FrameData         FrameType = 0x0
	FrameHeaders      FrameType = 0x1
	FramePriority     FrameType = 0x2
	FrameRSTStream    FrameType = 0x3
	FrameSettings     FrameType = 0x4
	FramePushPromise  FrameType = 0x5
	FramePing         FrameType = 0x6
	FrameGoAway       FrameType = 0x7
	FrameWindowUpdate FrameType = 0x8
	FrameContinuation FrameType = 0x9
)

// FrameFlags HTTP/2帧标志
type FrameFlags uint8

const (
	FlagDataEndStream   FrameFlags = 0x1
	FlagDataPadded      FrameFlags = 0x8
	FlagHeadersEndStream FrameFlags = 0x1
	FlagHeadersEndHeaders FrameFlags = 0x4
	FlagHeadersPadded    FrameFlags = 0x8
	FlagHeadersPriority  FrameFlags = 0x20
	FlagSettingsAck      FrameFlags = 0x1
	FlagPingAck          FrameFlags = 0x1
	FlagContinuationEndHeaders FrameFlags = 0x4
	FlagPushPromiseEndHeaders  FrameFlags = 0x4
	FlagPushPromisePadded      FrameFlags = 0x8
)

// FrameHeader HTTP/2帧头
type FrameHeader struct {
	Length   uint32    // 24位长度
	Type     FrameType // 8位类型
	Flags    FrameFlags // 8位标志
	StreamID uint32    // 31位流ID（最高位保留）
}

// Frame HTTP/2帧接口
type Frame interface {
	Header() FrameHeader
	WriteTo([]byte) (int, error)
}

// DataFrame DATA帧
type DataFrame struct {
	FrameHeader
	Data    []byte
	PadLen  uint8
	Padding []byte
}

// HeadersFrame HEADERS帧
type HeadersFrame struct {
	FrameHeader
	Priority   *PriorityParam
	HeaderBlock []byte
	PadLen     uint8
	Padding    []byte
}

// PriorityFrame PRIORITY帧
type PriorityFrame struct {
	FrameHeader
	PriorityParam
}

// PriorityParam 优先级参数
type PriorityParam struct {
	StreamDep uint32 // 31位流依赖
	Exclusive bool   // 1位独占标志
	Weight    uint8  // 8位权重
}

// RSTStreamFrame RST_STREAM帧
type RSTStreamFrame struct {
	FrameHeader
	ErrCode ErrCode
}

// SettingsFrame SETTINGS帧
type SettingsFrame struct {
	FrameHeader
	Settings []Setting
}

// Setting 设置参数
type Setting struct {
	ID  SettingID
	Val uint32
}

// PushPromiseFrame PUSH_PROMISE帧
type PushPromiseFrame struct {
	FrameHeader
	PromiseID   uint32
	HeaderBlock []byte
	PadLen      uint8
	Padding     []byte
}

// PingFrame PING帧
type PingFrame struct {
	FrameHeader
	Data [8]byte
}

// GoAwayFrame GOAWAY帧
type GoAwayFrame struct {
	FrameHeader
	LastStreamID uint32
	ErrCode      ErrCode
	DebugData    []byte
}

// WindowUpdateFrame WINDOW_UPDATE帧
type WindowUpdateFrame struct {
	FrameHeader
	Increment uint32
}

// ContinuationFrame CONTINUATION帧
type ContinuationFrame struct {
	FrameHeader
	HeaderBlock []byte
}

// ParseFrameHeader 解析帧头
func ParseFrameHeader(data []byte) (FrameHeader, error) {
	if len(data) < 9 {
		return FrameHeader{}, errors.New("frame header too short")
	}

	length := binary.BigEndian.Uint32(data[0:4]) >> 8
	frameType := FrameType(data[3])
	flags := FrameFlags(data[4])
	streamID := binary.BigEndian.Uint32(data[5:9]) & 0x7fffffff

	return FrameHeader{
		Length:   length,
		Type:     frameType,
		Flags:    flags,
		StreamID: streamID,
	}, nil
}

// WriteFrameHeader 写入帧头
func WriteFrameHeader(header FrameHeader, buf []byte) error {
	if len(buf) < 9 {
		return errors.New("buffer too small")
	}

	// 写入长度（24位）
	binary.BigEndian.PutUint32(buf[0:4], header.Length<<8)
	buf[0] = byte(header.Length >> 16)
	buf[1] = byte(header.Length >> 8)
	buf[2] = byte(header.Length)

	// 写入类型和标志
	buf[3] = byte(header.Type)
	buf[4] = byte(header.Flags)

	// 写入流ID（31位）
	binary.BigEndian.PutUint32(buf[5:9], header.StreamID&0x7fffffff)

	return nil
}

// Header 实现Frame接口
func (f *DataFrame) Header() FrameHeader { return f.FrameHeader }
func (f *HeadersFrame) Header() FrameHeader { return f.FrameHeader }
func (f *PriorityFrame) Header() FrameHeader { return f.FrameHeader }
func (f *RSTStreamFrame) Header() FrameHeader { return f.FrameHeader }
func (f *SettingsFrame) Header() FrameHeader { return f.FrameHeader }
func (f *PushPromiseFrame) Header() FrameHeader { return f.FrameHeader }
func (f *PingFrame) Header() FrameHeader { return f.FrameHeader }
func (f *GoAwayFrame) Header() FrameHeader { return f.FrameHeader }
func (f *WindowUpdateFrame) Header() FrameHeader { return f.FrameHeader }
func (f *ContinuationFrame) Header() FrameHeader { return f.FrameHeader }

// String 返回帧类型的字符串表示
func (t FrameType) String() string {
	switch t {
	case FrameData:
		return "DATA"
	case FrameHeaders:
		return "HEADERS"
	case FramePriority:
		return "PRIORITY"
	case FrameRSTStream:
		return "RST_STREAM"
	case FrameSettings:
		return "SETTINGS"
	case FramePushPromise:
		return "PUSH_PROMISE"
	case FramePing:
		return "PING"
	case FrameGoAway:
		return "GOAWAY"
	case FrameWindowUpdate:
		return "WINDOW_UPDATE"
	case FrameContinuation:
		return "CONTINUATION"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", uint8(t))
	}
}