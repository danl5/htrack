package types

// TCPTuple TCP四元组信息
type TCPTuple struct {
	SrcIP   string // 源IP地址
	SrcPort uint16 // 源端口
	DstIP   string // 目标IP地址
	DstPort uint16 // 目标端口
}

// PacketInfo 数据包信息
type PacketInfo struct {
	Data        []byte    // 原始数据
	Direction   Direction // 数据方向
	TCPTuple    *TCPTuple // TCP四元组信息
	TimeDiff    uint64    // 时间偏移
	PID         uint32    // 进程ID
	TID         uint32    // 线程ID
	ProcessName string    // 进程名称
}
