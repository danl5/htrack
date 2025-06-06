package tests

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/danl5/htrack"
	"github.com/danl5/htrack/types"
)

// TestHTrackChannelBasic 测试HTrack基本的channel功能
func TestHTrackChannelBasic(t *testing.T) {
	// 创建配置，启用channel输出
	config := &htrack.Config{
		MaxSessions:        100,
		MaxTransactions:    100,
		TransactionTimeout: 30 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        false, // 暂时只测试HTTP/1.1
		AutoCleanup:        false, // 测试期间不自动清理
		ChannelBufferSize:  10,
		EnableChannels:     true,
	}

	// 创建HTrack实例
	ht := htrack.New(config)
	defer ht.Close()

	// 用于同步的WaitGroup
	var wg sync.WaitGroup
	var requestReceived, responseReceived bool
	var receivedRequest *types.HTTPRequest
	var receivedResponse *types.HTTPResponse

	// 启动goroutine监听请求channel
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case request := <-ht.GetRequestChan():
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
		case response := <-ht.GetResponseChan():
			responseReceived = true
			receivedResponse = response
			t.Logf("收到响应: %d %s", response.StatusCode, response.Status)
		case <-time.After(5 * time.Second):
			t.Error("超时：未收到响应")
		}
	}()

	// 发送HTTP请求数据
	httpRequest := "GET /api/test HTTP/1.1\r\nHost: example.com\r\nUser-Agent: TestClient/1.0\r\nContent-Length: 0\r\n\r\n"
	err := ht.ProcessPacket("test-session-1", []byte(httpRequest), types.DirectionRequest)
	if err != nil {
		t.Fatalf("处理请求失败: %v", err)
	}

	// 发送HTTP响应数据
	httpResponse := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 15\r\n\r\n{\"status\":\"ok\"}"
	err = ht.ProcessPacket("test-session-1", []byte(httpResponse), types.DirectionResponse)
	if err != nil {
		t.Fatalf("处理响应失败: %v", err)
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
		if receivedRequest.URL.Path != "/api/test" {
			t.Errorf("期望路径为/api/test，实际为: %s", receivedRequest.URL.Path)
		}
		if !receivedRequest.Complete {
			t.Error("请求应该是完整的")
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
		if !receivedResponse.Complete {
			t.Error("响应应该是完整的")
		}
		expectedBody := "{\"status\":\"ok\"}"
		if string(receivedResponse.Body) != expectedBody {
			t.Errorf("期望响应体为%s，实际为: %s", expectedBody, string(receivedResponse.Body))
		}
	}
}

// TestHTrackChannelMultipleRequests 测试处理多个请求的channel功能
func TestHTrackChannelMultipleRequests(t *testing.T) {
	config := &htrack.Config{
		MaxSessions:        100,
		MaxTransactions:    100,
		TransactionTimeout: 30 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        false,
		AutoCleanup:        false,
		ChannelBufferSize:  20, // 增大缓冲区
		EnableChannels:     true,
	}

	ht := htrack.New(config)
	defer ht.Close()

	const numRequests = 5
	var wg sync.WaitGroup
	var mu sync.Mutex
	receivedRequests := make([]*types.HTTPRequest, 0, numRequests)
	receivedResponses := make([]*types.HTTPResponse, 0, numRequests)

	// 监听请求channel
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numRequests; i++ {
			select {
			case request := <-ht.GetRequestChan():
				mu.Lock()
				receivedRequests = append(receivedRequests, request)
				mu.Unlock()
				t.Logf("收到请求 %d: %s %s", len(receivedRequests), request.Method, request.URL.String())
			case <-time.After(10 * time.Second):
				t.Errorf("超时：未收到第 %d 个请求", i+1)
				return
			}
		}
	}()

	// 监听响应channel
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numRequests; i++ {
			select {
			case response := <-ht.GetResponseChan():
				mu.Lock()
				receivedResponses = append(receivedResponses, response)
				mu.Unlock()
				t.Logf("收到响应 %d: %d %s", len(receivedResponses), response.StatusCode, response.Status)
			case <-time.After(10 * time.Second):
				t.Errorf("超时：未收到第 %d 个响应", i+1)
				return
			}
		}
	}()

	// 发送多个请求和响应
	for i := 0; i < numRequests; i++ {
		sessionID := fmt.Sprintf("test-session-%d", i+1)

		// 发送请求
		request := fmt.Sprintf("POST /api/data/%d HTTP/1.1\r\nHost: api.example.com\r\nContent-Type: application/json\r\nContent-Length: 15\r\n\r\n{\"id\":%d,\"test\":1}", i, i)
		err := ht.ProcessPacket(sessionID, []byte(request), types.DirectionRequest)
		if err != nil {
			t.Fatalf("处理请求 %d 失败: %v", i, err)
		}

		// 发送响应
		response := fmt.Sprintf("HTTP/1.1 201 Created\r\nContent-Type: application/json\r\nLocation: /api/data/%d\r\nContent-Length: 22\r\n\r\n{\"id\":%d,\"created\":true}", i, i)
		err = ht.ProcessPacket(sessionID, []byte(response), types.DirectionResponse)
		if err != nil {
			t.Fatalf("处理响应 %d 失败: %v", i, err)
		}

		// 稍微延迟，避免过快发送
		time.Sleep(100 * time.Millisecond)
	}

	// 等待所有数据处理完成
	wg.Wait()

	// 验证结果
	mu.Lock()
	defer mu.Unlock()

	if len(receivedRequests) != numRequests {
		t.Errorf("期望收到 %d 个请求，实际收到 %d 个", numRequests, len(receivedRequests))
	}

	if len(receivedResponses) != numRequests {
		t.Errorf("期望收到 %d 个响应，实际收到 %d 个", numRequests, len(receivedResponses))
	}

	// 验证每个请求的内容
	for i, req := range receivedRequests {
		if req.Method != "POST" {
			t.Errorf("请求 %d: 期望方法为POST，实际为: %s", i, req.Method)
		}
		expectedPath := fmt.Sprintf("/api/data/%d", i)
		if req.URL.Path != expectedPath {
			t.Errorf("请求 %d: 期望路径为%s，实际为: %s", i, expectedPath, req.URL.Path)
		}
		if !req.Complete {
			t.Errorf("请求 %d: 应该是完整的", i)
		}
	}

	// 验证每个响应的内容
	for i, resp := range receivedResponses {
		if resp.StatusCode != 201 {
			t.Errorf("响应 %d: 期望状态码为201，实际为: %d", i, resp.StatusCode)
		}
		if resp.Status != "Created" {
			t.Errorf("响应 %d: 期望状态为Created，实际为: %s", i, resp.Status)
		}
		if !resp.Complete {
			t.Errorf("响应 %d: 应该是完整的", i)
		}
	}
}

// TestHTrackChannelDisabled 测试禁用channel时的行为
func TestHTrackChannelDisabled(t *testing.T) {
	config := &htrack.Config{
		MaxSessions:        100,
		MaxTransactions:    100,
		TransactionTimeout: 30 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        false,
		AutoCleanup:        false,
		ChannelBufferSize:  10,
		EnableChannels:     false, // 禁用channels
	}

	ht := htrack.New(config)
	defer ht.Close()

	// 验证channels为nil
	if ht.GetRequestChan() != nil {
		t.Error("禁用channels时，RequestChan应该为nil")
	}
	if ht.GetResponseChan() != nil {
		t.Error("禁用channels时，ResponseChan应该为nil")
	}

	// 发送数据应该仍然能正常处理，只是不会输出到channel
	httpRequest := "GET /test HTTP/1.1\r\nHost: example.com\r\nContent-Length: 0\r\n\r\n"
	err := ht.ProcessPacket("test-session", []byte(httpRequest), types.DirectionRequest)
	if err != nil {
		t.Fatalf("处理请求失败: %v", err)
	}

	httpResponse := "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK"
	err = ht.ProcessPacket("test-session", []byte(httpResponse), types.DirectionResponse)
	if err != nil {
		t.Fatalf("处理响应失败: %v", err)
	}
}

// TestHTrackChannelBufferOverflow 测试channel缓冲区溢出的处理
func TestHTrackChannelBufferOverflow(t *testing.T) {
	config := &htrack.Config{
		MaxSessions:        100,
		MaxTransactions:    100,
		TransactionTimeout: 30 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        false,
		AutoCleanup:        false,
		ChannelBufferSize:  2, // 很小的缓冲区
		EnableChannels:     true,
	}

	ht := htrack.New(config)
	defer ht.Close()

	// 不读取channel，让它填满
	// 发送多个请求，超过缓冲区大小
	for i := 0; i < 5; i++ {
		sessionID := fmt.Sprintf("overflow-session-%d", i)
		request := fmt.Sprintf("GET /test/%d HTTP/1.1\r\nHost: example.com\r\nContent-Length: 0\r\n\r\n", i)
		err := ht.ProcessPacket(sessionID, []byte(request), types.DirectionRequest)
		if err != nil {
			t.Fatalf("处理请求 %d 失败: %v", i, err)
		}
		time.Sleep(50 * time.Millisecond) // 稍微延迟
	}

	// 验证程序没有阻塞或崩溃
	// 由于使用了非阻塞的select，超出缓冲区的数据会被丢弃
	t.Log("缓冲区溢出测试完成，程序应该正常运行")

	// 现在读取一些数据，验证channel仍然可用
	var receivedCount int
	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-ht.GetRequestChan():
			receivedCount++
			t.Logf("从channel读取到第 %d 个请求", receivedCount)
		case <-timeout:
			t.Logf("总共从channel读取到 %d 个请求", receivedCount)
			return
		}
	}
}
