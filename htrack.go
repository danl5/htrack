package htrack

import (
	"errors"
	"time"

	"github.com/danl5/htrack/connection"
	"github.com/danl5/htrack/parser"
	"github.com/danl5/htrack/types"
)

// HTrack HTTP协议数据解析器
type HTrack struct {
	manager *connection.Manager
	config  *Config

	// 直接解析器（用于无sessionID的直接解析模式）
	parsers map[types.HTTPVersion]parser.Parser
	
	// 事件处理器
	eventHandlers *EventHandlers

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
		parsers: make(map[types.HTTPVersion]parser.Parser),
	}

	// 初始化解析器
	ht.initParsers(config)

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
//           如果为空字符串，则启用直接解析模式，跳过数据包重组
// packetInfo: 包含数据、方向和TCP四元组信息的数据包信息
func (ht *HTrack) ProcessPacket(sessionID string, packetInfo *types.PacketInfo) error {
	if len(packetInfo.Data) == 0 {
		return errors.New("empty packet data")
	}

	// 如果sessionID为空，启用直接解析模式
	if sessionID == "" {
		return ht.ProcessPacketDirect(packetInfo)
	}

	// 否则使用现有的连接重组逻辑
	return ht.manager.ProcessPacket(sessionID, packetInfo)
}

// SetEventHandlers 设置事件处理器
func (ht *HTrack) SetEventHandlers(handlers *EventHandlers) {
	if handlers == nil {
		return
	}

	// 保存事件处理器引用（用于直接解析模式）
	ht.eventHandlers = handlers

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
func ProcessHTTPPacket(sessionID string, packetInfo *types.PacketInfo) (*types.HTTPRequest, *types.HTTPResponse, error) {
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
	err := ht.ProcessPacket(sessionID, packetInfo)
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
	return ProcessHTTPPacket("temp-session", &types.PacketInfo{
		Data:      data,
		Direction: types.DirectionRequest,
		TCPTuple:  &types.TCPTuple{},
	})
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

// initParsers 初始化解析器
func (ht *HTrack) initParsers(config *Config) {
	if config.EnableHTTP1 {
		ht.parsers[types.HTTP10] = parser.NewHTTP1ParserWithVersion(types.HTTP10)
		ht.parsers[types.HTTP11] = parser.NewHTTP1ParserWithVersion(types.HTTP11)
	}
	if config.EnableHTTP2 {
		ht.parsers[types.HTTP2] = parser.NewHTTP2Parser()
	}
	// TLS解析器总是启用
	ht.parsers[types.TLS_OTHER] = parser.NewTLSGenericParser()
}

// ProcessPacketDirect 直接解析数据包（无连接重组）
func (ht *HTrack) ProcessPacketDirect(packetInfo *types.PacketInfo) error {
	// 检测HTTP版本
	version := ht.detectHTTPVersion(packetInfo.Data)
	if version == types.Unknown {
		return errors.New("unknown HTTP version")
	}

	// 获取对应的解析器
	parser, exists := ht.parsers[version]
	if !exists {
		return errors.New("unsupported HTTP version")
	}

	// 根据数据包方向进行解析
	switch packetInfo.Direction {
	case types.DirectionClientToServer:
		return ht.parseDirectRequest(parser, packetInfo)
	case types.DirectionServerToClient:
		return ht.parseDirectResponse(parser, packetInfo)
	default:
		// 如果方向未知，尝试两种解析方式
		if err := ht.parseDirectRequest(parser, packetInfo); err == nil {
			return nil
		}
		return ht.parseDirectResponse(parser, packetInfo)
	}
}

// parseDirectRequest 直接解析请求
func (ht *HTrack) parseDirectRequest(parser parser.Parser, packetInfo *types.PacketInfo) error {
	requests, err := parser.ParseRequest("direct-session", packetInfo.Data, packetInfo)
	if err != nil {
		return err
	}

	// 触发事件回调
	for _, request := range requests {
		if request != nil {
			// 触发请求解析事件
			if ht.eventHandlers != nil && ht.eventHandlers.OnRequestParsed != nil {
				ht.eventHandlers.OnRequestParsed(request)
			}

			// 发送到channel（如果启用）
			if ht.RequestChan != nil && request.Complete {
				select {
				case ht.RequestChan <- request:
					// 成功发送
				default:
					// channel已满，丢弃数据
				}
			}
		}
	}

	return nil
}

// parseDirectResponse 直接解析响应
func (ht *HTrack) parseDirectResponse(parser parser.Parser, packetInfo *types.PacketInfo) error {
	responses, err := parser.ParseResponse("direct-session", packetInfo.Data, packetInfo)
	if err != nil {
		return err
	}

	// 触发事件回调
	for _, response := range responses {
		if response != nil {
			// 触发响应解析事件
			if ht.eventHandlers != nil && ht.eventHandlers.OnResponseParsed != nil {
				ht.eventHandlers.OnResponseParsed(response)
			}

			// 发送到channel（如果启用）
			if ht.ResponseChan != nil && response.Complete {
				select {
				case ht.ResponseChan <- response:
					// 成功发送
				default:
					// channel已满，丢弃数据
				}
			}
		}
	}

	return nil
}

// detectHTTPVersion 检测HTTP版本
func (ht *HTrack) detectHTTPVersion(data []byte) types.HTTPVersion {
	// 按优先级检测版本
	for _, parser := range ht.parsers {
		if detectedVersion := parser.DetectVersion(data); detectedVersion != types.Unknown {
			return detectedVersion
		}
	}
	return types.Unknown
}
