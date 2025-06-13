package tests

import (
	"testing"

	"github.com/danl5/htrack"
	"github.com/danl5/htrack/types"
)

// TestProcessInfoPropagation 测试进程信息传递
func TestProcessInfoPropagation(t *testing.T) {
	// 创建HTrack实例
	config := &htrack.Config{
		MaxSessions:        100,
		MaxTransactions:    1000,
		SessionTimeout:     300000000000, // 5分钟
		TransactionTimeout: 30000000000,  // 30秒
		BufferSize:         8192,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		AutoCleanup:        false,
		EnableChannels:     false,
	}
	ht := htrack.New(config)

	// 设置回调函数来验证进程信息
	var capturedRequest *types.HTTPRequest
	var capturedResponse *types.HTTPResponse

	handlers := &htrack.EventHandlers{
		OnRequestParsed: func(req *types.HTTPRequest) {
			capturedRequest = req
		},
		OnResponseParsed: func(resp *types.HTTPResponse) {
			capturedResponse = resp
		},
	}
	ht.SetEventHandlers(handlers)

	// 测试HTTP/1.1请求
	httpRequest := "GET /test HTTP/1.1\r\nHost: example.com\r\n\r\n"
	packetInfo := &types.PacketInfo{
		Data:        []byte(httpRequest),
		Direction:   types.DirectionRequest,
		TCPTuple:    &types.TCPTuple{SrcIP: "192.168.1.1", SrcPort: 12345, DstIP: "192.168.1.2", DstPort: 80},
		TimeDiff:    0,
		PID:         1234,
		ProcessName: "test-process",
	}

	err := ht.ProcessPacket("test-session", packetInfo)
	if err != nil {
		t.Fatalf("处理请求包失败: %v", err)
	}

	// 验证请求中的进程信息
	if capturedRequest == nil {
		t.Fatal("未捕获到请求")
	}

	if capturedRequest.PID != 1234 {
		t.Errorf("期望PID为1234，实际为%d", capturedRequest.PID)
	}

	if capturedRequest.ProcessName != "test-process" {
		t.Errorf("期望进程名为'test-process'，实际为'%s'", capturedRequest.ProcessName)
	}

	// 测试HTTP/1.1响应
	httpResponse := "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nHello"
	responsePacketInfo := &types.PacketInfo{
		Data:        []byte(httpResponse),
		Direction:   types.DirectionResponse,
		TCPTuple:    &types.TCPTuple{SrcIP: "192.168.1.2", SrcPort: 80, DstIP: "192.168.1.1", DstPort: 12345},
		TimeDiff:    100,
		PID:         5678,
		ProcessName: "server-process",
	}

	err = ht.ProcessPacket("test-session", responsePacketInfo)
	if err != nil {
		t.Fatalf("处理响应包失败: %v", err)
	}

	// 验证响应中的进程信息
	if capturedResponse == nil {
		t.Fatal("未捕获到响应")
	}

	if capturedResponse.PID != 5678 {
		t.Errorf("期望PID为5678，实际为%d", capturedResponse.PID)
	}

	if capturedResponse.ProcessName != "server-process" {
		t.Errorf("期望进程名为'server-process'，实际为'%s'", capturedResponse.ProcessName)
	}

	t.Logf("进程信息测试通过 - 请求PID: %d, 进程名: %s; 响应PID: %d, 进程名: %s",
		capturedRequest.PID, capturedRequest.ProcessName,
		capturedResponse.PID, capturedResponse.ProcessName)
}