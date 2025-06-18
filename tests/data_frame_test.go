package tests

import (
	"fmt"
	"testing"
	"time"

	"github.com/danl5/htrack"
	"github.com/danl5/htrack/types"
)

// TestDataFrameParsing 专门测试DATA帧的解析
func TestDataFrameParsing(t *testing.T) {
	// 创建htrack实例
	config := &htrack.Config{
		EnableChannels: true,
		ChannelBufferSize: 100,
		EnableHTTP2: true,
		MaxSessions: 10000,  // 设置足够大的连接数限制
		MaxTransactions: 10000,
	}
	hTrack := htrack.New(config)

	var parsedRequests []*types.HTTPRequest
	var parsedResponses []*types.HTTPResponse

	// 启动goroutine监听请求channel
	go func() {
		for request := range hTrack.RequestChan {
			if request != nil {
				parsedRequests = append(parsedRequests, request)
				frameType := request.Headers.Get("X-HTTP2-Frame-Type")
				streamID := request.Headers.Get("X-HTTP2-Stream-ID")
				fmt.Printf("✅ [请求] 帧类型: %s, 流ID: %s, 方法: %s\n", frameType, streamID, request.Method)
				if len(request.Body) > 0 {
					fmt.Printf("  请求体长度: %d bytes\n", len(request.Body))
					if frameType == "DATA" {
						fmt.Printf("  DATA帧内容: %s\n", string(request.Body))
					}
				}
			}
		}
	}()

	// 启动goroutine监听响应channel
	go func() {
		for response := range hTrack.ResponseChan {
			if response != nil {
				parsedResponses = append(parsedResponses, response)
				frameType := response.Headers.Get("X-HTTP2-Frame-Type")
				streamID := response.Headers.Get("X-HTTP2-Stream-ID")
				fmt.Printf("✅ [响应] 帧类型: %s, 流ID: %s, 状态: %s\n", frameType, streamID, response.Status)
				if len(response.Body) > 0 {
					fmt.Printf("  响应体长度: %d bytes\n", len(response.Body))
					if frameType == "DATA" {
						fmt.Printf("  DATA帧内容: %s\n", string(response.Body))
					}
				}
			}
		}
	}()

	// 测试用例：纯DATA帧
	testCases := []struct {
		name         string
		connectionID string
		direction    types.Direction
		data         []byte
		description  string
	}{
		{
			name:         "纯DATA帧-空连接ID",
			connectionID: "", // 空连接ID
			direction:    types.DirectionClientToServer,
			data:         []byte{0, 0, 11, 0, 1, 0, 0, 0, 1, 72, 101, 108, 108, 111, 32, 87, 111, 114, 108, 100}, // DATA帧: "Hello World"
			description:  "包含Hello World的DATA帧",
		},
		{
			name:         "纯DATA帧-正常连接ID",
			connectionID: "test-conn-123",
			direction:    types.DirectionClientToServer,
			data:         []byte{0, 0, 11, 0, 1, 0, 0, 0, 1, 72, 101, 108, 108, 111, 32, 87, 111, 114, 108, 100}, // DATA帧: "Hello World"
			description:  "包含Hello World的DATA帧",
		},
		{
			name:         "大DATA帧-空连接ID",
			connectionID: "", // 空连接ID
			direction:    types.DirectionServerToClient,
			data:         append([]byte{0, 0, 100, 0, 1, 0, 0, 0, 2}, make([]byte, 100)...), // 100字节的DATA帧
			description:  "100字节的大DATA帧",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fmt.Printf("\n=== 测试: %s ===\n", tc.name)
			fmt.Printf("连接ID: '%s'\n", tc.connectionID)
			fmt.Printf("描述: %s\n", tc.description)
			fmt.Printf("数据长度: %d bytes\n", len(tc.data))
			fmt.Printf("帧头: %v\n", tc.data[:9])

			// 记录处理前的计数
			beforeReqCount := len(parsedRequests)
			beforeRespCount := len(parsedResponses)

			// 创建包信息
			packetInfo := &types.PacketInfo{
				Data: tc.data,
				TCPTuple: &types.TCPTuple{
					SrcIP:   "192.168.1.100",
					DstIP:   "192.168.1.200",
					SrcPort: 12345,
					DstPort: 443,
				},
				Direction: tc.direction,
				PID:       1234,
				TID:       5678,
				ProcessName: "test-process",
			}

			// 处理数据包
			err := hTrack.ProcessPacket(tc.connectionID, packetInfo)
			if err != nil {
				t.Fatalf("处理数据包失败: %v", err)
			}

			// 等待处理完成
			time.Sleep(100 * time.Millisecond)

			// 检查结果
			newReqCount := len(parsedRequests) - beforeReqCount
			newRespCount := len(parsedResponses) - beforeRespCount

			fmt.Printf("解析结果: %d个请求, %d个响应\n", newReqCount, newRespCount)

			// 验证是否解析出了DATA帧
			dataFrameFound := false
			for i := beforeReqCount; i < len(parsedRequests); i++ {
				if parsedRequests[i].Headers.Get("X-HTTP2-Frame-Type") == "DATA" {
					dataFrameFound = true
					fmt.Printf("✅ 找到DATA帧 (请求)\n")
					break
				}
			}
			for i := beforeRespCount; i < len(parsedResponses); i++ {
				if parsedResponses[i].Headers.Get("X-HTTP2-Frame-Type") == "DATA" {
					dataFrameFound = true
					fmt.Printf("✅ 找到DATA帧 (响应)\n")
					break
				}
			}

			if !dataFrameFound {
				t.Errorf("❌ 未找到DATA帧")
				// 打印所有解析到的帧类型
				fmt.Printf("解析到的帧类型:\n")
				for i := beforeReqCount; i < len(parsedRequests); i++ {
					fmt.Printf("  请求帧: %s\n", parsedRequests[i].Headers.Get("X-HTTP2-Frame-Type"))
				}
				for i := beforeRespCount; i < len(parsedResponses); i++ {
					fmt.Printf("  响应帧: %s\n", parsedResponses[i].Headers.Get("X-HTTP2-Frame-Type"))
				}
			}
		})
	}
}
