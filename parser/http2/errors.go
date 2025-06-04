package http2

import "fmt"

// ErrCode HTTP/2错误码
type ErrCode uint32

const (
	ErrCodeNo                 ErrCode = 0x0
	ErrCodeProtocol           ErrCode = 0x1
	ErrCodeInternal           ErrCode = 0x2
	ErrCodeFlowControl        ErrCode = 0x3
	ErrCodeSettingsTimeout    ErrCode = 0x4
	ErrCodeStreamClosed       ErrCode = 0x5
	ErrCodeFrameSize          ErrCode = 0x6
	ErrCodeRefusedStream      ErrCode = 0x7
	ErrCodeCancel             ErrCode = 0x8
	ErrCodeCompression        ErrCode = 0x9
	ErrCodeConnect            ErrCode = 0xa
	ErrCodeEnhanceYourCalm    ErrCode = 0xb
	ErrCodeInadequateSecurity ErrCode = 0xc
	ErrCodeHTTP11Required     ErrCode = 0xd
)

// String 返回错误码的字符串表示
func (e ErrCode) String() string {
	switch e {
	case ErrCodeNo:
		return "NO_ERROR"
	case ErrCodeProtocol:
		return "PROTOCOL_ERROR"
	case ErrCodeInternal:
		return "INTERNAL_ERROR"
	case ErrCodeFlowControl:
		return "FLOW_CONTROL_ERROR"
	case ErrCodeSettingsTimeout:
		return "SETTINGS_TIMEOUT"
	case ErrCodeStreamClosed:
		return "STREAM_CLOSED"
	case ErrCodeFrameSize:
		return "FRAME_SIZE_ERROR"
	case ErrCodeRefusedStream:
		return "REFUSED_STREAM"
	case ErrCodeCancel:
		return "CANCEL"
	case ErrCodeCompression:
		return "COMPRESSION_ERROR"
	case ErrCodeConnect:
		return "CONNECT_ERROR"
	case ErrCodeEnhanceYourCalm:
		return "ENHANCE_YOUR_CALM"
	case ErrCodeInadequateSecurity:
		return "INADEQUATE_SECURITY"
	case ErrCodeHTTP11Required:
		return "HTTP_1_1_REQUIRED"
	default:
		return fmt.Sprintf("UNKNOWN_ERROR(%d)", uint32(e))
	}
}

// ConnectionError 连接级错误
type ConnectionError struct {
	Code ErrCode
	Reason string
}

func (e ConnectionError) Error() string {
	return fmt.Sprintf("connection error: %s: %s", e.Code, e.Reason)
}

// StreamError 流级错误
type StreamError struct {
	StreamID uint32
	Code     ErrCode
	Reason   string
}

func (e StreamError) Error() string {
	return fmt.Sprintf("stream error on stream %d: %s: %s", e.StreamID, e.Code, e.Reason)
}