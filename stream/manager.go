package stream

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/danl5/htrack/state"
	"github.com/danl5/htrack/types"
)

// Manager HTTP/2流管理器
type Manager struct {
	mu           sync.RWMutex
	connectionID string
	streams      map[uint32]*Stream
	maxStreams   uint32
	nextStreamID uint32
	isClient     bool
	closed       bool

	// 流控制
	connectionWindowSize int32
	maxFrameSize         uint32

	// 设置参数
	settings map[string]uint32

	// 统计信息
	stats *ManagerStats

	// 回调函数
	onStreamCreated   func(*Stream)
	onStreamClosed    func(*Stream)
	onStreamError     func(*Stream, error)
	onConnectionError func(error)
}

// ManagerStats 管理器统计信息
type ManagerStats struct {
	TotalStreams   uint64
	ActiveStreams  uint32
	ClosedStreams  uint64
	ErrorStreams   uint64
	BytesReceived  uint64
	BytesSent      uint64
	FramesReceived uint64
	FramesSent     uint64
	LastActivity   time.Time
	CreatedAt      time.Time
}

// NewManager 创建新的流管理器
func NewManager(connectionID string, isClient bool) *Manager {
	m := &Manager{
		connectionID:         connectionID,
		streams:              make(map[uint32]*Stream),
		maxStreams:           1000, // 默认最大流数
		isClient:             isClient,
		connectionWindowSize: 65535, // 默认连接窗口大小
		maxFrameSize:         16384, // 默认最大帧大小
		settings:             make(map[string]uint32),
		stats: &ManagerStats{
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
		},
	}

	// 设置初始流ID
	if isClient {
		m.nextStreamID = 1 // 客户端使用奇数
	} else {
		m.nextStreamID = 2 // 服务器使用偶数
	}

	// 设置默认参数
	m.settings["HEADER_TABLE_SIZE"] = 4096
	m.settings["ENABLE_PUSH"] = 1
	m.settings["MAX_CONCURRENT_STREAMS"] = 1000
	m.settings["INITIAL_WINDOW_SIZE"] = 65535
	m.settings["MAX_FRAME_SIZE"] = 16384
	m.settings["MAX_HEADER_LIST_SIZE"] = 8192

	return m
}

// CreateStream 创建新流
func (m *Manager) CreateStream(priority *types.StreamPriority) (*Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, errors.New("connection is closed")
	}

	// 检查流数量限制
	if uint32(len(m.streams)) >= m.maxStreams {
		return nil, errors.New("max streams exceeded")
	}

	// 获取下一个流ID
	streamID := m.nextStreamID
	m.nextStreamID += 2 // 跳过一个ID（奇偶性）

	// 创建流
	stream := NewStream(streamID, m.connectionID, priority)
	m.streams[streamID] = stream

	// 设置回调
	stream.SetOnStateChange(m.onStreamStateChange)
	stream.SetOnComplete(m.onStreamComplete)
	stream.SetOnError(m.onStreamErrorInternal)

	// 更新统计
	m.stats.TotalStreams++
	m.stats.ActiveStreams++
	m.stats.LastActivity = time.Now()

	// 触发回调
	if m.onStreamCreated != nil {
		m.onStreamCreated(stream)
	}

	return stream, nil
}

// GetStream 获取流
func (m *Manager) GetStream(streamID uint32) (*Stream, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stream, exists := m.streams[streamID]
	if !exists {
		return nil, fmt.Errorf("stream %d not found", streamID)
	}

	return stream, nil
}

// GetOrCreateStream 获取或创建流
func (m *Manager) GetOrCreateStream(streamID uint32, priority *types.StreamPriority) (*Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, errors.New("connection is closed")
	}

	// 检查流是否已存在
	if stream, exists := m.streams[streamID]; exists {
		return stream, nil
	}

	// 验证流ID
	if err := m.validateStreamID(streamID); err != nil {
		return nil, err
	}

	// 检查流数量限制
	if uint32(len(m.streams)) >= m.maxStreams {
		return nil, errors.New("max streams exceeded")
	}

	// 创建流
	stream := NewStream(streamID, m.connectionID, priority)
	m.streams[streamID] = stream

	// 设置回调
	stream.SetOnStateChange(m.onStreamStateChange)
	stream.SetOnComplete(m.onStreamComplete)
	stream.SetOnError(m.onStreamErrorInternal)

	// 更新统计
	m.stats.TotalStreams++
	m.stats.ActiveStreams++
	m.stats.LastActivity = time.Now()

	// 触发回调
	if m.onStreamCreated != nil {
		m.onStreamCreated(stream)
	}

	return stream, nil
}

// CloseStream 关闭流
func (m *Manager) CloseStream(streamID uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	stream, exists := m.streams[streamID]
	if !exists {
		return fmt.Errorf("stream %d not found", streamID)
	}

	if err := stream.Close(); err != nil {
		return err
	}

	// 从活跃流中移除
	delete(m.streams, streamID)

	// 更新统计
	m.stats.ActiveStreams--
	m.stats.ClosedStreams++
	m.stats.LastActivity = time.Now()

	return nil
}

// ResetStream 重置流
func (m *Manager) ResetStream(streamID uint32, errorCode uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	stream, exists := m.streams[streamID]
	if !exists {
		return fmt.Errorf("stream %d not found", streamID)
	}

	if err := stream.Reset(errorCode); err != nil {
		return err
	}

	// 从活跃流中移除
	delete(m.streams, streamID)

	// 更新统计
	m.stats.ActiveStreams--
	m.stats.ErrorStreams++
	m.stats.LastActivity = time.Now()

	return nil
}

// ProcessHeaders 处理头部帧
func (m *Manager) ProcessHeaders(streamID uint32, headers map[string]string, endStream bool, priority *types.StreamPriority) error {
	// 获取或创建流
	stream, err := m.GetOrCreateStream(streamID, priority)
	if err != nil {
		return err
	}

	// 更新统计
	m.mu.Lock()
	m.stats.FramesReceived++
	m.stats.LastActivity = time.Now()
	m.mu.Unlock()

	return stream.ProcessHeaders(headers, endStream)
}

// ProcessData 处理数据帧
func (m *Manager) ProcessData(streamID uint32, data []byte, endStream bool) error {
	stream, err := m.GetStream(streamID)
	if err != nil {
		return err
	}

	// 检查连接级流控制
	m.mu.Lock()
	if int32(len(data)) > m.connectionWindowSize {
		m.mu.Unlock()
		return errors.New("connection flow control violation")
	}
	m.connectionWindowSize -= int32(len(data))

	// 更新统计
	m.stats.FramesReceived++
	m.stats.BytesReceived += uint64(len(data))
	m.stats.LastActivity = time.Now()
	m.mu.Unlock()

	return stream.ProcessData(data, endStream)
}

// UpdateWindowSize 更新窗口大小
func (m *Manager) UpdateWindowSize(streamID uint32, increment int32) error {
	if streamID == 0 {
		// 连接级窗口更新
		m.mu.Lock()
		m.connectionWindowSize += increment
		if m.connectionWindowSize < 0 {
			m.mu.Unlock()
			return errors.New("connection window size underflow")
		}
		m.mu.Unlock()
		return nil
	}

	// 流级窗口更新
	stream, err := m.GetStream(streamID)
	if err != nil {
		return err
	}

	return stream.UpdateWindowSize(increment)
}

// UpdateSettings 更新设置
func (m *Manager) UpdateSettings(settings map[string]uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, value := range settings {
		m.settings[key] = value

		// 应用特定设置
		switch key {
		case "MAX_CONCURRENT_STREAMS":
			m.maxStreams = value
		case "INITIAL_WINDOW_SIZE":
			// 更新所有流的窗口大小
			for _, stream := range m.streams {
				stream.UpdateWindowSize(int32(value) - 65535) // 假设原始窗口大小为65535
			}
		case "MAX_FRAME_SIZE":
			m.maxFrameSize = value
		}
	}

	return nil
}

// GetActiveStreams 获取活跃流列表
func (m *Manager) GetActiveStreams() []*Stream {
	m.mu.RLock()
	defer m.mu.RUnlock()

	streams := make([]*Stream, 0, len(m.streams))
	for _, stream := range m.streams {
		streams = append(streams, stream)
	}

	return streams
}

// GetStreamCount 获取流数量
func (m *Manager) GetStreamCount() uint32 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return uint32(len(m.streams))
}

// GetStats 获取统计信息
func (m *Manager) GetStats() *ManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 返回统计信息的副本
	stats := *m.stats
	return &stats
}

// Close 关闭管理器
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	// 关闭所有流
	for streamID, stream := range m.streams {
		stream.Close()
		delete(m.streams, streamID)
	}

	m.closed = true
	m.stats.LastActivity = time.Now()

	return nil
}

// IsClosed 检查是否已关闭
func (m *Manager) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closed
}

// 设置回调函数
func (m *Manager) SetOnStreamCreated(callback func(*Stream)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStreamCreated = callback
}

func (m *Manager) SetOnStreamClosed(callback func(*Stream)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStreamClosed = callback
}

func (m *Manager) SetOnStreamError(callback func(*Stream, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStreamError = callback
}

func (m *Manager) SetOnConnectionError(callback func(error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onConnectionError = callback
}

// 私有方法

// validateStreamID 验证流ID
func (m *Manager) validateStreamID(streamID uint32) error {
	if streamID == 0 {
		return errors.New("stream ID cannot be zero")
	}

	// 检查奇偶性
	if m.isClient {
		if streamID%2 == 0 {
			return errors.New("client streams must have odd IDs")
		}
	} else {
		if streamID%2 == 1 {
			return errors.New("server streams must have even IDs")
		}
	}

	return nil
}

// 内部回调方法
func (m *Manager) onStreamStateChange(stream *Stream, from, to state.HTTP2StreamState) {
	// 可以在这里添加状态变化的处理逻辑
}

func (m *Manager) onStreamComplete(stream *Stream) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 从活跃流中移除
	delete(m.streams, stream.GetID())

	// 更新统计
	m.stats.ActiveStreams--
	m.stats.ClosedStreams++
	m.stats.LastActivity = time.Now()

	// 触发外部回调
	if m.onStreamClosed != nil {
		m.onStreamClosed(stream)
	}
}

func (m *Manager) onStreamErrorInternal(stream *Stream, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 从活跃流中移除
	delete(m.streams, stream.GetID())

	// 更新统计
	m.stats.ActiveStreams--
	m.stats.ErrorStreams++
	m.stats.LastActivity = time.Now()

	// 触发外部回调
	if m.onStreamError != nil {
		m.onStreamError(stream, err)
	}
}
