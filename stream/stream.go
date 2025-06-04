package stream

import (
	"errors"
	"sync"
	"time"

	"github.com/danl5/htrack/state"
	"github.com/danl5/htrack/types"
)

// Stream 表示HTTP/2流
type Stream struct {
	mu           sync.RWMutex
	id           uint32
	connectionID string
	state        state.HTTP2StreamState
	priority     *types.StreamPriority
	metadata     *types.StreamMetadata
	request      *types.RequestBuilder
	response     *types.ResponseBuilder
	stateMachine state.StateMachine
	createdAt    time.Time
	lastActivity time.Time
	closed       bool
	error        error

	// 流控制
	windowSize       int32
	remoteWindowSize int32

	// 数据缓冲
	headerBuffer []byte
	dataBuffer   []byte
	pendingData  []*StreamData

	// 回调函数
	onStateChange     func(*Stream, state.HTTP2StreamState, state.HTTP2StreamState)
	onDataReceived    func(*Stream, []byte)
	onHeadersReceived func(*Stream, map[string]string)
	onComplete        func(*Stream)
	onError           func(*Stream, error)
}

// StreamData 流数据
type StreamData struct {
	Data      []byte
	EndStream bool
	Timestamp time.Time
	Padding   int
}

// NewStream 创建新的流
func NewStream(id uint32, connectionID string, priority *types.StreamPriority) *Stream {
	stream := &Stream{
		id:               id,
		connectionID:     connectionID,
		state:            state.StreamStateIdle,
		priority:         priority,
		request:          types.NewRequestBuilder(),
		response:         types.NewResponseBuilder(),
		createdAt:        time.Now(),
		lastActivity:     time.Now(),
		windowSize:       65535, // 默认窗口大小
		remoteWindowSize: 65535,
		pendingData:      make([]*StreamData, 0),
	}

	// 创建流元数据
	stream.metadata = &types.StreamMetadata{
		StreamID:  id,
		State:     types.StreamState(stream.state),
		CreatedAt: stream.createdAt,
	}

	if priority != nil {
		stream.metadata.ParentID = priority.StreamDependency
		stream.metadata.Weight = priority.Weight
		stream.metadata.Exclusive = priority.Exclusive
	}

	// 创建状态机
	stream.stateMachine = state.NewGenericStateMachine(
		stream.getStateMachineID(),
		state.StreamStateIdle,
		&StreamStateValidator{},
	)

	// 注册事件处理器
	stream.setupEventHandlers()

	return stream
}

// GetID 获取流ID
func (s *Stream) GetID() uint32 {
	return s.id
}

// GetConnectionID 获取连接ID
func (s *Stream) GetConnectionID() string {
	return s.connectionID
}

// GetState 获取当前状态
func (s *Stream) GetState() state.HTTP2StreamState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// GetMetadata 获取流元数据
func (s *Stream) GetMetadata() *types.StreamMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 更新元数据
	s.metadata.State = types.StreamState(s.state)
	if s.closed && s.metadata.ClosedAt == nil {
		now := time.Now()
		s.metadata.ClosedAt = &now
	}

	return s.metadata
}

// GetRequest 获取请求
func (s *Stream) GetRequest() *types.HTTPRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.request != nil {
		req := s.request.GetRequest()
		req.StreamID = &s.id
		req.Priority = s.priority
		return req
	}
	return nil
}

// GetResponse 获取响应
func (s *Stream) GetResponse() *types.HTTPResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.response != nil {
		resp := s.response.GetResponse()
		resp.StreamID = &s.id
		return resp
	}
	return nil
}

// ProcessHeaders 处理头部帧
func (s *Stream) ProcessHeaders(headers map[string]string, endStream bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastActivity = time.Now()

	// 触发状态机事件
	event := &state.StateMachineEvent{
		Type:      state.EventStreamHeaders,
		Data:      map[string]interface{}{"headers": headers, "end_stream": endStream},
		Timestamp: time.Now(),
		Source:    "stream",
	}

	if err := s.stateMachine.Transition(event); err != nil {
		s.error = err
		if s.onError != nil {
			s.onError(s, err)
		}
		return err
	}

	// 判断是请求头还是响应头
	if s.isRequestHeaders(headers) {
		// 处理请求头
		if err := s.processRequestHeaders(headers); err != nil {
			return err
		}
	} else {
		// 处理响应头
		if err := s.processResponseHeaders(headers); err != nil {
			return err
		}
	}

	// 回调
	if s.onHeadersReceived != nil {
		s.onHeadersReceived(s, headers)
	}

	// 如果设置了end_stream，关闭流
	if endStream {
		return s.closeStream(false)
	}

	return nil
}

// ProcessData 处理数据帧
func (s *Stream) ProcessData(data []byte, endStream bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastActivity = time.Now()

	// 检查流控制
	if int32(len(data)) > s.windowSize {
		return errors.New("flow control violation")
	}

	// 更新窗口大小
	s.windowSize -= int32(len(data))

	// 触发状态机事件
	event := &state.StateMachineEvent{
		Type:      state.EventStreamData,
		Data:      map[string]interface{}{"data": data, "end_stream": endStream},
		Timestamp: time.Now(),
		Source:    "stream",
	}

	if err := s.stateMachine.Transition(event); err != nil {
		s.error = err
		if s.onError != nil {
			s.onError(s, err)
		}
		return err
	}

	// 存储数据
	streamData := &StreamData{
		Data:      make([]byte, len(data)),
		EndStream: endStream,
		Timestamp: time.Now(),
	}
	copy(streamData.Data, data)
	s.pendingData = append(s.pendingData, streamData)

	// 处理数据
	if err := s.processStreamData(data); err != nil {
		return err
	}

	// 回调
	if s.onDataReceived != nil {
		s.onDataReceived(s, data)
	}

	// 如果设置了end_stream，关闭流
	if endStream {
		return s.closeStream(false)
	}

	return nil
}

// Reset 重置流
func (s *Stream) Reset(errorCode uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 触发状态机事件
	event := &state.StateMachineEvent{
		Type:      state.EventStreamReset,
		Data:      errorCode,
		Timestamp: time.Now(),
		Source:    "stream",
	}

	if err := s.stateMachine.Transition(event); err != nil {
		return err
	}

	return s.closeStream(true)
}

// Close 关闭流
func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeStream(false)
}

// IsComplete 检查流是否完成
func (s *Stream) IsComplete() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.state == state.StreamStateClosed ||
		(s.request != nil && s.request.IsComplete() &&
			s.response != nil && s.response.IsComplete())
}

// HasError 检查是否有错误
func (s *Stream) HasError() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.error != nil
}

// GetError 获取错误
func (s *Stream) GetError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.error
}

// UpdateWindowSize 更新窗口大小
func (s *Stream) UpdateWindowSize(increment int32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.windowSize += increment
	if s.windowSize < 0 {
		return errors.New("window size underflow")
	}

	return nil
}

// SetPriority 设置优先级
func (s *Stream) SetPriority(priority *types.StreamPriority) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.priority = priority
	if s.metadata != nil {
		if priority != nil {
			s.metadata.ParentID = priority.StreamDependency
			s.metadata.Weight = priority.Weight
			s.metadata.Exclusive = priority.Exclusive
		}
	}

	return nil
}

// 设置回调函数
func (s *Stream) SetOnStateChange(callback func(*Stream, state.HTTP2StreamState, state.HTTP2StreamState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onStateChange = callback
}

func (s *Stream) SetOnDataReceived(callback func(*Stream, []byte)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDataReceived = callback
}

func (s *Stream) SetOnHeadersReceived(callback func(*Stream, map[string]string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onHeadersReceived = callback
}

func (s *Stream) SetOnComplete(callback func(*Stream)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onComplete = callback
}

func (s *Stream) SetOnError(callback func(*Stream, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onError = callback
}

// 私有方法

// getStateMachineID 获取状态机ID
func (s *Stream) getStateMachineID() string {
	return s.connectionID + "-stream-" + string(rune(s.id))
}

// setupEventHandlers 设置事件处理器
func (s *Stream) setupEventHandlers() {
	// 这里可以添加具体的事件处理逻辑
	// 为了简化，暂时留空
}

// isRequestHeaders 判断是否是请求头
func (s *Stream) isRequestHeaders(headers map[string]string) bool {
	// 检查是否包含请求方法
	_, hasMethod := headers[":method"]
	_, hasPath := headers[":path"]
	return hasMethod && hasPath
}

// processRequestHeaders 处理请求头
func (s *Stream) processRequestHeaders(headers map[string]string) error {
	// 这里应该将HTTP/2头部转换为HTTP/1.1格式
	// 为了简化，暂时留空
	return nil
}

// processResponseHeaders 处理响应头
func (s *Stream) processResponseHeaders(headers map[string]string) error {
	// 这里应该将HTTP/2头部转换为HTTP/1.1格式
	// 为了简化，暂时留空
	return nil
}

// processStreamData 处理流数据
func (s *Stream) processStreamData(data []byte) error {
	// 根据当前状态决定是请求体还是响应体
	if s.state == state.StreamStateOpen || s.state == state.StreamStateHalfClosedRemote {
		// 可能是请求体数据
		if s.request != nil {
			// 创建请求片段
			fragment := &types.RequestFragment{
				Data:      make([]byte, len(data)),
				Timestamp: time.Now(),
				Direction: types.DirectionClientToServer,
			}
			copy(fragment.Data, data)
			return s.request.AddFragment(fragment)
		}
	} else {
		// 可能是响应体数据
		if s.response != nil {
			// 创建响应片段
			fragment := &types.ResponseFragment{
				Data:      make([]byte, len(data)),
				Timestamp: time.Now(),
				Direction: types.DirectionServerToClient,
			}
			copy(fragment.Data, data)
			return s.response.AddFragment(fragment)
		}
	}

	return nil
}

// closeStream 关闭流
func (s *Stream) closeStream(reset bool) error {
	oldState := s.state
	s.state = state.StreamStateClosed
	s.closed = true

	// 清理所有缓冲区以防止内存泄漏
	s.headerBuffer = nil
	s.dataBuffer = nil
	// 清理pendingData切片
	for i := range s.pendingData {
		s.pendingData[i] = nil // 帮助GC回收
	}
	s.pendingData = nil

	// 触发状态变化回调
	if s.onStateChange != nil {
		s.onStateChange(s, oldState, s.state)
	}

	// 触发完成回调
	if s.onComplete != nil {
		s.onComplete(s)
	}

	return nil
}

// StreamStateValidator HTTP/2流状态验证器
type StreamStateValidator struct{}

func (v *StreamStateValidator) ValidateTransition(from, to interface{}, event string) error {
	fromState, ok1 := from.(state.HTTP2StreamState)
	toState, ok2 := to.(state.HTTP2StreamState)

	if !ok1 || !ok2 {
		return errors.New("invalid state type")
	}

	// 定义有效的状态转换
	validTransitions := map[state.HTTP2StreamState][]state.HTTP2StreamState{
		state.StreamStateIdle: {
			state.StreamStateReservedLocal,
			state.StreamStateReservedRemote,
			state.StreamStateOpen,
			state.StreamStateHalfClosedRemote,
		},
		state.StreamStateReservedLocal: {
			state.StreamStateOpen,
			state.StreamStateHalfClosedLocal,
			state.StreamStateClosed,
		},
		state.StreamStateReservedRemote: {
			state.StreamStateOpen,
			state.StreamStateHalfClosedRemote,
			state.StreamStateClosed,
		},
		state.StreamStateOpen: {
			state.StreamStateHalfClosedLocal,
			state.StreamStateHalfClosedRemote,
			state.StreamStateClosed,
		},
		state.StreamStateHalfClosedLocal: {
			state.StreamStateClosed,
		},
		state.StreamStateHalfClosedRemote: {
			state.StreamStateClosed,
		},
	}

	validTargets, exists := validTransitions[fromState]
	if !exists {
		return errors.New("invalid state transition")
	}

	for _, validTarget := range validTargets {
		if validTarget == toState {
			return nil
		}
	}

	return errors.New("invalid state transition")
}
