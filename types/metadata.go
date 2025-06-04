package types

import (
	"net"
	"time"
)

// HTTPVersion 表示HTTP协议版本
type HTTPVersion int

const (
	HTTP10 HTTPVersion = iota
	HTTP11
	HTTP2
	HTTP3
	Unknown
)

func (v HTTPVersion) String() string {
	switch v {
	case HTTP10:
		return "HTTP/1.0"
	case HTTP11:
		return "HTTP/1.1"
	case HTTP2:
		return "HTTP/2"
	default:
		return "Unknown"
	}
}

// ConnectionMetadata 连接元数据
type ConnectionMetadata struct {
	ID         string      // 连接唯一标识
	LocalAddr  net.Addr    // 本地地址
	RemoteAddr net.Addr    // 远程地址
	StartTime  time.Time   // 连接开始时间
	Version    HTTPVersion // HTTP版本
	IsSecure   bool        // 是否为HTTPS
	ServerName string      // 服务器名称（SNI）
}

// StreamMetadata HTTP/2流元数据
type StreamMetadata struct {
	StreamID   uint32      // 流ID
	ParentID   uint32      // 父流ID
	Weight     uint8       // 权重
	Dependency uint32      // 依赖流ID
	Exclusive  bool        // 是否独占依赖
	State      StreamState // 流状态
	CreatedAt  time.Time   // 创建时间
	ClosedAt   *time.Time  // 关闭时间
}

// StreamState HTTP/2流状态
type StreamState int

const (
	StreamIdle StreamState = iota
	StreamReservedLocal
	StreamReservedRemote
	StreamOpen
	StreamHalfClosedLocal
	StreamHalfClosedRemote
	StreamClosed
)

func (s StreamState) String() string {
	switch s {
	case StreamIdle:
		return "idle"
	case StreamReservedLocal:
		return "reserved_local"
	case StreamReservedRemote:
		return "reserved_remote"
	case StreamOpen:
		return "open"
	case StreamHalfClosedLocal:
		return "half_closed_local"
	case StreamHalfClosedRemote:
		return "half_closed_remote"
	case StreamClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// PacketMetadata 数据包元数据
type PacketMetadata struct {
	SequenceNum uint64    // 序列号
	Timestamp   time.Time // 时间戳
	Size        int       // 数据包大小
	Direction   Direction // 数据方向
	Fragmented  bool      // 是否分片
	Reassembled bool      // 是否已重组
}

// Direction 数据传输方向
type Direction int

const (
	DirectionClientToServer Direction = iota
	DirectionServerToClient
	DirectionRequest  = DirectionClientToServer
	DirectionResponse = DirectionServerToClient
)

func (d Direction) String() string {
	switch d {
	case DirectionClientToServer:
		return "client_to_server"
	case DirectionServerToClient:
		return "server_to_client"
	default:
		return "unknown"
	}
}

// ConnectionState 连接状态
type ConnectionState int

const (
	ConnectionStateIdle ConnectionState = iota
	ConnectionStateConnecting
	ConnectionStateEstablished
	ConnectionStateClosing
	ConnectionStateClosed
	ConnectionStateError
)

func (s ConnectionState) String() string {
	switch s {
	case ConnectionStateIdle:
		return "idle"
	case ConnectionStateConnecting:
		return "connecting"
	case ConnectionStateEstablished:
		return "established"
	case ConnectionStateClosing:
		return "closing"
	case ConnectionStateClosed:
		return "closed"
	case ConnectionStateError:
		return "error"
	default:
		return "unknown"
	}
}

// TransactionState 事务状态
type TransactionState int

const (
	TransactionStateIdle TransactionState = iota
	TransactionStateRequestReceived
	TransactionStateRequestComplete
	TransactionStateResponseStarted
	TransactionStateResponseComplete
	TransactionStateCompleted
	TransactionStateTimeout
	TransactionStateError
)

func (s TransactionState) String() string {
	switch s {
	case TransactionStateIdle:
		return "idle"
	case TransactionStateRequestReceived:
		return "request_received"
	case TransactionStateRequestComplete:
		return "request_complete"
	case TransactionStateResponseStarted:
		return "response_started"
	case TransactionStateResponseComplete:
		return "response_complete"
	case TransactionStateCompleted:
		return "completed"
	case TransactionStateTimeout:
		return "timeout"
	case TransactionStateError:
		return "error"
	default:
		return "unknown"
	}
}

// TransactionMetadata 事务元数据
type TransactionMetadata struct {
	ID             string          // 事务唯一标识
	ConnectionID   string          // 所属连接ID
	StreamID       *uint32         // HTTP/2流ID（可选）
	StartTime      time.Time       // 事务开始时间
	EndTime        *time.Time      // 事务结束时间
	RequestTime    *time.Time      // 请求完成时间
	ResponseTime   *time.Time      // 响应开始时间
	Duration       *time.Duration  // 总耗时
	RequestSize    int64           // 请求大小
	ResponseSize   int64           // 响应大小
	PacketCount    int             // 数据包数量
	ReassemblyInfo *ReassemblyInfo // 重组信息
}

// ReassemblyInfo 数据包重组信息
type ReassemblyInfo struct {
	TotalPackets    int       // 总数据包数
	ReceivedPackets int       // 已接收数据包数
	MissingPackets  []uint64  // 缺失的数据包序列号
	OutOfOrder      bool      // 是否有乱序
	Reassembled     bool      // 是否完成重组
	LastUpdate      time.Time // 最后更新时间
}
