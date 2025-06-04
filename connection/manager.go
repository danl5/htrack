package connection

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/danl5/htrack/parser"
	"github.com/danl5/htrack/state"
	"github.com/danl5/htrack/stream"
	"github.com/danl5/htrack/types"
)

// Manager 连接管理器
type Manager struct {
	mu           sync.RWMutex
	connections  map[string]*Connection
	packetBuffer *parser.PacketBuffer
	stateMachine *state.StateMachineManager
	callbacks    *Callbacks
	config       *Config
	statistics   *Statistics
	closed       bool
}

// Connection HTTP连接
type Connection struct {
	ID            string
	Version       types.HTTPVersion
	State         types.ConnectionState
	Metadata      *types.ConnectionMetadata
	Parser        parser.Parser
	StreamManager *stream.Manager
	StateMachine  state.StateMachine
	Transactions  map[string]*Transaction
	PacketBuffer  *parser.PacketBuffer
	LastDirection types.Direction
	CreatedAt     time.Time
	LastActivity  time.Time
	Mu            sync.RWMutex
}

// Transaction HTTP事务
type Transaction struct {
	ID           string
	ConnectionID string
	StreamID     *uint32
	Request      *types.HTTPRequest
	Response     *types.HTTPResponse
	State        types.TransactionState
	Metadata     *types.TransactionMetadata
	StateMachine state.StateMachine
	CreatedAt    time.Time
	CompletedAt  *time.Time
}

// Config 配置
type Config struct {
	MaxConnections     int
	MaxTransactions    int
	ConnectionTimeout  time.Duration
	TransactionTimeout time.Duration
	BufferSize         int
	EnableHTTP2        bool
	EnableHTTP1        bool
}

// Callbacks 回调函数
type Callbacks struct {
	OnConnectionCreated   func(*Connection)
	OnConnectionClosed    func(*Connection)
	OnTransactionCreated  func(*Transaction)
	OnTransactionComplete func(*Transaction)
	OnRequestParsed       func(*types.HTTPRequest)
	OnResponseParsed      func(*types.HTTPResponse)
	OnError               func(error)
}

// Statistics 统计信息
type Statistics struct {
	mu                 sync.RWMutex
	TotalConnections   int64
	ActiveConnections  int64
	TotalTransactions  int64
	ActiveTransactions int64
	TotalRequests      int64
	TotalResponses     int64
	TotalBytes         int64
	ErrorCount         int64
	HTTP1Connections   int64
	HTTP2Connections   int64
	HTTP2Streams       int64
}

// NewManager 创建连接管理器
func NewManager(config *Config) *Manager {
	if config == nil {
		config = DefaultConfig()
	}

	m := &Manager{
		connections:  make(map[string]*Connection),
		packetBuffer: parser.NewPacketBuffer(config.BufferSize),
		stateMachine: state.NewStateMachineManager(),
		callbacks:    &Callbacks{},
		config:       config,
		statistics:   &Statistics{},
	}

	// 启动定期清理goroutine
	go m.startCleanupRoutine()

	return m
}

// ProcessPacket 处理数据包
func (m *Manager) ProcessPacket(connectionID string, data []byte, direction types.Direction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.New("manager is closed")
	}

	// 获取或创建连接
	conn, err := m.getOrCreateConnection(connectionID, data)
	if err != nil {
		return err
	}

	// 更新活动时间
	conn.LastActivity = time.Now()
	conn.LastDirection = direction

	// 添加数据到缓冲区
	conn.PacketBuffer.AddPacket(data, direction)

	// 尝试解析完整消息
	return m.tryParseMessages(conn)
}

// GetConnection 获取连接
func (m *Manager) GetConnection(connectionID string) (*Connection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.connections[connectionID]
	return conn, exists
}

// GetActiveConnections 获取活跃连接
func (m *Manager) GetActiveConnections() []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var connections []*Connection
	for _, conn := range m.connections {
		if conn.State == types.ConnectionStateEstablished {
			connections = append(connections, conn)
		}
	}

	return connections
}

// GetTransaction 获取事务
func (m *Manager) GetTransaction(transactionID string) (*Transaction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, conn := range m.connections {
		conn.Mu.RLock()
		if tx, exists := conn.Transactions[transactionID]; exists {
			conn.Mu.RUnlock()
			return tx, true
		}
		conn.Mu.RUnlock()
	}

	return nil, false
}

// GetActiveTransactions 获取活跃事务
func (m *Manager) GetActiveTransactions() []*Transaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var transactions []*Transaction
	for _, conn := range m.connections {
		conn.Mu.RLock()
		for _, tx := range conn.Transactions {
			if tx.State != types.TransactionStateCompleted {
				transactions = append(transactions, tx)
			}
		}
		conn.Mu.RUnlock()
	}

	return transactions
}

// SetCallbacks 设置回调函数
func (m *Manager) SetCallbacks(callbacks *Callbacks) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if callbacks != nil {
		m.callbacks = callbacks
	}
}

// GetStatistics 获取统计信息
func (m *Manager) GetStatistics() *Statistics {
	m.statistics.mu.RLock()
	defer m.statistics.mu.RUnlock()

	// 返回统计信息的副本
	return &Statistics{
		TotalConnections:   m.statistics.TotalConnections,
		ActiveConnections:  m.statistics.ActiveConnections,
		TotalTransactions:  m.statistics.TotalTransactions,
		ActiveTransactions: m.statistics.ActiveTransactions,
		TotalRequests:      m.statistics.TotalRequests,
		TotalResponses:     m.statistics.TotalResponses,
		TotalBytes:         m.statistics.TotalBytes,
		ErrorCount:         m.statistics.ErrorCount,
		HTTP1Connections:   m.statistics.HTTP1Connections,
		HTTP2Connections:   m.statistics.HTTP2Connections,
		HTTP2Streams:       m.statistics.HTTP2Streams,
	}
}

// CleanupExpired 清理过期连接和事务
func (m *Manager) CleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// 清理过期连接
	for id, conn := range m.connections {
		if now.Sub(conn.LastActivity) > m.config.ConnectionTimeout {
			m.closeConnection(conn)
			delete(m.connections, id)
		}
	}

	// 清理过期事务
	for _, conn := range m.connections {
		conn.Mu.Lock()
		for id, tx := range conn.Transactions {
			if tx.CompletedAt == nil && now.Sub(tx.CreatedAt) > m.config.TransactionTimeout {
				tx.State = types.TransactionStateTimeout
				completedAt := now
				tx.CompletedAt = &completedAt
				delete(conn.Transactions, id)
			}
		}
		conn.Mu.Unlock()
	}
}

// Close 关闭管理器
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	// 关闭所有连接
	for _, conn := range m.connections {
		m.closeConnection(conn)
	}

	// 清空连接映射
	m.connections = make(map[string]*Connection)

	m.closed = true

	return nil
}

// 私有方法

// getOrCreateConnection 获取或创建连接
func (m *Manager) getOrCreateConnection(connectionID string, data []byte) (*Connection, error) {
	if conn, exists := m.connections[connectionID]; exists {
		return conn, nil
	}

	// 检查连接数限制
	if len(m.connections) >= m.config.MaxConnections {
		return nil, errors.New("max connections reached")
	}

	// 检测HTTP版本
	version := m.DetectHTTPVersion(data)

	// 创建解析器
	var httpParser parser.Parser
	var streamManager *stream.Manager

	switch version {
	case types.HTTP11, types.HTTP10:
		if !m.config.EnableHTTP1 {
			return nil, errors.New("HTTP/1.x not enabled")
		}
		httpParser = parser.NewHTTP1ParserWithVersion(version)

	case types.HTTP2:
		if !m.config.EnableHTTP2 {
			return nil, errors.New("HTTP/2 not enabled")
		}
		httpParser = parser.NewHTTP2Parser()
		streamManager = stream.NewManager(connectionID, true)

	default:
		return nil, fmt.Errorf("unsupported HTTP version: %v", version)
	}

	// 创建状态机
	stateMachine := state.NewGenericStateMachine(
		connectionID,
		types.ConnectionStateIdle,
		state.NewConnectionStateValidator(),
	)

	// 创建连接
	conn := &Connection{
		ID:            connectionID,
		Version:       version,
		State:         types.ConnectionStateIdle,
		Metadata:      &types.ConnectionMetadata{},
		Parser:        httpParser,
		StreamManager: streamManager,
		StateMachine:  stateMachine,
		Transactions:  make(map[string]*Transaction),
		PacketBuffer:  parser.NewPacketBuffer(m.config.BufferSize),
		CreatedAt:     time.Now(),
		LastActivity:  time.Now(),
	}

	// 更新状态
	conn.State = types.ConnectionStateEstablished
	event := &state.StateMachineEvent{
		Type:      state.EventConnectionEstablished,
		Timestamp: time.Now(),
		Source:    "connection_manager",
	}
	conn.StateMachine.Transition(event)

	// 添加到管理器
	m.connections[connectionID] = conn

	// 更新统计
	m.updateConnectionStatistics(version, true)

	// 触发回调
	if m.callbacks.OnConnectionCreated != nil {
		m.callbacks.OnConnectionCreated(conn)
	}

	return conn, nil
}

func (m *Manager) DetectHTTPVersion(data []byte) types.HTTPVersion {
	// 最小数据长度检查
	if len(data) == 0 {
		return types.Unknown
	}

	// 优先检测HTTP/2
	if version := parser.NewHTTP2Parser().DetectVersion(data); version == types.HTTP2 {
		return types.HTTP2
	}

	// 检测HTTP/1.x
	return parser.NewHTTP1Parser().DetectVersion(data)
}

// tryParseMessages 尝试解析消息
func (m *Manager) tryParseMessages(conn *Connection) error {
	var data []byte

	// 根据方向获取相应的数据
	if conn.LastDirection == types.DirectionRequest {
		data = conn.PacketBuffer.GetRequestData()
	} else if conn.LastDirection == types.DirectionResponse {
		data = conn.PacketBuffer.GetResponseData()
	} else {
		// 如果没有方向信息，使用所有数据（向后兼容）
		data = conn.PacketBuffer.GetReassembledData()
	}

	if len(data) == 0 {
		return nil
	}

	return m.tryParseHTTPMessages(conn, data)
}

// tryParseHTTPMessages 尝试解析HTTP消息
func (m *Manager) tryParseHTTPMessages(conn *Connection, data []byte) error {
	// 检查是否有完整消息或者数据看起来像HTTP但格式错误
	isComplete := conn.Parser.IsComplete(data)

	// 改进的HTTP数据检测逻辑
	looksLikeHTTP := m.looksLikeHTTPData(data)

	// 只有当数据包含完整的HTTP头部结束标记时才认为是完整的HTTP消息
	hasCompleteHeaders := bytes.Contains(data, []byte("\r\n\r\n"))

	// 对于HTTP/2，使用不同的完整性检查
	if conn.Version == types.HTTP2 {
		// HTTP/2数据总是尝试解析
		return m.parseHTTP2Messages(conn, data)
	}

	if !isComplete && !looksLikeHTTP {
		return nil // 等待更多数据
	}

	// 对于分片数据，只有当有完整头部时才尝试解析
	isInvalidHTTPData := looksLikeHTTP && !hasCompleteHeaders && !isComplete &&
		!bytes.HasPrefix(data, []byte("GET ")) &&
		!bytes.HasPrefix(data, []byte("POST ")) &&
		!bytes.HasPrefix(data, []byte("PUT ")) &&
		!bytes.HasPrefix(data, []byte("DELETE ")) &&
		!bytes.HasPrefix(data, []byte("HTTP/"))

	if looksLikeHTTP && !hasCompleteHeaders && !isComplete && !isInvalidHTTPData {
		return nil // 等待更多数据
	}

	// 根据方向尝试解析相应的消息类型
	if conn.LastDirection == types.DirectionRequest {
		return m.parseRequestMessage(conn, data, isComplete, looksLikeHTTP)
	} else if conn.LastDirection == types.DirectionResponse {
		return m.parseResponseMessage(conn, data, isComplete, looksLikeHTTP)
	} else {
		// 如果没有方向信息，尝试两种解析（向后兼容）
		if err := m.parseRequestMessage(conn, data, isComplete, looksLikeHTTP); err == nil {
			return nil
		}
		return m.parseResponseMessage(conn, data, isComplete, looksLikeHTTP)
	}
}

// looksLikeHTTPData 检查数据是否看起来像HTTP
func (m *Manager) looksLikeHTTPData(data []byte) bool {
	return bytes.Contains(data, []byte("HTTP")) ||
		bytes.HasPrefix(data, []byte("GET ")) ||
		bytes.HasPrefix(data, []byte("POST ")) ||
		bytes.HasPrefix(data, []byte("PUT ")) ||
		bytes.HasPrefix(data, []byte("DELETE ")) ||
		bytes.HasPrefix(data, []byte("HEAD ")) ||
		bytes.HasPrefix(data, []byte("OPTIONS ")) ||
		bytes.HasPrefix(data, []byte("PATCH ")) ||
		// 检测可能的无效HTTP数据
		(len(data) > 10 && bytes.Contains(bytes.ToUpper(data), []byte("HTTP")))
}

// parseHTTP2Messages 解析HTTP/2消息 - 支持多流并发处理
func (m *Manager) parseHTTP2Messages(conn *Connection, data []byte) error {
	// HTTP/2的解析逻辑 - 支持多流并发
	if conn.LastDirection == types.DirectionRequest {
		// 尝试解析所有请求流
		if http2Parser, ok := conn.Parser.(*parser.HTTP2Parser); ok {
			if requests, err := http2Parser.ParseAllRequests(data); err == nil {
				// 处理所有请求
				for _, request := range requests {
					if err := m.handleParsedRequest(conn, request); err != nil {
						if m.callbacks != nil && m.callbacks.OnError != nil {
							m.callbacks.OnError(fmt.Errorf("failed to handle request for stream %d: %w", *request.StreamID, err))
						}
					}
				}
				return nil
			} else {
				// 对于HTTP/2，某些错误是可以接受的（如只有连接前导）
				if bytes.HasPrefix(data, []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")) {
					return nil // 连接前导是正常的
				}
				// 检查是否是不完整的帧数据
				if len(data) < 9 {
					return nil // 等待更多数据
				}
				// 检查帧长度是否完整
				frameLength := uint32(data[0])<<16 | uint32(data[1])<<8 | uint32(data[2])
				if len(data) < int(9+frameLength) {
					return nil // 等待完整帧数据
				}
				// For HTTP/2 fragmented data, don't log errors - just wait for more data
				// This is especially important for extreme fragmentation scenarios
				return nil // 不返回错误，继续处理
			}
		} else {
			// 回退到单流解析
			if request, err := conn.Parser.ParseRequest(conn.ID, data); err == nil {
				return m.handleParsedRequest(conn, request)
			}
		}
	} else if conn.LastDirection == types.DirectionResponse {
		// 尝试解析所有响应流
		if http2Parser, ok := conn.Parser.(*parser.HTTP2Parser); ok {
			if responses, err := http2Parser.ParseAllResponses(data); err == nil {
				// 处理所有响应
				for _, response := range responses {
					if err := m.handleParsedResponse(conn, response); err != nil {
						if m.callbacks != nil && m.callbacks.OnError != nil {
							m.callbacks.OnError(fmt.Errorf("failed to handle response for stream %d: %w", *response.StreamID, err))
						}
					}
				}
				return nil
			} else {
				// For HTTP/2 fragmented data, don't log errors - just wait for more data
				return nil // 不返回错误，继续处理
			}
		} else {
			// 回退到单流解析
			if response, err := conn.Parser.ParseResponse(conn.ID, data); err == nil {
				return m.handleParsedResponse(conn, response)
			}
		}
	}
	return nil
}

// parseRequestMessage 解析请求消息
func (m *Manager) parseRequestMessage(conn *Connection, data []byte, isComplete, looksLikeHTTP bool) error {
	if request, err := conn.Parser.ParseRequest(conn.ID, data); err == nil {
		return m.handleParsedRequest(conn, request)
	} else if isComplete || looksLikeHTTP {
		if m.callbacks != nil && m.callbacks.OnError != nil {
			m.callbacks.OnError(fmt.Errorf("failed to parse request: %w", err))
		}
		return fmt.Errorf("failed to parse HTTP message")
	}
	return nil
}

// parseResponseMessage 解析响应消息
func (m *Manager) parseResponseMessage(conn *Connection, data []byte, isComplete, looksLikeHTTP bool) error {
	if response, err := conn.Parser.ParseResponse(conn.ID, data); err == nil {
		return m.handleParsedResponse(conn, response)
	} else if isComplete || looksLikeHTTP {
		if m.callbacks != nil && m.callbacks.OnError != nil {
			m.callbacks.OnError(fmt.Errorf("failed to parse response: %w", err))
		}
		return fmt.Errorf("failed to parse HTTP message")
	}
	return nil
}

// handleParsedRequest 处理解析的请求
func (m *Manager) handleParsedRequest(conn *Connection, request *types.HTTPRequest) error {
	// 创建事务
	transactionID := generateTransactionID()
	tx := &Transaction{
		ID:           transactionID,
		ConnectionID: conn.ID,
		StreamID:     request.StreamID,
		Request:      request,
		State:        types.TransactionStateRequestReceived,
		Metadata:     &types.TransactionMetadata{},
		StateMachine: state.NewGenericStateMachine(
			transactionID,
			types.TransactionStateIdle,
			state.NewTransactionStateValidator(),
		),
		CreatedAt: time.Now(),
	}

	// 添加到连接
	conn.Mu.Lock()
	conn.Transactions[transactionID] = tx
	conn.Mu.Unlock()

	// 如果请求完成且有StreamID，关闭对应的stream
	if request.Complete && request.StreamID != nil && conn.StreamManager != nil {
		if err := conn.StreamManager.CloseStream(*request.StreamID); err != nil {
			// 记录错误但不中断处理流程
			// 可以在这里添加日志记录
		}
	}

	// 更新统计
	m.statistics.mu.Lock()
	m.statistics.TotalRequests++
	m.statistics.TotalTransactions++
	m.statistics.ActiveTransactions++
	m.statistics.mu.Unlock()

	// 触发回调
	if m.callbacks.OnTransactionCreated != nil {
		m.callbacks.OnTransactionCreated(tx)
	}
	if m.callbacks.OnRequestParsed != nil {
		m.callbacks.OnRequestParsed(request)
	}

	return nil
}

// handleParsedResponse 处理解析的响应
func (m *Manager) handleParsedResponse(conn *Connection, response *types.HTTPResponse) error {
	// 查找对应的事务
	var tx *Transaction
	conn.Mu.RLock()
	for _, transaction := range conn.Transactions {
		if response.StreamID != nil && transaction.StreamID != nil {
			if *response.StreamID == *transaction.StreamID {
				tx = transaction
				break
			}
		} else if response.StreamID == nil && transaction.StreamID == nil {
			// HTTP/1.x情况
			if transaction.Response == nil {
				tx = transaction
				break
			}
		}
	}
	conn.Mu.RUnlock()

	if tx != nil {
		// 更新事务
		tx.Response = response
		tx.State = types.TransactionStateCompleted
		completedAt := time.Now()
		tx.CompletedAt = &completedAt

		// 如果响应完成且有StreamID，关闭对应的stream
		if response.Complete && response.StreamID != nil && conn.StreamManager != nil {
			if err := conn.StreamManager.CloseStream(*response.StreamID); err != nil {
				// 记录错误但不中断处理流程
				// 可以在这里添加日志记录
			}
		}

		// 更新统计
		m.statistics.mu.Lock()
		m.statistics.TotalResponses++
		m.statistics.ActiveTransactions--
		m.statistics.mu.Unlock()

		// 触发回调
		if m.callbacks.OnTransactionComplete != nil {
			m.callbacks.OnTransactionComplete(tx)
		}

		// 清理已完成的事务以防止内存泄漏
		conn.Mu.Lock()
		delete(conn.Transactions, tx.ID)
		conn.Mu.Unlock()
	}

	if m.callbacks.OnResponseParsed != nil {
		m.callbacks.OnResponseParsed(response)
	}

	return nil
}

// closeConnection 关闭连接
func (m *Manager) closeConnection(conn *Connection) {
	conn.State = types.ConnectionStateClosed

	// 关闭流管理器（如果存在）
	if conn.StreamManager != nil {
		conn.StreamManager.Close()
	}

	// 更新统计
	m.updateConnectionStatistics(conn.Version, false)

	// 触发回调
	if m.callbacks.OnConnectionClosed != nil {
		m.callbacks.OnConnectionClosed(conn)
	}
}

// updateConnectionStatistics 更新连接统计
func (m *Manager) updateConnectionStatistics(version types.HTTPVersion, increment bool) {
	m.statistics.mu.Lock()
	defer m.statistics.mu.Unlock()

	if increment {
		m.statistics.TotalConnections++
		m.statistics.ActiveConnections++

		switch version {
		case types.HTTP11, types.HTTP10:
			m.statistics.HTTP1Connections++
		case types.HTTP2:
			m.statistics.HTTP2Connections++
		}
	} else {
		m.statistics.ActiveConnections--
	}
}

// generateTransactionID 生成事务ID
func generateTransactionID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// startCleanupRoutine 启动定期清理例程
func (m *Manager) startCleanupRoutine() {
	ticker := time.NewTicker(5 * time.Minute) // 每5分钟清理一次
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.performCleanup()
		}
	}
}

// performCleanup 执行清理操作
func (m *Manager) performCleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}

	now := time.Now()
	connectionsToRemove := make([]string, 0)

	// 清理过期连接
	for connectionID, conn := range m.connections {
		// 检查连接是否超时
		if now.Sub(conn.LastActivity) > m.config.ConnectionTimeout {
			connectionsToRemove = append(connectionsToRemove, connectionID)
			continue
		}

		// 清理连接中的过期事务
		conn.Mu.Lock()
		transactionsToRemove := make([]string, 0)
		for txID, tx := range conn.Transactions {
			// 如果事务已完成且超过超时时间，或者事务创建时间超过超时时间
			if (tx.CompletedAt != nil && now.Sub(*tx.CompletedAt) > m.config.TransactionTimeout) ||
				(tx.CompletedAt == nil && now.Sub(tx.CreatedAt) > m.config.TransactionTimeout) {
				transactionsToRemove = append(transactionsToRemove, txID)
			}
		}
		// 移除过期事务
		for _, txID := range transactionsToRemove {
			delete(conn.Transactions, txID)
		}
		conn.Mu.Unlock()
	}

	// 移除过期连接
	for _, connectionID := range connectionsToRemove {
		if conn, exists := m.connections[connectionID]; exists {
			m.closeConnection(conn)
			delete(m.connections, connectionID)
		}
	}

	// PacketBuffer是单个缓冲区，不需要额外清理
	// 过期连接的清理已经包含了相关的缓冲区清理
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		MaxConnections:     1000,
		MaxTransactions:    10000,
		ConnectionTimeout:  30 * time.Minute,
		TransactionTimeout: 5 * time.Minute,
		BufferSize:         64 * 1024, // 64KB
		EnableHTTP2:        true,
		EnableHTTP1:        true,
	}
}
