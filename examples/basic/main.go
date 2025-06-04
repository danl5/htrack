package main

import (
	"fmt"
	"log"
	"time"

	"github.com/danl5/htrack"
	"github.com/danl5/htrack/types"
)

func main() {
	fmt.Println("HTrack HTTP通信生命周期管理器示例")
	fmt.Println("======================================")

	// 创建HTrack实例
	config := &htrack.Config{
		MaxConnections:     100,
		MaxTransactions:    1000,
		ConnectionTimeout:  10 * time.Minute,
		TransactionTimeout: 2 * time.Minute,
		BufferSize:         32 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		AutoCleanup:        true,
		CleanupInterval:    1 * time.Minute,
	}

	ht := htrack.New(config)
	defer ht.Close()

	// 设置事件处理器
	ht.SetEventHandlers(&htrack.EventHandlers{
		OnConnectionCreated: func(connectionID string, version types.HTTPVersion) {
			fmt.Printf("[连接创建] ID: %s, 版本: %v\n", connectionID, version)
		},
		OnConnectionClosed: func(connectionID string) {
			fmt.Printf("[连接关闭] ID: %s\n", connectionID)
		},
		OnTransactionCreated: func(transactionID, connectionID string) {
			fmt.Printf("[事务创建] 事务ID: %s, 连接ID: %s\n", transactionID, connectionID)
		},
		OnTransactionComplete: func(transactionID string, request *types.HTTPRequest, response *types.HTTPResponse) {
			fmt.Printf("[事务完成] 事务ID: %s\n", transactionID)
			if request != nil {
				fmt.Printf("  请求: %s %s\n", request.Method, request.URL.Path)
			}
			if response != nil {
				fmt.Printf("  响应: %d %s\n", response.StatusCode, response.Status)
			}
		},
		OnRequestParsed: func(request *types.HTTPRequest) {
			fmt.Printf("[请求解析] %s %s %s\n", request.Method, request.URL.Path, request.Proto)
			fmt.Printf("  Headers: %d个\n", len(request.Headers))
			fmt.Printf("  Body: %d字节\n", len(request.Body))
		},
		OnResponseParsed: func(response *types.HTTPResponse) {
			fmt.Printf("[响应解析] %d %s %s\n", response.StatusCode, response.Status, response.Proto)
			fmt.Printf("  Headers: %d个\n", len(response.Headers))
			fmt.Printf("  Body: %d字节\n", len(response.Body))
		},
		OnError: func(err error) {
			fmt.Printf("[错误] %v\n", err)
		},
	})

	// 示例1: 处理HTTP/1.1 GET请求
	fmt.Println("\n=== 示例1: HTTP/1.1 GET请求 ===")
	http1GetRequest := []byte(
		"GET /api/users HTTP/1.1\r\n" +
			"Host: example.com\r\n" +
			"User-Agent: HTrack/1.0\r\n" +
			"Accept: application/json\r\n" +
			"Connection: keep-alive\r\n" +
			"\r\n")

	err := ht.ProcessPacket("conn-1", http1GetRequest, types.DirectionRequest)
	if err != nil {
		log.Printf("处理请求失败: %v", err)
	}

	// 对应的响应
	http1GetResponse := []byte(
		"HTTP/1.1 200 OK\r\n" +
			"Content-Type: application/json\r\n" +
			"Content-Length: 45\r\n" +
			"Server: nginx/1.18.0\r\n" +
			"\r\n" +
			`{"users":[{"id":1,"name":"Alice"}]}`)

	err = ht.ProcessPacket("conn-1", http1GetResponse, types.DirectionResponse)
	if err != nil {
		log.Printf("处理响应失败: %v", err)
	}

	// 示例2: 处理HTTP/1.1 POST请求
	fmt.Println("\n=== 示例2: HTTP/1.1 POST请求 ===")
	http1PostRequest := []byte(
		"POST /api/users HTTP/1.1\r\n" +
			"Host: example.com\r\n" +
			"Content-Type: application/json\r\n" +
			"Content-Length: 25\r\n" +
			"\r\n" +
			`{"name":"Bob","age":30}`)

	err = ht.ProcessPacket("conn-2", http1PostRequest, types.DirectionRequest)
	if err != nil {
		log.Printf("处理POST请求失败: %v", err)
	}

	http1PostResponse := []byte(
		"HTTP/1.1 201 Created\r\n" +
			"Content-Type: application/json\r\n" +
			"Content-Length: 22\r\n" +
			"Location: /api/users/2\r\n" +
			"\r\n" +
			`{"id":2,"name":"Bob"}`)

	err = ht.ProcessPacket("conn-2", http1PostResponse, types.DirectionResponse)
	if err != nil {
		log.Printf("处理POST响应失败: %v", err)
	}

	// 示例3: 分片数据包处理
	fmt.Println("\n=== 示例3: 分片数据包处理 ===")
	// 将请求分成多个片段
	fragment1 := []byte("GET /api/products HTTP/1.1\r\n")
	fragment2 := []byte("Host: shop.example.com\r\n")
	fragment3 := []byte("Accept: */*\r\n\r\n")

	// 依次处理片段
	err = ht.ProcessPacket("conn-3", fragment1, types.DirectionRequest)
	if err != nil {
		log.Printf("处理片段1失败: %v", err)
	}

	err = ht.ProcessPacket("conn-3", fragment2, types.DirectionRequest)
	if err != nil {
		log.Printf("处理片段2失败: %v", err)
	}

	err = ht.ProcessPacket("conn-3", fragment3, types.DirectionRequest)
	if err != nil {
		log.Printf("处理片段3失败: %v", err)
	}

	// 等待一下让事件处理完成
	time.Sleep(100 * time.Millisecond)

	// 显示统计信息
	fmt.Println("\n=== 统计信息 ===")
	stats := ht.GetStatistics()
	fmt.Printf("总连接数: %d\n", stats.TotalConnections)
	fmt.Printf("活跃连接数: %d\n", stats.ActiveConnections)
	fmt.Printf("总事务数: %d\n", stats.TotalTransactions)
	fmt.Printf("活跃事务数: %d\n", stats.ActiveTransactions)
	fmt.Printf("总请求数: %d\n", stats.TotalRequests)
	fmt.Printf("总响应数: %d\n", stats.TotalResponses)
	fmt.Printf("HTTP/1.x连接数: %d\n", stats.HTTP1Connections)
	fmt.Printf("HTTP/2连接数: %d\n", stats.HTTP2Connections)

	// 显示活跃连接
	fmt.Println("\n=== 活跃连接 ===")
	connections := ht.GetActiveConnections()
	for _, conn := range connections {
		fmt.Printf("连接ID: %s\n", conn.ID)
		fmt.Printf("  版本: %v\n", conn.Version)
		fmt.Printf("  状态: %v\n", conn.State)
		fmt.Printf("  创建时间: %s\n", conn.CreatedAt.Format("15:04:05"))
		fmt.Printf("  最后活动: %s\n", conn.LastActivity.Format("15:04:05"))
		fmt.Printf("  事务数: %d\n", len(conn.Transactions))
		fmt.Println()
	}

	// 显示活跃事务
	fmt.Println("=== 活跃事务 ===")
	transactions := ht.GetActiveTransactions()
	for _, tx := range transactions {
		fmt.Printf("事务ID: %s\n", tx.ID)
		fmt.Printf("  连接ID: %s\n", tx.ConnectionID)
		if tx.StreamID != nil {
			fmt.Printf("  流ID: %d\n", *tx.StreamID)
		}
		fmt.Printf("  状态: %v\n", tx.State)
		fmt.Printf("  有请求: %v\n", tx.HasRequest)
		fmt.Printf("  有响应: %v\n", tx.HasResponse)
		fmt.Printf("  创建时间: %s\n", tx.CreatedAt.Format("15:04:05"))
		if tx.CompletedAt != nil {
			fmt.Printf("  完成时间: %s\n", tx.CompletedAt.Format("15:04:05"))
		}
		fmt.Println()
	}

	fmt.Println("\n=== 示例完成 ===")
}
