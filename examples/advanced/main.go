package main

import (
	"fmt"
	"log"
	"time"

	"github.com/danl5/htrack"
	"github.com/danl5/htrack/types"
)

func main() {
	fmt.Println("HTrack 高级示例 - HTTP/2 和复杂场景")
	fmt.Println("=====================================")

	// 创建HTrack实例
	ht := htrack.New(&htrack.Config{
		MaxConnections:     50,
		MaxTransactions:    500,
		ConnectionTimeout:  5 * time.Minute,
		TransactionTimeout: 1 * time.Minute,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		AutoCleanup:        true,
		CleanupInterval:    30 * time.Second,
	})
	defer ht.Close()

	// 设置详细的事件处理器
	ht.SetEventHandlers(&htrack.EventHandlers{
		OnConnectionCreated: func(connectionID string, version types.HTTPVersion) {
			fmt.Printf("🔗 [连接建立] %s (%v)\n", connectionID, version)
		},
		OnConnectionClosed: func(connectionID string) {
			fmt.Printf("❌ [连接关闭] %s\n", connectionID)
		},
		OnTransactionCreated: func(transactionID, connectionID string) {
			fmt.Printf("📝 [事务开始] %s @ %s\n", transactionID, connectionID)
		},
		OnTransactionComplete: func(transactionID string, request *types.HTTPRequest, response *types.HTTPResponse) {
			fmt.Printf("✅ [事务完成] %s\n", transactionID)
			if request != nil && response != nil {
				duration := response.Timestamp.Sub(request.Timestamp)
				fmt.Printf("   %s %s -> %d (%v)\n",
					request.Method, request.URL.Path, response.StatusCode, duration)
			}
		},
		OnRequestParsed: func(request *types.HTTPRequest) {
			fmt.Printf("📤 [请求] %s %s\n", request.Method, request.URL.Path)
			if request.StreamID != nil {
				fmt.Printf("   Stream ID: %d\n", *request.StreamID)
			}
		},
		OnResponseParsed: func(response *types.HTTPResponse) {
			fmt.Printf("📥 [响应] %d %s\n", response.StatusCode, response.Status)
			if response.StreamID != nil {
				fmt.Printf("   Stream ID: %d\n", *response.StreamID)
			}
		},
		OnError: func(err error) {
			fmt.Printf("⚠️  [错误] %v\n", err)
		},
	})

	// 示例1: HTTP/2连接前导和设置帧
	fmt.Println("\n=== 示例1: HTTP/2连接建立 ===")
	http2Preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")

	// HTTP/2 SETTINGS帧
	settingsFrame := []byte{
		0x00, 0x00, 0x0C, // Length: 12
		0x04,                   // Type: SETTINGS
		0x00,                   // Flags: 0
		0x00, 0x00, 0x00, 0x00, // Stream ID: 0
		// Settings payload
		0x00, 0x03, 0x00, 0x00, 0x00, 0x64, // MAX_CONCURRENT_STREAMS: 100
		0x00, 0x04, 0x00, 0x00, 0xFF, 0xFF, // INITIAL_WINDOW_SIZE: 65535
	}

	http2ConnectionData := append(http2Preface, settingsFrame...)
	err := ht.ProcessPacket("http2-conn-1", http2ConnectionData, types.DirectionRequest)
	if err != nil {
		log.Printf("处理HTTP/2连接数据失败: %v", err)
	}

	// 示例2: HTTP/2 HEADERS帧（请求）
	fmt.Println("\n=== 示例2: HTTP/2请求 ===")
	headersFrame := []byte{
		0x00, 0x00, 0x20, // Length: 32
		0x01,                   // Type: HEADERS
		0x05,                   // Flags: END_HEADERS | END_STREAM
		0x00, 0x00, 0x00, 0x01, // Stream ID: 1
		// 简化的HPACK编码头部（实际应该更复杂）
		0x07, ':', 'm', 'e', 't', 'h', 'o', 'd', 0x03, 'G', 'E', 'T',
		0x05, ':', 'p', 'a', 't', 'h', 0x09, '/', 'a', 'p', 'i', '/', 'd', 'a', 't', 'a',
		0x07, ':', 's', 'c', 'h', 'e', 'm', 'e', 0x05, 'h', 't', 't', 'p', 's',
	}

	err = ht.ProcessPacket("http2-conn-1", headersFrame, types.DirectionRequest)
	if err != nil {
		log.Printf("处理HTTP/2请求失败: %v", err)
	}

	// 示例3: HTTP/2响应
	fmt.Println("\n=== 示例3: HTTP/2响应 ===")
	responseHeadersFrame := []byte{
		0x00, 0x00, 0x15, // Length: 21
		0x01,                   // Type: HEADERS
		0x04,                   // Flags: END_HEADERS
		0x00, 0x00, 0x00, 0x01, // Stream ID: 1
		// 简化的响应头部
		0x07, ':', 's', 't', 'a', 't', 'u', 's', 0x03, '2', '0', '0',
		0x0C, 'c', 'o', 'n', 't', 'e', 'n', 't', '-', 't', 'y', 'p', 'e', 0x10, 'a', 'p', 'p', 'l', 'i', 'c', 'a', 't', 'i', 'o', 'n', '/', 'j', 's', 'o', 'n',
	}

	// DATA帧
	dataFrame := []byte{
		0x00, 0x00, 0x0F, // Length: 15
		0x00,                   // Type: DATA
		0x01,                   // Flags: END_STREAM
		0x00, 0x00, 0x00, 0x01, // Stream ID: 1
		// JSON数据
		'{', '"', 's', 't', 'a', 't', 'u', 's', '"', ':', '"', 'o', 'k', '"', '}',
	}

	responseData := append(responseHeadersFrame, dataFrame...)
	err = ht.ProcessPacket("http2-conn-1", responseData, types.DirectionResponse)
	if err != nil {
		log.Printf("处理HTTP/2响应失败: %v", err)
	}

	// 示例4: 并发HTTP/1.1连接
	fmt.Println("\n=== 示例4: 并发HTTP/1.1连接 ===")
	for i := 1; i <= 3; i++ {
		connID := fmt.Sprintf("http1-conn-%d", i)

		// 并发发送请求
		go func(id string, num int) {
			request := fmt.Sprintf(
				"GET /api/resource/%d HTTP/1.1\r\n"+
					"Host: api.example.com\r\n"+
					"User-Agent: HTrack-Client/1.0\r\n"+
					"Accept: application/json\r\n"+
					"Connection: keep-alive\r\n"+
					"\r\n", num)

			err := ht.ProcessPacket(id, []byte(request), types.DirectionRequest)
			if err != nil {
				log.Printf("处理并发请求%d失败: %v", num, err)
			}

			// 模拟响应延迟
			time.Sleep(time.Duration(num*100) * time.Millisecond)

			response := fmt.Sprintf(
				"HTTP/1.1 200 OK\r\n"+
					"Content-Type: application/json\r\n"+
					"Content-Length: %d\r\n"+
					"Server: nginx/1.18.0\r\n"+
					"\r\n"+
					`{"id":%d,"data":"resource-%d"}`, 25+num, num, num)

			err = ht.ProcessPacket(id, []byte(response), types.DirectionResponse)
			if err != nil {
				log.Printf("处理并发响应%d失败: %v", num, err)
			}
		}(connID, i)
	}

	// 等待并发处理完成
	time.Sleep(1 * time.Second)

	// 示例5: 大文件传输模拟（分块传输）
	fmt.Println("\n=== 示例5: 分块传输编码 ===")
	chunkedRequest := []byte(
		"POST /upload HTTP/1.1\r\n" +
			"Host: upload.example.com\r\n" +
			"Transfer-Encoding: chunked\r\n" +
			"Content-Type: application/octet-stream\r\n" +
			"\r\n" +
			"A\r\n" +
			"0123456789\r\n" +
			"5\r\n" +
			"ABCDE\r\n" +
			"0\r\n" +
			"\r\n")

	err = ht.ProcessPacket("upload-conn", chunkedRequest, types.DirectionRequest)
	if err != nil {
		log.Printf("处理分块请求失败: %v", err)
	}

	chunkedResponse := []byte(
		"HTTP/1.1 201 Created\r\n" +
			"Content-Type: application/json\r\n" +
			"Transfer-Encoding: chunked\r\n" +
			"\r\n" +
			"1C\r\n" +
			`{"status":"uploaded"}` + "\r\n" +
			"0\r\n" +
			"\r\n")

	err = ht.ProcessPacket("upload-conn", chunkedResponse, types.DirectionResponse)
	if err != nil {
		log.Printf("处理分块响应失败: %v", err)
	}

	// 等待处理完成
	time.Sleep(500 * time.Millisecond)

	// 示例6: 错误处理和恢复
	fmt.Println("\n=== 示例6: 错误处理 ===")
	// 发送格式错误的HTTP请求
	malformedRequest := []byte("INVALID HTTP REQUEST\r\n\r\n")
	err = ht.ProcessPacket("error-conn", malformedRequest, types.DirectionRequest)
	if err != nil {
		fmt.Printf("预期的错误: %v\n", err)
	}

	// 发送不完整的请求
	incompleteRequest := []byte("GET /test HTTP/1.1\r\nHost: test.com\r\n")
	err = ht.ProcessPacket("incomplete-conn", incompleteRequest, types.DirectionRequest)
	if err != nil {
		log.Printf("处理不完整请求: %v", err)
	}

	// 补全请求
	completeRequest := []byte("User-Agent: Test\r\n\r\n")
	err = ht.ProcessPacket("incomplete-conn", completeRequest, types.DirectionRequest)
	if err != nil {
		log.Printf("补全请求失败: %v", err)
	}

	// 等待处理完成
	time.Sleep(200 * time.Millisecond)

	// 显示最终统计信息
	fmt.Println("\n=== 最终统计信息 ===")
	stats := ht.GetStatistics()
	fmt.Printf("📊 总连接数: %d (HTTP/1.x: %d, HTTP/2: %d)\n",
		stats.TotalConnections, stats.HTTP1Connections, stats.HTTP2Connections)
	fmt.Printf("📊 活跃连接数: %d\n", stats.ActiveConnections)
	fmt.Printf("📊 总事务数: %d (活跃: %d)\n", stats.TotalTransactions, stats.ActiveTransactions)
	fmt.Printf("📊 请求/响应: %d/%d\n", stats.TotalRequests, stats.TotalResponses)
	fmt.Printf("📊 错误数: %d\n", stats.ErrorCount)
	fmt.Printf("📊 HTTP/2流数: %d\n", stats.HTTP2Streams)

	// 显示连接详情
	fmt.Println("\n=== 连接详情 ===")
	connections := ht.GetActiveConnections()
	for i, conn := range connections {
		fmt.Printf("连接 %d:\n", i+1)
		fmt.Printf("  ID: %s\n", conn.ID)
		fmt.Printf("  版本: %v\n", conn.Version)
		fmt.Printf("  状态: %v\n", conn.State)
		fmt.Printf("  运行时间: %v\n", time.Since(conn.CreatedAt).Round(time.Millisecond))
		fmt.Printf("  事务数: %d\n", len(conn.Transactions))

		// 显示该连接的事务
		for _, txID := range conn.Transactions {
			if txInfo, err := ht.GetTransaction(txID); err == nil {
				fmt.Printf("    事务 %s: %v\n", txID[:8], txInfo.State)
			}
		}
		fmt.Println()
	}

	// 演示便捷函数
	fmt.Println("=== 便捷函数演示 ===")
	simpleRequest := []byte(
		"GET /simple HTTP/1.1\r\n" +
			"Host: simple.com\r\n" +
			"\r\n")

	req, resp, err := htrack.ParseHTTPMessage(simpleRequest)
	if err != nil {
		log.Printf("解析失败: %v", err)
	} else {
		if req != nil {
			fmt.Printf("✅ 解析到请求: %s %s\n", req.Method, req.URL.Path)
		}
		if resp != nil {
			fmt.Printf("✅ 解析到响应: %d\n", resp.StatusCode)
		}
	}

	fmt.Println("\n🎉 高级示例完成！")
}
