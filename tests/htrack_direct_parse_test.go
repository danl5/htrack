package tests

import (
	"sync"
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
	count := 0

	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			count += 1
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

	if count != 1 {
		t.Errorf("期望OnRequestParsed被调用一次，实际为%d", count)
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
	count := 0

	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			// 不处理请求
		},
		OnResponseParsed: func(response *types.HTTPResponse) {
			count += 1
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

	if count != 1 {
		t.Errorf("期望OnResponseParsed被调用一次，实际为%d", count)
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

// TestDirectParseWithChannel 测试通过channel输出的直接解析
func TestDirectParseWithChannel(t *testing.T) {
	// 创建配置，启用channel输出
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:        10000,
		MaxTransactions:    10000,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		EnableChannels:     true, // 启用channel输出
		ChannelBufferSize:  10,   // 设置缓冲区大小
	})
	defer hTrack.Close()

	// 设置事件处理器
	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			t.Logf("事件处理器收到请求: %s %s, User-Agent: %s", request.Method, request.URL.Path, request.Headers.Get("User-Agent"))
		},
		OnResponseParsed: func(response *types.HTTPResponse) {
			// 不在这里处理，让channel处理
		},
		OnError: func(err error) {
			t.Errorf("解析错误: %v", err)
		},
	})

	// 用于同步的WaitGroup
	var wg sync.WaitGroup
	var requestReceived, responseReceived bool
	var receivedRequest *types.HTTPRequest
	var receivedResponse *types.HTTPResponse
	var reqCount, respCount int

	// 启动goroutine监听请求channel
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case request := <-hTrack.GetRequestChan():
			reqCount += 1
			requestReceived = true
			receivedRequest = request
			t.Logf("收到请求: %s %s", request.Method, request.URL.String())
		case <-time.After(5 * time.Second):
			t.Error("超时：未收到请求")
		}
	}()

	// 启动goroutine监听响应channel
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case response := <-hTrack.GetResponseChan():
			respCount += 1
			responseReceived = true
			receivedResponse = response
			t.Logf("收到响应: %d %s", response.StatusCode, response.Status)
		case <-time.After(5 * time.Second):
			t.Error("超时：未收到响应")
		}
	}()

	// HTTP/1.1 GET请求数据
	requestData := []byte("GET /api/channel-test HTTP/1.1\r\nHost: example.com\r\nUser-Agent: channel-test-agent\r\nContent-Length: 0\r\n\r\n")

	// 创建请求数据包信息
	requestPacketInfo := &types.PacketInfo{
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
	err := hTrack.ProcessPacket("", requestPacketInfo)
	if err != nil {
		t.Fatalf("直接解析请求失败: %v", err)
	}

	// HTTP/1.1响应数据
	responseData := []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 21\r\n\r\n{\"message\":\"success\"}")

	// 创建响应数据包信息
	responsePacketInfo := &types.PacketInfo{
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
	err = hTrack.ProcessPacket("", responsePacketInfo)
	if err != nil {
		t.Fatalf("直接解析响应失败: %v", err)
	}

	// 等待所有goroutine完成
	wg.Wait()

	// 验证结果
	if !requestReceived {
		t.Error("未收到请求数据")
	}
	if !responseReceived {
		t.Error("未收到响应数据")
	}

	// 验证请求内容
	if receivedRequest != nil {
		if receivedRequest.Method != "GET" {
			t.Errorf("期望方法为GET，实际为: %s", receivedRequest.Method)
		}
		if receivedRequest.URL.Path != "/api/channel-test" {
			t.Errorf("期望路径为/api/channel-test，实际为: %s", receivedRequest.URL.Path)
		}
		if receivedRequest.Headers.Get("Host") != "example.com" {
			t.Errorf("期望Host为example.com，实际为%s", receivedRequest.Headers.Get("Host"))
		}
		if receivedRequest.Headers.Get("User-Agent") != "channel-test-agent" {
			t.Errorf("期望User-Agent为channel-test-agent，实际为%s", receivedRequest.Headers.Get("User-Agent"))
		}
		if !receivedRequest.Complete {
			t.Error("请求应该是完整的")
		}
		if reqCount != 1 {
			t.Errorf("期望收到1个请求，实际为: %d", reqCount)
		}
	}

	// 验证响应内容
	if receivedResponse != nil {
		if receivedResponse.StatusCode != 200 {
			t.Errorf("期望状态码为200，实际为: %d", receivedResponse.StatusCode)
		}
		if receivedResponse.Status != "OK" {
			t.Errorf("期望状态为OK，实际为: %s", receivedResponse.Status)
		}
		if receivedResponse.Headers.Get("Content-Type") != "application/json" {
			t.Errorf("期望Content-Type为application/json，实际为%s", receivedResponse.Headers.Get("Content-Type"))
		}
		if !receivedResponse.Complete {
			t.Error("响应应该是完整的")
		}
		expectedBody := "{\"message\":\"success\"}"
		if string(receivedResponse.Body) != expectedBody {
			t.Errorf("期望响应体为%s，实际为: %s", expectedBody, string(receivedResponse.Body))
		}
		if respCount != 1 {
			t.Errorf("期望收到1个响应，实际为: %d", respCount)
		}
	}

	t.Logf("✅ 通过channel的直接解析成功")
}

// TestDirectParseWithChannelMultipleRequests 测试通过channel输出多个请求的直接解析
func TestDirectParseWithChannelMultipleRequests(t *testing.T) {
	// 创建配置，启用channel输出
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:        10000,
		MaxTransactions:    10000,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		EnableChannels:     true,  // 启用channel输出
		ChannelBufferSize:  20,    // 增大缓冲区以处理多个请求
	})
	defer hTrack.Close()

	// 设置事件处理器
	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			t.Logf("事件处理器收到请求: %s %s, User-Agent: %s", request.Method, request.URL.Path, request.Headers.Get("User-Agent"))
		},
		OnResponseParsed: func(response *types.HTTPResponse) {
			// 不在这里处理，让channel处理
		},
		OnError: func(err error) {
			t.Errorf("解析错误: %v", err)
		},
	})

	// 用于同步的WaitGroup
	var wg sync.WaitGroup
	var mu sync.Mutex
	receivedRequests := make([]*types.HTTPRequest, 0, 2)
	reqCount := 0

	// 启动goroutine监听请求channel
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2; i++ {
			select {
			case request := <-hTrack.GetRequestChan():
				mu.Lock()
				receivedRequests = append(receivedRequests, request)
				reqCount++
				mu.Unlock()
				t.Logf("收到第%d个请求: %s %s, User-Agent: %s, Content-Type: %s, Body: %s", 
					reqCount, request.Method, request.URL.String(), 
					request.Headers.Get("User-Agent"), request.Headers.Get("Content-Type"), string(request.Body))
			case <-time.After(5 * time.Second):
				t.Errorf("超时：未收到第%d个请求", i+1)
				return
			}
		}
	}()

	// 发送第一个HTTP请求
	requestData1 := []byte("GET /api/first HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test-agent-1\r\nContent-Length: 0\r\n\r\n")
	requestPacketInfo1 := &types.PacketInfo{
		Data:      requestData1,
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
		ProcessName: "test-process-1",
	}

	// 使用空sessionID进行直接解析
	t.Logf("发送第一个请求: %s", string(requestData1[:50]))
	err := hTrack.ProcessPacket("", requestPacketInfo1)
	if err != nil {
		t.Fatalf("直接解析第一个请求失败: %v", err)
	}

	// 发送第二个HTTP请求
	requestData2 := []byte("POST /api/second HTTP/1.1\r\nHost: api.example.com\r\nUser-Agent: test-agent-2\r\nContent-Type: application/json\r\nContent-Length: 15\r\n\r\n{\"data\":\"test\"}") 
	requestPacketInfo2 := &types.PacketInfo{
		Data:      requestData2,
		Direction: types.DirectionClientToServer,
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.101",
			SrcPort: 12346,
			DstIP:   "192.168.1.200",
			DstPort: 80,
		},
		TimeDiff:    50,
		PID:         1235,
		TID:         5679,
		ProcessName: "test-process-2",
	}

	// 使用空sessionID进行直接解析
	t.Logf("发送第二个请求: %s", string(requestData2[:50]))
	err = hTrack.ProcessPacket("", requestPacketInfo2)
	if err != nil {
		t.Fatalf("直接解析第二个请求失败: %v", err)
	}

	// 等待所有goroutine完成
	wg.Wait()

	// 验证结果
	mu.Lock()
	defer mu.Unlock()

	if reqCount != 2 {
		t.Errorf("期望收到2个请求，实际收到%d个", reqCount)
	}

	if len(receivedRequests) != 2 {
		t.Errorf("期望receivedRequests长度为2，实际为%d", len(receivedRequests))
	}

	// 验证第一个请求
	if len(receivedRequests) > 0 {
		req1 := receivedRequests[0]
		if req1.Method != "GET" {
			t.Errorf("第一个请求期望方法为GET，实际为: %s", req1.Method)
		}
		if req1.URL.Path != "/api/first" {
			t.Errorf("第一个请求期望路径为/api/first，实际为: %s", req1.URL.Path)
		}
		if req1.Headers.Get("User-Agent") != "test-agent-1" {
			t.Errorf("第一个请求期望User-Agent为test-agent-1，实际为%s", req1.Headers.Get("User-Agent"))
		}
		if !req1.Complete {
			t.Error("第一个请求应该是完整的")
		}
	}

	// 验证第二个请求
	if len(receivedRequests) > 1 {
		req2 := receivedRequests[1]
		if req2.Method != "POST" {
			t.Errorf("第二个请求期望方法为POST，实际为: %s", req2.Method)
		}
		if req2.URL.Path != "/api/second" {
			t.Errorf("第二个请求期望路径为/api/second，实际为: %s", req2.URL.Path)
		}
		if req2.Headers.Get("User-Agent") != "test-agent-2" {
			t.Errorf("第二个请求期望User-Agent为test-agent-2，实际为%s", req2.Headers.Get("User-Agent"))
		}
		if req2.Headers.Get("Content-Type") != "application/json" {
			t.Errorf("第二个请求期望Content-Type为application/json，实际为%s", req2.Headers.Get("Content-Type"))
		}
		expectedBody := "{\"data\":\"test\"}"
		if string(req2.Body) != expectedBody {
			t.Errorf("第二个请求期望Body为%s，实际为: %s", expectedBody, string(req2.Body))
		}
		if !req2.Complete {
			t.Error("第二个请求应该是完整的")
		}
	}

	t.Logf("✅ 通过channel的多个请求直接解析成功")
}
