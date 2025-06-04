package state

import (
	"errors"
	"time"
)

// ConnectionState 连接状态
type ConnectionState int

const (
	// HTTP/1.x 连接状态
	ConnStateIdle    ConnectionState = iota // 空闲状态
	ConnStateActive                         // 活跃状态
	ConnStateClosing                        // 正在关闭
	ConnStateClosed                         // 已关闭

	// HTTP/2 连接状态
	ConnStateHTTP2Preface  // HTTP/2前导
	ConnStateHTTP2Settings // HTTP/2设置交换
	ConnStateHTTP2Active   // HTTP/2活跃
	ConnStateHTTP2GoAway   // HTTP/2正在关闭
)

func (s ConnectionState) String() string {
	switch s {
	case ConnStateIdle:
		return "idle"
	case ConnStateActive:
		return "active"
	case ConnStateClosing:
		return "closing"
	case ConnStateClosed:
		return "closed"
	case ConnStateHTTP2Preface:
		return "http2_preface"
	case ConnStateHTTP2Settings:
		return "http2_settings"
	case ConnStateHTTP2Active:
		return "http2_active"
	case ConnStateHTTP2GoAway:
		return "http2_goaway"
	default:
		return "unknown"
	}
}

// TransactionState HTTP事务状态
type TransactionState int

const (
	TxStateIdle             TransactionState = iota // 空闲
	TxStateRequestHeaders                           // 接收请求头
	TxStateRequestBody                              // 接收请求体
	TxStateRequestComplete                          // 请求完成
	TxStateResponseHeaders                          // 接收响应头
	TxStateResponseBody                             // 接收响应体
	TxStateResponseComplete                         // 响应完成
	TxStateComplete                                 // 事务完成
	TxStateError                                    // 错误状态
)

func (s TransactionState) String() string {
	switch s {
	case TxStateIdle:
		return "idle"
	case TxStateRequestHeaders:
		return "request_headers"
	case TxStateRequestBody:
		return "request_body"
	case TxStateRequestComplete:
		return "request_complete"
	case TxStateResponseHeaders:
		return "response_headers"
	case TxStateResponseBody:
		return "response_body"
	case TxStateResponseComplete:
		return "response_complete"
	case TxStateComplete:
		return "complete"
	case TxStateError:
		return "error"
	default:
		return "unknown"
	}
}

// HTTP2StreamState HTTP/2流状态（重新定义以避免循环导入）
type HTTP2StreamState int

const (
	StreamStateIdle HTTP2StreamState = iota
	StreamStateReservedLocal
	StreamStateReservedRemote
	StreamStateOpen
	StreamStateHalfClosedLocal
	StreamStateHalfClosedRemote
	StreamStateClosed
)

func (s HTTP2StreamState) String() string {
	switch s {
	case StreamStateIdle:
		return "idle"
	case StreamStateReservedLocal:
		return "reserved_local"
	case StreamStateReservedRemote:
		return "reserved_remote"
	case StreamStateOpen:
		return "open"
	case StreamStateHalfClosedLocal:
		return "half_closed_local"
	case StreamStateHalfClosedRemote:
		return "half_closed_remote"
	case StreamStateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// StateTransition 状态转换
type StateTransition struct {
	From      interface{} // 源状态
	To        interface{} // 目标状态
	Event     string      // 触发事件
	Timestamp time.Time   // 转换时间
	Data      interface{} // 附加数据
}

// StateMachineEvent 状态机事件
type StateMachineEvent struct {
	Type      string      // 事件类型
	Data      interface{} // 事件数据
	Timestamp time.Time   // 事件时间
	Source    string      // 事件源
}

// 连接状态机事件类型
const (
	EventConnectionEstablished = "connection_established"
	EventConnectionClosed      = "connection_closed"
	EventHTTP2PrefaceReceived  = "http2_preface_received"
	EventHTTP2SettingsReceived = "http2_settings_received"
	EventHTTP2GoAwayReceived   = "http2_goaway_received"
	EventDataReceived          = "data_received"
	EventError                 = "error"
)

// 事务状态机事件类型
const (
	EventRequestStarted   = "request_started"
	EventRequestHeaders   = "request_headers"
	EventRequestBody      = "request_body"
	EventRequestComplete  = "request_complete"
	EventResponseStarted  = "response_started"
	EventResponseHeaders  = "response_headers"
	EventResponseBody     = "response_body"
	EventResponseComplete = "response_complete"
	EventTransactionError = "transaction_error"
)

// HTTP/2流状态机事件类型
const (
	EventStreamCreated      = "stream_created"
	EventStreamHeaders      = "stream_headers"
	EventStreamData         = "stream_data"
	EventStreamEndStream    = "stream_end_stream"
	EventStreamReset        = "stream_reset"
	EventStreamClosed       = "stream_closed"
	EventStreamPriority     = "stream_priority"
	EventStreamWindowUpdate = "stream_window_update"
)

// StateValidator 状态验证器接口
type StateValidator interface {
	ValidateTransition(from, to interface{}, event string) error
}

// ConnectionStateValidator 连接状态验证器
type ConnectionStateValidator struct{}

// NewConnectionStateValidator 创建连接状态验证器
func NewConnectionStateValidator() *ConnectionStateValidator {
	return &ConnectionStateValidator{}
}

func (v *ConnectionStateValidator) ValidateTransition(from, to interface{}, event string) error {
	fromState, ok1 := from.(ConnectionState)
	toState, ok2 := to.(ConnectionState)

	if !ok1 || !ok2 {
		return ErrInvalidStateType
	}

	// 定义有效的状态转换
	validTransitions := map[ConnectionState][]ConnectionState{
		ConnStateIdle: {
			ConnStateActive,
			ConnStateHTTP2Preface,
			ConnStateClosed,
		},
		ConnStateActive: {
			ConnStateClosing,
			ConnStateClosed,
			ConnStateHTTP2Preface,
		},
		ConnStateClosing: {
			ConnStateClosed,
		},
		ConnStateHTTP2Preface: {
			ConnStateHTTP2Settings,
			ConnStateClosed,
		},
		ConnStateHTTP2Settings: {
			ConnStateHTTP2Active,
			ConnStateClosed,
		},
		ConnStateHTTP2Active: {
			ConnStateHTTP2GoAway,
			ConnStateClosed,
		},
		ConnStateHTTP2GoAway: {
			ConnStateClosed,
		},
	}

	validTargets, exists := validTransitions[fromState]
	if !exists {
		return ErrInvalidStateTransition
	}

	for _, validTarget := range validTargets {
		if validTarget == toState {
			return nil
		}
	}

	return ErrInvalidStateTransition
}

// TransactionStateValidator 事务状态验证器
type TransactionStateValidator struct{}

// NewTransactionStateValidator 创建事务状态验证器
func NewTransactionStateValidator() *TransactionStateValidator {
	return &TransactionStateValidator{}
}

func (v *TransactionStateValidator) ValidateTransition(from, to interface{}, event string) error {
	fromState, ok1 := from.(TransactionState)
	toState, ok2 := to.(TransactionState)

	if !ok1 || !ok2 {
		return ErrInvalidStateType
	}

	// 定义有效的状态转换
	validTransitions := map[TransactionState][]TransactionState{
		TxStateIdle: {
			TxStateRequestHeaders,
			TxStateError,
		},
		TxStateRequestHeaders: {
			TxStateRequestBody,
			TxStateRequestComplete,
			TxStateError,
		},
		TxStateRequestBody: {
			TxStateRequestComplete,
			TxStateError,
		},
		TxStateRequestComplete: {
			TxStateResponseHeaders,
			TxStateError,
		},
		TxStateResponseHeaders: {
			TxStateResponseBody,
			TxStateResponseComplete,
			TxStateError,
		},
		TxStateResponseBody: {
			TxStateResponseComplete,
			TxStateError,
		},
		TxStateResponseComplete: {
			TxStateComplete,
			TxStateError,
		},
	}

	validTargets, exists := validTransitions[fromState]
	if !exists {
		return ErrInvalidStateTransition
	}

	for _, validTarget := range validTargets {
		if validTarget == toState {
			return nil
		}
	}

	return ErrInvalidStateTransition
}

// 错误定义
var (
	ErrInvalidStateType       = errors.New("invalid state type")
	ErrInvalidStateTransition = errors.New("invalid state transition")
	ErrStateMachineNotFound   = errors.New("state machine not found")
	ErrEventHandlerNotFound   = errors.New("event handler not found")
)
