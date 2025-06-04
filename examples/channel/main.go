package main

import (
	"fmt"
	"log"
	"time"

	"github.com/danl5/htrack"
	"github.com/danl5/htrack/types"
)

func main() {
	// 创建配置，启用channel输出
	config := &htrack.Config{
		MaxConnections:     100,
		MaxTransactions:    1000,
		ConnectionTimeout:  30 * time.Second,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		AutoCleanup:        true,
		CleanupInterval:    5 * time.Minute,
		ChannelBufferSize:  200,  // 设置channel缓冲区大小
		EnableChannels:     true, // 启用channel输出
	}

	// 创建HTrack实例
	ht := htrack.New(config)
	defer ht.Close()

	// 启动goroutine监听请求channel
	go func() {
		for request := range ht.GetRequestChan() {
			fmt.Printf("收到完整请求: %s %s\n", request.Method, request.URL.String())
			fmt.Printf("请求头数量: %d\n", len(request.Headers))
			fmt.Printf("请求体大小: %d bytes\n", len(request.Body))
			fmt.Printf("解析时间: %s\n", request.Timestamp.Format(time.RFC3339))
			if request.StreamID != nil {
				fmt.Printf("HTTP/2 流ID: %d\n", *request.StreamID)
			}
			fmt.Println("---")
		}
		fmt.Println("请求channel已关闭")
	}()

	// 启动goroutine监听响应channel
	go func() {
		for response := range ht.GetResponseChan() {
			fmt.Printf("收到完整响应: %d %s\n", response.StatusCode, response.Status)
			fmt.Printf("响应头数量: %d\n", len(response.Headers))
			fmt.Printf("响应体大小: %d bytes\n", len(response.Body))
			fmt.Printf("解析时间: %s\n", response.Timestamp.Format(time.RFC3339))
			if response.StreamID != nil {
				fmt.Printf("HTTP/2 流ID: %d\n", *response.StreamID)
			}
			fmt.Println("---")
		}
		fmt.Println("响应channel已关闭")
	}()

	// 模拟处理HTTP/1.1请求
	fmt.Println("处理HTTP/1.1数据包...")
	httpRequest := "GET /api/users HTTP/1.1\r\nHost: example.com\r\nUser-Agent: TestClient/1.0\r\nContent-Length: 0\r\n\r\n"
	err := ht.ProcessPacket("conn-1", []byte(httpRequest), types.DirectionRequest)
	if err != nil {
		log.Printf("处理请求失败: %v", err)
	}

	httpResponse := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 13\r\n\r\n{\"status\":\"ok\"}"
	err = ht.ProcessPacket("conn-1", []byte(httpResponse), types.DirectionResponse)
	if err != nil {
		log.Printf("处理响应失败: %v", err)
	}

	// 等待一段时间让数据处理完成
	time.Sleep(2 * time.Second)

	// 模拟处理更多数据包
	fmt.Println("\n处理更多HTTP数据包...")
	for i := 0; i < 3; i++ {
		connID := fmt.Sprintf("conn-%d", i+2)

		// 发送请求
		request := fmt.Sprintf("POST /api/data/%d HTTP/1.1\r\nHost: api.example.com\r\nContent-Type: application/json\r\nContent-Length: 15\r\n\r\n{\"id\":%d,\"test\":1}", i, i)
		err := ht.ProcessPacket(connID, []byte(request), types.DirectionRequest)
		if err != nil {
			log.Printf("处理请求 %d 失败: %v", i, err)
		}

		// 发送响应
		response := fmt.Sprintf("HTTP/1.1 201 Created\r\nContent-Type: application/json\r\nLocation: /api/data/%d\r\nContent-Length: 22\r\n\r\n{\"id\":%d,\"created\":true}", i, i)
		err = ht.ProcessPacket(connID, []byte(response), types.DirectionResponse)
		if err != nil {
			log.Printf("处理响应 %d 失败: %v", i, err)
		}

		time.Sleep(500 * time.Millisecond)
	}

	// 等待所有数据处理完成
	time.Sleep(3 * time.Second)

	fmt.Println("\n示例完成，正在关闭...")
}
