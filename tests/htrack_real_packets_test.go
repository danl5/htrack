package tests

import (
	"fmt"
	"testing"
	"time"

	htrack "github.com/danl5/htrack"
	"github.com/danl5/htrack/types"
)

func TestHTrackProcessPacket(t *testing.T) {

	hTrack := htrack.New(&htrack.Config{
		MaxSessions:        10000,
		MaxTransactions:    10000,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        true,
		EnableHTTP2:        true,
		EnableChannels:     true,
	})

	// 启动 goroutine 监听请求 channel
	go func() {
		for request := range hTrack.RequestChan {
			if request != nil {
				fmt.Printf("✅ [Channel] 请求解析成功\n")
				path := ""
				if request.URL != nil {
					path = request.URL.Path
				}
				var streamID uint32 = 0
				if request.StreamID != nil {
					streamID = *(request.StreamID)
				}
				fmt.Printf("  请求: Method=%s, Path=%s, StreamID=%d, Complete=%t\n",
					request.Method, path, streamID, request.Complete)
				if len(request.Body) > 0 {
					fmt.Printf("  请求体长度: %d bytes\n", len(request.Body))
					if len(request.Body) <= 100 {
						fmt.Printf("  请求体预览: %s\n", string(request.Body))
					} else {
						fmt.Printf("  请求体预览: %s...\n", string(request.Body[:100]))
					}
				}
			}
		}
	}()

	// 启动 goroutine 监听响应 channel
	go func() {
		for response := range hTrack.ResponseChan {
			if response != nil {
				fmt.Printf("✅ [Channel] 响应解析成功\n")
				var streamID uint32 = 0
				if response.StreamID != nil {
					streamID = *(response.StreamID)
				}

				fmt.Printf("  响应: Status=%d, StreamID=%d, Complete=%t, BodyLen=%d\n",
					response.StatusCode, streamID, response.Complete, len(response.Body))
				if len(response.Body) > 0 {
					if len(response.Body) <= 100 {
						fmt.Printf("  响应体预览: %s\n", string(response.Body))
					} else {
						fmt.Printf("  响应体预览: %s...\n", string(response.Body[:100]))
					}
				}
			}
		}
	}()

	packets := []struct {
		connectionID string
		direction    string
		data         []byte
		description  string
	}{
		{
			connectionID: "413586-nghttpd-7f6255e22000",
			direction:    "server_to_client",
			data:         []byte{0, 0, 6, 4, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 100},
			description:  "第1个包: SETTINGS帧",
		},
		{
			connectionID: "413586-nghttpd-7f6255e22000",
			direction:    "client_to_server",
			data:         []byte{80, 82, 73, 32, 42, 32, 72, 84, 84, 80, 47, 50, 46, 48, 13, 10, 13, 10, 83, 77, 13, 10, 13, 10, 0, 0, 12, 4, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 4, 63, 255, 255, 255, 0, 0, 4, 8, 0, 0, 0, 0, 0, 63, 255, 0, 0, 0, 0, 74, 1, 4, 0, 0, 0, 1, 4, 136, 97, 37, 66, 88, 26, 36, 26, 36, 135, 65, 142, 11, 75, 132, 12, 174, 23, 153, 92, 45, 183, 113, 230, 154, 103, 131, 122, 143, 156, 84, 28, 114, 41, 84, 211, 165, 53, 137, 128, 174, 227, 107, 131, 64, 133, 156, 163, 144, 182, 7, 131, 134, 24, 97, 64, 133, 156, 163, 144, 182, 11, 5, 66, 66, 66, 66, 66, 15, 13, 2, 52, 51, 0, 0, 43, 0, 1, 0, 0, 0, 1, 123, 34, 116, 101, 115, 116, 34, 58, 32, 34, 100, 97, 116, 97, 34, 44, 32, 34, 109, 101, 115, 115, 97, 103, 101, 34, 58, 32, 34, 104, 101, 108, 108, 111, 32, 119, 111, 114, 108, 100, 34, 125, 10},
			description:  "第2个包: HTTP/2前导+SETTINGS+HEADERS+DATA",
		},
		{
			connectionID: "413586-nghttpd-7f6255e22000",
			direction:    "server_to_client",
			data:         []byte{0, 0, 0, 4, 1, 0, 0, 0, 0, 0, 0, 69, 1, 4, 0, 0, 0, 1, 141, 118, 144, 170, 105, 210, 154, 228, 82, 169, 167, 74, 107, 19, 1, 93, 166, 87, 7, 97, 150, 208, 122, 190, 148, 3, 234, 101, 182, 165, 4, 1, 54, 160, 90, 184, 1, 92, 105, 165, 49, 104, 223, 95, 146, 73, 124, 165, 137, 211, 77, 31, 106, 18, 113, 216, 130, 166, 14, 27, 240, 172, 247, 15, 13, 3, 49, 52, 55, 0, 0, 147, 0, 1, 0, 0, 0, 1, 60, 104, 116, 109, 108, 62, 60, 104, 101, 97, 100, 62, 60, 116, 105, 116, 108, 101, 62, 52, 48, 52, 32, 78, 111, 116, 32, 70, 111, 117, 110, 100, 60, 47, 116, 105, 116, 108, 101, 62, 60, 47, 104, 101, 97, 100, 62, 60, 98, 111, 100, 121, 62, 60, 104, 49, 62, 52, 48, 52, 32, 78, 111, 116, 32, 70, 111, 117, 110, 100, 60, 47, 104, 49, 62, 60, 104, 114, 62, 60, 97, 100, 100, 114, 101, 115, 115, 62, 110, 103, 104, 116, 116, 112, 100, 32, 110, 103, 104, 116, 116, 112, 50, 47, 49, 46, 52, 51, 46, 48, 32, 97, 116, 32, 112, 111, 114, 116, 32, 56, 52, 52, 51, 60, 47, 97, 100, 100, 114, 101, 115, 115, 62, 60, 47, 98, 111, 100, 121, 62, 60, 47, 104, 116, 109, 108, 62},
			description:  "第3个包: SETTINGS_ACK+HEADERS+DATA (stream 1响应)",
		},
		{
			connectionID: "413586-nghttpd-7f6255e22000",
			direction:    "client_to_server",
			data:         []byte{0, 0, 0, 4, 1, 0, 0, 0, 0, 0, 0, 21, 1, 4, 0, 0, 0, 3, 4, 136, 97, 37, 66, 88, 26, 36, 26, 36, 135, 193, 131, 192, 191, 190, 15, 13, 2, 52, 51, 0, 0, 43, 0, 1, 0, 0, 0, 3, 123, 34, 116, 101, 115, 116, 34, 58, 32, 34, 100, 97, 116, 97, 34, 44, 32, 34, 109, 101, 115, 115, 97, 103, 101, 34, 58, 32, 34, 104, 101, 108, 108, 111, 32, 119, 111, 114, 108, 100, 34, 125, 10},
			description:  "第4个包: SETTINGS_ACK+HEADERS+DATA (stream 3请求)",
		},
		{
			connectionID: "413586-nghttpd-7f6255e22000",
			direction:    "server_to_client",
			data:         []byte{0, 0, 10, 1, 4, 0, 0, 0, 3, 141, 192, 191, 190, 15, 13, 3, 49, 52, 55, 0, 0, 147, 0, 1, 0, 0, 0, 3, 60, 104, 116, 109, 108, 62, 60, 104, 101, 97, 100, 62, 60, 116, 105, 116, 108, 101, 62, 52, 48, 52, 32, 78, 111, 116, 32, 70, 111, 117, 110, 100, 60, 47, 116, 105, 116, 108, 101, 62, 60, 47, 104, 101, 97, 100, 62, 60, 98, 111, 100, 121, 62, 60, 104, 49, 62, 52, 48, 52, 32, 78, 111, 116, 32, 70, 111, 117, 110, 100, 60, 47, 104, 49, 62, 60, 104, 114, 62, 60, 97, 100, 100, 114, 101, 115, 115, 62, 110, 103, 104, 116, 116, 112, 100, 32, 110, 103, 104, 116, 116, 112, 50, 47, 49, 46, 52, 51, 46, 48, 32, 97, 116, 32, 112, 111, 114, 116, 32, 56, 52, 52, 51, 60, 47, 97, 100, 100, 114, 101, 115, 115, 62, 60, 47, 98, 111, 100, 121, 62, 60, 47, 104, 116, 109, 108, 62},
			description:  "第5个包: HEADERS+DATA (stream 3响应) - 这是用户关心的包",
		},
		{
			connectionID: "413586-nghttpd-7f6255e22000",
			direction:    "client_to_server",
			data:         []byte{0, 0, 8, 7, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			description:  "第6个包: GOAWAY帧",
		},
	}

	for _, packet := range packets {
		fmt.Printf("\n=== %s ===\n", packet.description)
		fmt.Printf("Connection ID: %s\n", packet.connectionID)
		fmt.Printf("Direction: %s\n", packet.direction)
		fmt.Printf("Data length: %d bytes\n", len(packet.data))
		fmt.Printf("First 20 bytes: %v\n", packet.data[:min(20, len(packet.data))])

		// 确定数据方向
		var direction types.Direction
		if packet.direction == "client_to_server" {
			direction = types.DirectionRequest
		} else {
			direction = types.DirectionResponse
		}

		err := hTrack.ProcessPacket(packet.connectionID, &types.PacketInfo{
			Data:      packet.data,
			Direction: direction,
			TCPTuple:  &types.TCPTuple{},
		})
		if err != nil {
			fmt.Printf("❌ 数据包解析错误: %v\n", err)
			continue
		}
	}

	// 等待一段时间让 channel 处理完所有消息
	fmt.Printf("\n=== 等待 Channel 消息处理完成 ===\n")
	time.Sleep(2 * time.Second)

	// 关闭 HTrack 以停止 channel
	hTrack.Close()
	fmt.Printf("\n=== 测试完成 ===\n")
}
