package tests

import (
	"testing"
	"time"

	"github.com/danl5/htrack/connection"
	"github.com/danl5/htrack/types"
)

// TestTLSIntegration 测试TLS协议的完整集成流程
func TestTLSIntegration(t *testing.T) {
	// 创建连接管理器
	manager := connection.NewManager(connection.DefaultConfig())
	defer manager.Close()

	// 设置回调函数来捕获解析结果
	var capturedRequests []*types.HTTPRequest
	var capturedResponses []*types.HTTPResponse
	var capturedTransactions []*connection.Transaction

	callbacks := &connection.Callbacks{
		OnRequestParsed: func(req *types.HTTPRequest) {
			t.Logf("Request parsed: %+v", req)
			capturedRequests = append(capturedRequests, req)
		},
		OnResponseParsed: func(resp *types.HTTPResponse) {
			t.Logf("Response parsed: %+v", resp)
			capturedResponses = append(capturedResponses, resp)
		},
		OnTransactionComplete: func(tx *connection.Transaction) {
			t.Logf("Transaction complete: %+v", tx)
			capturedTransactions = append(capturedTransactions, tx)
		},
	}
	manager.SetCallbacks(callbacks)

	// 创建TLS握手数据
	tlsClientHello := []byte{
		0x16, 0x03, 0x03, 0x00, 0x26, // TLS Handshake, TLS 1.2, Length: 38
		0x01, 0x00, 0x00, 0x22, // Client Hello, Length: 34
		0x03, 0x03, // TLS 1.2
		// Random (32 bytes)
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20,
	}

	tlsServerHello := []byte{
		0x16, 0x03, 0x03, 0x00, 0x16, // TLS Handshake, TLS 1.2, Length: 22
		0x02, 0x00, 0x00, 0x12, // Server Hello, Length: 18
		0x03, 0x03, // TLS 1.2
		// Random (16 bytes for simplicity)
		0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
		0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F, 0x30,
	}

	tlsApplicationData := []byte{
		0x17, 0x03, 0x03, 0x00, 0x10, // TLS Application Data, TLS 1.2, Length: 16
		0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
		0x49, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F, 0x50,
	}

	// 创建数据包信息
	clientPacketInfo := &types.PacketInfo{
		Data: tlsClientHello,
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.100",
			SrcPort: 54321,
			DstIP:   "192.168.1.200",
			DstPort: 443,
		},
		Direction: types.DirectionRequest,
		TimeDiff:  0,
	}

	serverPacketInfo := &types.PacketInfo{
		Data: tlsServerHello,
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.200",
			SrcPort: 443,
			DstIP:   "192.168.1.100",
			DstPort: 54321,
		},
		Direction: types.DirectionResponse,
		TimeDiff:  100,
	}

	appDataPacketInfo := &types.PacketInfo{
		Data: tlsApplicationData,
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.200",
			SrcPort: 443,
			DstIP:   "192.168.1.100",
			DstPort: 54321,
		},
		Direction: types.DirectionResponse,
		TimeDiff:  200,
	}

	// 生成连接ID
	connectionID := "192.168.1.100:54321-192.168.1.200:443"

	// 测试协议检测
	detectedVersion := manager.DetectHTTPVersion(clientPacketInfo.Data)
	t.Logf("Detected protocol version: %v", detectedVersion)

	// 处理客户端握手
	t.Logf("Processing client hello packet...")
	err := manager.ProcessPacket(connectionID, clientPacketInfo)
	if err != nil {
		t.Fatalf("Failed to process client hello: %v", err)
	}

	// 检查连接是否创建
	conn, exists := manager.GetConnection(connectionID)
	if exists {
		t.Logf("Connection created with version: %v", conn.Version)
	} else {
		t.Logf("Connection not found")
	}

	// 处理服务器握手
	t.Logf("Processing server hello packet...")
	err = manager.ProcessPacket(connectionID, serverPacketInfo)
	if err != nil {
		t.Fatalf("Failed to process server hello: %v", err)
	}

	// 处理应用数据
	t.Logf("Processing application data packet...")
	err = manager.ProcessPacket(connectionID, appDataPacketInfo)
	if err != nil {
		t.Fatalf("Failed to process application data: %v", err)
	}

	// 等待处理完成
	time.Sleep(100 * time.Millisecond)

	// 验证结果
	t.Logf("Captured requests: %d", len(capturedRequests))
	t.Logf("Captured responses: %d", len(capturedResponses))
	t.Logf("Captured transactions: %d", len(capturedTransactions))

	if len(capturedRequests) == 0 {
		t.Errorf("Expected at least one request to be processed")
	}
	if len(capturedResponses) == 0 {
		t.Errorf("Expected at least one response to be processed")
	}
	if len(capturedTransactions) == 0 {
		t.Errorf("Expected at least one transaction to be completed")
	}

	// 验证TLS特定的内容
	if len(capturedRequests) > 0 {
		req := capturedRequests[0]
		if req.Method != "TLS" {
			t.Errorf("Expected TLS method, got %s", req.Method)
		}
		if req.Proto != "TLS/Other" {
			t.Errorf("Expected TLS/Other protocol, got %s", req.Proto)
		}
	}

	if len(capturedResponses) > 0 {
		resp := capturedResponses[0]
		if resp.Status != "TLS Record" {
			t.Errorf("Expected TLS Record status, got %s", resp.Status)
		}
		if resp.Proto != "TLS/Other" {
			t.Errorf("Expected TLS/Other protocol, got %s", resp.Proto)
		}
	}

	// 验证请求
	if len(capturedRequests) > 0 {
		req := capturedRequests[0]
		if req.Method != "TLS" {
			t.Errorf("Expected TLS method, got %s", req.Method)
		}
		if req.Proto != "TLS/Other" {
			t.Errorf("Expected TLS/Other proto, got %s", req.Proto)
		}
		if !req.Complete {
			t.Error("TLS request should be marked as complete")
		}
		// 验证TLS特定头部
		if req.Headers["TLS-Record-Type"][0] != "22" { // Handshake
			t.Errorf("Expected TLS-Record-Type 22, got %s", req.Headers["TLS-Record-Type"][0])
		}
		if req.Headers["TLS-Version"][0] != "0x0303" { // TLS 1.2
			t.Errorf("Expected TLS-Version 0x0303, got %s", req.Headers["TLS-Version"][0])
		}
	}

	// 验证响应
	if len(capturedResponses) > 0 {
		resp := capturedResponses[0]
		if resp.Status != "TLS Record" {
			t.Errorf("Expected TLS Record status, got %s", resp.Status)
		}
		if resp.Proto != "TLS/Other" {
			t.Errorf("Expected TLS/Other proto, got %s", resp.Proto)
		}
		if !resp.Complete {
			t.Error("TLS response should be marked as complete")
		}
	}

	// 验证事务
	if len(capturedTransactions) == 0 {
		t.Error("No TLS transactions were captured")
	}

	// 验证连接统计
	stats := manager.GetStatistics()
	if stats.TotalConnections == 0 {
		t.Error("Expected at least one connection to be created")
	}
	if stats.TotalRequests == 0 {
		t.Error("Expected at least one request to be processed")
	}
	if stats.TotalResponses == 0 {
		t.Error("Expected at least one response to be processed")
	}
}

// TestTLSVersionDetection 测试TLS版本检测
func TestTLSVersionDetection(t *testing.T) {
	manager := connection.NewManager(connection.DefaultConfig())
	defer manager.Close()

	tests := []struct {
		name     string
		data     []byte
		expected types.HTTPVersion
	}{
		{
			name:     "TLS 1.2 Handshake",
			data:     []byte{0x16, 0x03, 0x03, 0x00, 0x10},
			expected: types.TLS_OTHER,
		},
		{
			name:     "TLS 1.3 Application Data",
			data:     []byte{0x17, 0x03, 0x04, 0x00, 0x08},
			expected: types.TLS_OTHER,
		},
		{
			name:     "HTTP/1.1 Request",
			data:     []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
			expected: types.HTTP11,
		},
		{
			name:     "Invalid Data",
			data:     []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			expected: types.Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := manager.DetectHTTPVersion(tt.data)
			if version != tt.expected {
				t.Errorf("DetectHTTPVersion() = %v, want %v", version, tt.expected)
			}
		})
	}
}

// TestTLSMultipleRecords 测试多个TLS记录的处理
func TestTLSMultipleRecords(t *testing.T) {
	manager := connection.NewManager(connection.DefaultConfig())
	defer manager.Close()

	var capturedRequests []*types.HTTPRequest
	callbacks := &connection.Callbacks{
		OnRequestParsed: func(req *types.HTTPRequest) {
			capturedRequests = append(capturedRequests, req)
		},
	}
	manager.SetCallbacks(callbacks)

	// 创建包含多个TLS记录的数据
	multipleRecords := []byte{
		// 第一个记录：Handshake
		0x16, 0x03, 0x03, 0x00, 0x04,
		0x01, 0x02, 0x03, 0x04,
		// 第二个记录：Application Data
		0x17, 0x03, 0x03, 0x00, 0x08,
		0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C,
	}

	packetInfo := &types.PacketInfo{
		Data: multipleRecords,
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.1",
			SrcPort: 12345,
			DstIP:   "192.168.1.2",
			DstPort: 443,
		},
		Direction: types.DirectionRequest,
		TimeDiff:  0,
	}

	// 生成连接ID
	connectionID := "192.168.1.1:12345-192.168.1.2:443"

	err := manager.ProcessPacket(connectionID, packetInfo)
	if err != nil {
		t.Fatalf("Failed to process multiple TLS records: %v", err)
	}

	// 等待处理完成
	time.Sleep(100 * time.Millisecond)

	// 验证解析了两个记录
	if len(capturedRequests) != 2 {
		t.Errorf("Expected 2 TLS requests, got %d", len(capturedRequests))
	}

	if len(capturedRequests) >= 2 {
		// 验证第一个记录
		if capturedRequests[0].Headers["TLS-Record-Type"][0] != "22" {
			t.Errorf("First record should be Handshake (22), got %s", capturedRequests[0].Headers["TLS-Record-Type"][0])
		}
		// 验证第二个记录
		if capturedRequests[1].Headers["TLS-Record-Type"][0] != "23" {
			t.Errorf("Second record should be Application Data (23), got %s", capturedRequests[1].Headers["TLS-Record-Type"][0])
		}
	}
}