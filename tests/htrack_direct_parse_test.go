package tests

import (
	"testing"
	"time"

	htrack "github.com/danl5/htrack"
	"github.com/danl5/htrack/types"
)

// TestDirectParseHTTP1Request 测试HTTP/1.1请求的直接解析
func TestDirectParseHTTP1Request(t *testing.T) {
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:        10000,
		MaxTransactions:    10000,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		EnableChannels:     false, // 使用事件处理器而不是channel
	})

	// 设置事件处理器
	var parsedRequest *types.HTTPRequest

	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			parsedRequest = request
		},
		OnResponseParsed: func(response *types.HTTPResponse) {
			// 不处理响应
		},
		OnError: func(err error) {
			// 记录错误但不存储
		},
	})

	// HTTP/1.1 GET请求数据
	requestData := []byte("GET /api/test HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test-agent\r\nContent-Length: 0\r\n\r\n")

	// 创建数据包信息
	packetInfo := &types.PacketInfo{
		Data:      requestData,
		Direction: types.DirectionClientToServer,
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.100",
			SrcPort: 12345,
			DstIP:   "192.168.1.200",
			DstPort: 80,
		},
		TimeDiff:    0,
		PID:         1234,
		TID:         5678,
		ProcessName: "test-process",
	}

	// 使用空sessionID进行直接解析
	err := hTrack.ProcessPacket("", packetInfo)
	if err != nil {
		t.Fatalf("直接解析失败: %v", err)
	}

	// 验证解析结果
	if parsedRequest == nil {
		t.Fatal("请求解析失败，parsedRequest为nil")
	}

	if parsedRequest.Method != "GET" {
		t.Errorf("期望Method为GET，实际为%s", parsedRequest.Method)
	}

	if parsedRequest.URL == nil || parsedRequest.URL.Path != "/api/test" {
		t.Errorf("期望Path为/api/test，实际为%v", parsedRequest.URL)
	}

	if parsedRequest.Headers.Get("Host") != "example.com" {
		t.Errorf("期望Host为example.com，实际为%s", parsedRequest.Headers.Get("Host"))
	}

	if parsedRequest.Headers.Get("User-Agent") != "test-agent" {
		t.Errorf("期望User-Agent为test-agent，实际为%s", parsedRequest.Headers.Get("User-Agent"))
	}

	// parseError在事件处理器中处理

	t.Logf("✅ HTTP/1.1请求直接解析成功: %s %s", parsedRequest.Method, parsedRequest.URL.Path)
}

// TestDirectParseHTTP1Response 测试HTTP/1.1响应的直接解析
func TestDirectParseHTTP1Response(t *testing.T) {
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:        10000,
		MaxTransactions:    10000,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		EnableChannels:     false,
	})

	// 设置事件处理器
	var parsedResponse *types.HTTPResponse

	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			// 不处理请求
		},
		OnResponseParsed: func(response *types.HTTPResponse) {
			parsedResponse = response
		},
		OnError: func(err error) {
			// 记录错误但不存储
		},
	})

	// HTTP/1.1响应数据
	responseData := []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 15\r\n\r\n{\"status\":\"ok\"}")

	// 创建数据包信息
	packetInfo := &types.PacketInfo{
		Data:      responseData,
		Direction: types.DirectionServerToClient,
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.200",
			SrcPort: 80,
			DstIP:   "192.168.1.100",
			DstPort: 12345,
		},
		TimeDiff:    100,
		PID:         1234,
		TID:         5678,
		ProcessName: "test-server",
	}

	// 使用空sessionID进行直接解析
	err := hTrack.ProcessPacket("", packetInfo)
	if err != nil {
		t.Fatalf("直接解析失败: %v", err)
	}

	// 验证解析结果
	if parsedResponse == nil {
		t.Fatal("响应解析失败，parsedResponse为nil")
	}

	if parsedResponse.StatusCode != 200 {
		t.Errorf("期望StatusCode为200，实际为%d", parsedResponse.StatusCode)
	}

	if parsedResponse.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("期望Content-Type为application/json，实际为%s", parsedResponse.Headers.Get("Content-Type"))
	}

	expectedBody := "{\"status\":\"ok\"}"
	if string(parsedResponse.Body) != expectedBody {
		t.Errorf("期望Body为%s，实际为%s", expectedBody, string(parsedResponse.Body))
	}

	// parseError在事件处理器中处理

	t.Logf("✅ HTTP/1.1响应直接解析成功: %d %s", parsedResponse.StatusCode, parsedResponse.Status)
}

// TestDirectParseVsSessionParse 测试直接解析与会话解析的对比
func TestDirectParseVsSessionParse(t *testing.T) {
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:        10000,
		MaxTransactions:    10000,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		EnableChannels:     false,
	})

	// 设置事件处理器
	var directRequest, sessionRequest *types.HTTPRequest

	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			if directRequest == nil {
				directRequest = request
			} else {
				sessionRequest = request
			}
		},
		OnResponseParsed: func(response *types.HTTPResponse) {},
		OnError: func(err error) {
			// 记录错误但不存储
		},
	})

	// HTTP/1.1请求数据
	requestData := []byte("GET /test HTTP/1.1\r\nHost: example.com\r\n\r\n")

	packetInfo := &types.PacketInfo{
		Data:      requestData,
		Direction: types.DirectionClientToServer,
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.100",
			SrcPort: 12345,
			DstIP:   "192.168.1.200",
			DstPort: 80,
		},
	}

	// 1. 直接解析（空sessionID）
	err := hTrack.ProcessPacket("", packetInfo)
	if err != nil {
		t.Fatalf("直接解析失败: %v", err)
	}

	// 2. 会话解析（非空sessionID）
	err = hTrack.ProcessPacket("session-123", packetInfo)
	if err != nil {
		t.Fatalf("会话解析失败: %v", err)
	}

	// 验证两种解析方式都成功
	if directRequest == nil {
		t.Fatal("直接解析失败，directRequest为nil")
	}

	if sessionRequest == nil {
		t.Fatal("会话解析失败，sessionRequest为nil")
	}

	// 验证解析结果一致性
	if directRequest.Method != sessionRequest.Method {
		t.Errorf("Method不一致: 直接解析=%s, 会话解析=%s", directRequest.Method, sessionRequest.Method)
	}

	if directRequest.URL.Path != sessionRequest.URL.Path {
		t.Errorf("Path不一致: 直接解析=%s, 会话解析=%s", directRequest.URL.Path, sessionRequest.URL.Path)
	}

	// 错误处理在事件处理器中完成

	t.Logf("✅ 直接解析与会话解析结果一致")
}

// TestDirectParseInvalidData 测试直接解析无效数据
func TestDirectParseInvalidData(t *testing.T) {
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:        10000,
		MaxTransactions:    10000,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		EnableChannels:     false,
	})

	// 设置事件处理器
	var parsedRequest *types.HTTPRequest

	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			parsedRequest = request
		},
		OnResponseParsed: func(response *types.HTTPResponse) {},
		OnError: func(err error) {
			// 记录错误但不存储
		},
	})

	// 无效的HTTP数据
	invalidData := []byte("这不是有效的HTTP数据")

	packetInfo := &types.PacketInfo{
		Data:      invalidData,
		Direction: types.DirectionClientToServer,
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.100",
			SrcPort: 12345,
			DstIP:   "192.168.1.200",
			DstPort: 80,
		},
	}

	// 使用空sessionID进行直接解析
	err := hTrack.ProcessPacket("", packetInfo)

	// 对于无效数据，应该能够优雅处理（不崩溃）
	if err != nil {
		t.Logf("预期的解析错误: %v", err)
	}

	// 验证没有解析出有效请求
	if parsedRequest != nil {
		t.Errorf("不应该解析出有效请求，但得到了: %+v", parsedRequest)
	}

	t.Logf("✅ 无效数据处理正常")
}

// TestDirectParseEmptyData 测试直接解析空数据
func TestDirectParseEmptyData(t *testing.T) {
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:        10000,
		MaxTransactions:    10000,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		EnableChannels:     false,
	})

	// 设置事件处理器
	var parsedRequest *types.HTTPRequest

	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			parsedRequest = request
		},
		OnResponseParsed: func(response *types.HTTPResponse) {},
		OnError: func(err error) {
			// 记录错误但不存储
		},
	})

	// 空数据
	emptyData := []byte{}

	packetInfo := &types.PacketInfo{
		Data:      emptyData,
		Direction: types.DirectionClientToServer,
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.100",
			SrcPort: 12345,
			DstIP:   "192.168.1.200",
			DstPort: 80,
		},
	}

	// 使用空sessionID进行直接解析
	err := hTrack.ProcessPacket("", packetInfo)

	// 空数据应该返回错误
	if err == nil {
		t.Error("空数据应该返回错误")
	} else {
		t.Logf("预期的空数据错误: %v", err)
	}

	// 验证没有解析出请求
	if parsedRequest != nil {
		t.Errorf("不应该解析出请求，但得到了: %+v", parsedRequest)
	}

	t.Logf("✅ 空数据处理正常")
}