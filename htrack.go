package htrack

import (
	"errors"
	"time"

	"github.com/danl5/htrack/connection"
	"github.com/danl5/htrack/types"
)

// HTrack HTTP协议数据解析器
type HTrack struct {
	manager *connection.Manager
	config  *Config

	// Channel输出
	RequestChan  chan *types.HTTPRequest  // 请求完成通道
	ResponseChan chan *types.HTTPResponse // 响应完成通道
}

// Config HTrack配置
type Config struct {
	// 解析器配置
	MaxSessions        int           `json:"max_sessions"`        // 最大并发解析会话数
	MaxTransactions    int           `json:"max_transactions"`    // 最大事务数
	SessionTimeout     time.Duration `json:"session_timeout"`     // 会话超时时间
	TransactionTimeout time.Duration `json:"transaction_timeout"` // 事务超时时间

	// 缓冲区配置
	BufferSize int `json:"buffer_size"` // 数据包缓冲区大小

	// 协议支持
	EnableHTTP1 bool `json:"enable_http1"` // 启用HTTP/1.x解析
	EnableHTTP2 bool `json:"enable_http2"` // 启用HTTP/2解析

	// 自动清理
	AutoCleanup     bool          `json:"auto_cleanup"`     // 自动清理过期数据
	CleanupInterval time.Duration `json:"cleanup_interval"` // 清理间隔

	// Channel配置
	ChannelBufferSize int  `json:"channel_buffer_size"` // Channel缓冲区大小
	EnableChannels    bool `json:"enable_channels"`     // 是否启用Channel输出
}

// EventHandlers 事件处理器
type EventHandlers struct {
	// 连接事件
	OnConnectionCreated func(connectionID string, version types.HTTPVersion)
	OnConnectionClosed  func(connectionID string)

	// 事务事件
	OnTransactionCreated  func(transactionID, connectionID string)
	OnTransactionComplete func(transactionID string, request *types.HTTPRequest, response *types.HTTPResponse)

	// 消息事件
	OnRequestParsed  func(request *types.HTTPRequest)
	OnResponseParsed func(response *types.HTTPResponse)

	// 错误事件
	OnError func(err error)
}

// Statistics 统计信息
type Statistics struct {
	TotalConnections   int64 `json:"total_connections"`
	ActiveConnections  int64 `json:"active_connections"`
	TotalTransactions  int64 `json:"total_transactions"`
	ActiveTransactions int64 `json:"active_transactions"`
	TotalRequests      int64 `json:"total_requests"`
	TotalResponses     int64 `json:"total_responses"`
	TotalBytes         int64 `json:"total_bytes"`
	ErrorCount         int64 `json:"error_count"`
	HTTP1Connections   int64 `json:"http1_connections"`
	HTTP2Connections   int64 `json:"http2_connections"`
	HTTP2Streams       int64 `json:"http2_streams"`
}

// ConnectionInfo 连接信息
type ConnectionInfo struct {
	ID           string                `json:"id"`
	Version      types.HTTPVersion     `json:"version"`
	State        types.ConnectionState `json:"state"`
	CreatedAt    time.Time             `json:"created_at"`
	LastActivity time.Time             `json:"last_activity"`
	Transactions []string              `json:"transactions"`
}

// TransactionInfo 事务信息
type TransactionInfo struct {
	ID           string                 `json:"id"`
	ConnectionID string                 `json:"connection_id"`
	StreamID     *uint32                `json:"stream_id,omitempty"`
	State        types.TransactionState `json:"state"`
	CreatedAt    time.Time              `json:"created_at"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
	HasRequest   bool                   `json:"has_request"`
	HasResponse  bool                   `json:"has_response"`
}

// New 创建新的HTrack实例
func New(config *Config) *HTrack {
	if config == nil {
		config = DefaultConfig()
	}

	// 转换配置
	connConfig := &connection.Config{
		MaxConnections:     config.MaxSessions,
		MaxTransactions:    config.MaxTransactions,
		ConnectionTimeout:  config.SessionTimeout,
		TransactionTimeout: config.TransactionTimeout,
		BufferSize:         config.BufferSize,
		EnableHTTP2:        config.EnableHTTP2,
		EnableHTTP1:        config.EnableHTTP1,
	}

	ht := &HTrack{
		manager: connection.NewManager(connConfig),
		config:  config,
	}

	// 初始化channels
	if config.EnableChannels {
		bufferSize := config.ChannelBufferSize
		if bufferSize <= 0 {
			bufferSize = 100 // 默认缓冲区大小
		}
		ht.RequestChan = make(chan *types.HTTPRequest, bufferSize)
		ht.ResponseChan = make(chan *types.HTTPResponse, bufferSize)

		// 设置内部事件处理器，将解析完成的请求和响应发送到channel
		ht.setupChannelHandlers()
	}

	// 启动自动清理
	if config.AutoCleanup {
		go ht.autoCleanup()
	}

	return ht
}

// ProcessPacket 处理HTTP数据包
// sessionID: 会话标识符（用于关联同一会话的请求响应）
// data: HTTP协议数据包内容
// direction: 数据方向（请求/响应）
func (ht *HTrack) ProcessPacket(sessionID string, data []byte, direction types.Direction) error {
	if len(data) == 0 {
		return errors.New("empty packet data")
	}

	return ht.manager.ProcessPacket(sessionID, data, direction)
}

// SetEventHandlers 设置事件处理器
func (ht *HTrack) SetEventHandlers(handlers *EventHandlers) {
	if handlers == nil {
		return
	}

	callbacks := &connection.Callbacks{}

	if handlers.OnConnectionCreated != nil {
		callbacks.OnConnectionCreated = func(conn *connection.Connection) {
			handlers.OnConnectionCreated(conn.ID, conn.Version)
		}
	}

	if handlers.OnConnectionClosed != nil {
		callbacks.OnConnectionClosed = func(conn *connection.Connection) {
			handlers.OnConnectionClosed(conn.ID)
		}
	}

	if handlers.OnTransactionCreated != nil {
		callbacks.OnTransactionCreated = func(tx *connection.Transaction) {
			handlers.OnTransactionCreated(tx.ID, tx.ConnectionID)
		}
	}

	if handlers.OnTransactionComplete != nil {
		callbacks.OnTransactionComplete = func(tx *connection.Transaction) {
			handlers.OnTransactionComplete(tx.ID, tx.Request, tx.Response)
		}
	}

	if handlers.OnRequestParsed != nil {
		callbacks.OnRequestParsed = handlers.OnRequestParsed
	}

	if handlers.OnResponseParsed != nil {
		callbacks.OnResponseParsed = handlers.OnResponseParsed
	}

	if handlers.OnError != nil {
		callbacks.OnError = handlers.OnError
	}

	ht.manager.SetCallbacks(callbacks)
}

// GetConnection 获取连接信息
func (ht *HTrack) GetConnection(connectionID string) (*ConnectionInfo, error) {
	conn, exists := ht.manager.GetConnection(connectionID)
	if !exists {
		return nil, errors.New("connection not found")
	}

	// 获取事务列表
	var transactions []string
	conn.Mu.RLock()
	for txID := range conn.Transactions {
		transactions = append(transactions, txID)
	}
	conn.Mu.RUnlock()

	return &ConnectionInfo{
		ID:           conn.ID,
		Version:      conn.Version,
		State:        conn.State,
		CreatedAt:    conn.CreatedAt,
		LastActivity: conn.LastActivity,
		Transactions: transactions,
	}, nil
}

// GetActiveConnections 获取所有活跃连接
func (ht *HTrack) GetActiveConnections() []*ConnectionInfo {
	conns := ht.manager.GetActiveConnections()
	var result []*ConnectionInfo

	for _, conn := range conns {
		var transactions []string
		conn.Mu.RLock()
		for txID := range conn.Transactions {
			transactions = append(transactions, txID)
		}
		conn.Mu.RUnlock()

		result = append(result, &ConnectionInfo{
			ID:           conn.ID,
			Version:      conn.Version,
			State:        conn.State,
			CreatedAt:    conn.CreatedAt,
			LastActivity: conn.LastActivity,
			Transactions: transactions,
		})
	}

	return result
}

// GetTransaction 获取事务信息
func (ht *HTrack) GetTransaction(transactionID string) (*TransactionInfo, error) {
	tx, exists := ht.manager.GetTransaction(transactionID)
	if !exists {
		return nil, errors.New("transaction not found")
	}

	return &TransactionInfo{
		ID:           tx.ID,
		ConnectionID: tx.ConnectionID,
		StreamID:     tx.StreamID,
		State:        tx.State,
		CreatedAt:    tx.CreatedAt,
		CompletedAt:  tx.CompletedAt,
		HasRequest:   tx.Request != nil,
		HasResponse:  tx.Response != nil,
	}, nil
}

// GetActiveTransactions 获取所有活跃事务
func (ht *HTrack) GetActiveTransactions() []*TransactionInfo {
	txs := ht.manager.GetActiveTransactions()
	var result []*TransactionInfo

	for _, tx := range txs {
		result = append(result, &TransactionInfo{
			ID:           tx.ID,
			ConnectionID: tx.ConnectionID,
			StreamID:     tx.StreamID,
			State:        tx.State,
			CreatedAt:    tx.CreatedAt,
			CompletedAt:  tx.CompletedAt,
			HasRequest:   tx.Request != nil,
			HasResponse:  tx.Response != nil,
		})
	}

	return result
}

// GetRequestResponse 获取完整的请求响应对
func (ht *HTrack) GetRequestResponse(transactionID string) (*types.HTTPRequest, *types.HTTPResponse, error) {
	tx, exists := ht.manager.GetTransaction(transactionID)
	if !exists {
		return nil, nil, errors.New("transaction not found")
	}

	return tx.Request, tx.Response, nil
}

// GetStatistics 获取统计信息
func (ht *HTrack) GetStatistics() *Statistics {
	stats := ht.manager.GetStatistics()

	return &Statistics{
		TotalConnections:   stats.TotalConnections,
		ActiveConnections:  stats.ActiveConnections,
		TotalTransactions:  stats.TotalTransactions,
		ActiveTransactions: stats.ActiveTransactions,
		TotalRequests:      stats.TotalRequests,
		TotalResponses:     stats.TotalResponses,
		TotalBytes:         stats.TotalBytes,
		ErrorCount:         stats.ErrorCount,
		HTTP1Connections:   stats.HTTP1Connections,
		HTTP2Connections:   stats.HTTP2Connections,
		HTTP2Streams:       stats.HTTP2Streams,
	}
}

// CleanupExpired 手动清理过期连接和事务
func (ht *HTrack) CleanupExpired() {
	ht.manager.CleanupExpired()
}

// Close 关闭HTrack实例
func (ht *HTrack) Close() error {
	// 关闭channels
	ht.CloseChannels()

	return ht.manager.Close()
}

// 私有方法

// autoCleanup 自动清理过期连接和事务
func (ht *HTrack) autoCleanup() {
	ticker := time.NewTicker(ht.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		ht.manager.CleanupExpired()
	}
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		MaxSessions:        10000,
		MaxTransactions:    10000,
		SessionTimeout:     30 * time.Second,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024, // 64KB
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		AutoCleanup:        true,
		CleanupInterval:    5 * time.Minute,
		ChannelBufferSize:  100,
		EnableChannels:     true,
	}
}

// 便捷函数

// ProcessHTTPPacket 处理HTTP数据包的便捷函数
func ProcessHTTPPacket(sessionID string, data []byte, direction types.Direction) (*types.HTTPRequest, *types.HTTPResponse, error) {
	ht := New(nil)
	defer ht.Close()

	var request *types.HTTPRequest
	var response *types.HTTPResponse
	var parseError error

	// 设置事件处理器
	ht.SetEventHandlers(&EventHandlers{
		OnRequestParsed: func(req *types.HTTPRequest) {
			request = req
		},
		OnResponseParsed: func(resp *types.HTTPResponse) {
			response = resp
		},
		OnError: func(err error) {
			parseError = err
		},
	})

	// 处理数据包
	err := ht.ProcessPacket(sessionID, data, direction)
	if err != nil {
		return nil, nil, err
	}

	if parseError != nil {
		return nil, nil, parseError
	}

	return request, response, nil
}

// ParseHTTPMessage 解析单个HTTP消息的便捷函数
func ParseHTTPMessage(data []byte) (*types.HTTPRequest, *types.HTTPResponse, error) {
	return ProcessHTTPPacket("temp-session", data, types.DirectionRequest)
}

// setupChannelHandlers 设置内部channel事件处理器
func (ht *HTrack) setupChannelHandlers() {
	ht.SetEventHandlers(&EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			// 只有完整的请求才发送到channel
			if request != nil && request.Complete {
				select {
				case ht.RequestChan <- request:
					// 成功发送到channel
				default:
					// channel已满，丢弃数据（非阻塞）
				}
			}
		},
		OnResponseParsed: func(response *types.HTTPResponse) {
			// 只有完整的响应才发送到channel
			if response != nil && response.Complete {
				select {
				case ht.ResponseChan <- response:
					// 成功发送到channel
				default:
					// channel已满，丢弃数据（非阻塞）
				}
			}
		},
	})
}

// GetRequestChan 获取请求channel（只读）
func (ht *HTrack) GetRequestChan() <-chan *types.HTTPRequest {
	return ht.RequestChan
}

// GetResponseChan 获取响应channel（只读）
func (ht *HTrack) GetResponseChan() <-chan *types.HTTPResponse {
	return ht.ResponseChan
}

// CloseChannels 关闭所有channels
func (ht *HTrack) CloseChannels() {
	if ht.RequestChan != nil {
		close(ht.RequestChan)
		ht.RequestChan = nil
	}
	if ht.ResponseChan != nil {
		close(ht.ResponseChan)
		ht.ResponseChan = nil
	}
}
