package tests

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/danl5/htrack"
	"github.com/danl5/htrack/types"
)

// TestHTTP2AllFrameTypes 测试所有HTTP/2帧类型的解析
func TestHTTP2AllFrameTypes(t *testing.T) {
	// 创建配置
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:       10000,
		EnableHTTP1:       false,
		EnableHTTP2:       true,
		EnableChannels:    true,
		ChannelBufferSize: 100,
	})

	// 设置事件处理器
	var receivedFrames []*types.HTTPRequest
	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(req *types.HTTPRequest) {
			receivedFrames = append(receivedFrames, req)
			t.Logf("收到HTTP/2帧: %s, Stream ID: %d, 帧类型: %s",
				req.Method, *req.StreamID, req.Headers.Get("X-HTTP2-Frame-Type"))
		},
		OnError: func(err error) {
			t.Logf("解析错误: %v", err)
		},
	})

	// 测试用例：不同类型的HTTP/2帧
	testCases := []struct {
		name        string
		frameData   []byte
		expectedType string
		expectedMethod string
		streamID    uint32
	}{
		{
			name:        "DATA帧",
			frameData:   createDataFrame(),
			expectedType: "DATA",
			expectedMethod: "HTTP2_FRAME_DATA",
			streamID:    1,
		},
		{
			name:        "HEADERS帧",
			frameData:   createHeadersFrame(),
			expectedType: "HEADERS",
			expectedMethod: "HTTP2_FRAME_HEADERS",
			streamID:    1,
		},
		{
			name:        "SETTINGS帧",
			frameData:   createSettingsFrame(),
			expectedType: "SETTINGS",
			expectedMethod: "HTTP2_FRAME_SETTINGS",
			streamID:    0,
		},
		{
			name:        "PING帧",
			frameData:   createPingFrame(),
			expectedType: "PING",
			expectedMethod: "HTTP2_FRAME_PING",
			streamID:    0,
		},
		{
			name:        "PRIORITY帧",
			frameData:   createPriorityFrame(),
			expectedType: "PRIORITY",
			expectedMethod: "HTTP2_FRAME_PRIORITY",
			streamID:    1,
		},
		{
			name:        "RST_STREAM帧",
			frameData:   createRSTStreamFrame(),
			expectedType: "RST_STREAM",
			expectedMethod: "HTTP2_FRAME_RST_STREAM",
			streamID:    1,
		},
		{
			name:        "WINDOW_UPDATE帧",
			frameData:   createWindowUpdateFrame(),
			expectedType: "WINDOW_UPDATE",
			expectedMethod: "HTTP2_FRAME_WINDOW_UPDATE",
			streamID:    0,
		},
		{
			name:        "GOAWAY帧",
			frameData:   createGoAwayFrame(),
			expectedType: "GOAWAY",
			expectedMethod: "HTTP2_FRAME_GOAWAY",
			streamID:    0,
		},
		{
			name:        "未知帧类型",
			frameData:   createUnknownFrame(),
			expectedType: "UNKNOWN_255",
			expectedMethod: "HTTP2_FRAME_UNKNOWN_255",
			streamID:    1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 重置接收到的帧
			receivedFrames = nil

			// 创建数据包信息
			packetInfo := &types.PacketInfo{
				Data:      tc.frameData,
				Direction: types.DirectionClientToServer,
				TCPTuple: &types.TCPTuple{
					SrcIP:   "192.168.1.100",
					SrcPort: 12345,
					DstIP:   "192.168.1.200",
					DstPort: 443,
				},
				TimeDiff:    0,
				PID:         1234,
				TID:         5678,
				ProcessName: "test-http2-process",
			}

			// 使用空connectionID进行直接解析
			err := hTrack.ProcessPacket("", packetInfo)
			if err != nil {
				t.Fatalf("直接解析%s失败: %v", tc.name, err)
			}

			// 等待一小段时间让异步处理完成
			time.Sleep(50 * time.Millisecond)

			// 验证结果
			if len(receivedFrames) == 0 {
				t.Fatalf("未收到%s", tc.name)
			}

			frame := receivedFrames[0]

			// 验证帧信息
			if frame.Method != tc.expectedMethod {
				t.Errorf("期望方法为%s，实际为: %s", tc.expectedMethod, frame.Method)
			}

			if *frame.StreamID != tc.streamID {
				t.Errorf("期望流ID为%d，实际为: %d", tc.streamID, *frame.StreamID)
			}

			frameType := frame.Headers.Get("X-HTTP2-Frame-Type")
			if frameType != tc.expectedType {
				t.Errorf("期望帧类型为%s，实际为: %s", tc.expectedType, frameType)
			}

			if !frame.Complete {
				t.Errorf("期望帧标记为完整")
			}

			t.Logf("✅ %s解析成功", tc.name)
		})
	}
}

// createSettingsFrame 创建SETTINGS帧
func createSettingsFrame() []byte {
	frame := make([]byte, 9) // 空SETTINGS帧，只有帧头
	// 长度: 0
	frame[0] = 0x00
	frame[1] = 0x00
	frame[2] = 0x00
	// 类型: SETTINGS (4)
	frame[3] = 0x04
	// 标志: ACK (1)
	frame[4] = 0x01
	// 流ID: 0
	frame[5] = 0x00
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x00
	return frame
}

// createPingFrame 创建PING帧
func createPingFrame() []byte {
	frame := make([]byte, 17) // 帧头(9) + PING数据(8)
	// 长度: 8
	frame[0] = 0x00
	frame[1] = 0x00
	frame[2] = 0x08
	// 类型: PING (6)
	frame[3] = 0x06
	// 标志: 0
	frame[4] = 0x00
	// 流ID: 0
	frame[5] = 0x00
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x00
	// PING数据: 8字节
	copy(frame[9:], []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})
	return frame
}

// createPriorityFrame 创建PRIORITY帧
func createPriorityFrame() []byte {
	frame := make([]byte, 14) // 帧头(9) + 优先级数据(5)
	// 长度: 5
	frame[0] = 0x00
	frame[1] = 0x00
	frame[2] = 0x05
	// 类型: PRIORITY (2)
	frame[3] = 0x02
	// 标志: 0
	frame[4] = 0x00
	// 流ID: 1
	frame[5] = 0x00
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x01
	// 优先级数据: 依赖流ID(4字节) + 权重(1字节)
	binary.BigEndian.PutUint32(frame[9:13], 0) // 依赖流ID: 0
	frame[13] = 16 // 权重: 16
	return frame
}

// createRSTStreamFrame 创建RST_STREAM帧
func createRSTStreamFrame() []byte {
	frame := make([]byte, 13) // 帧头(9) + 错误码(4)
	// 长度: 4
	frame[0] = 0x00
	frame[1] = 0x00
	frame[2] = 0x04
	// 类型: RST_STREAM (3)
	frame[3] = 0x03
	// 标志: 0
	frame[4] = 0x00
	// 流ID: 1
	frame[5] = 0x00
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x01
	// 错误码: CANCEL (8)
	binary.BigEndian.PutUint32(frame[9:13], 8)
	return frame
}

// createWindowUpdateFrame 创建WINDOW_UPDATE帧
func createWindowUpdateFrame() []byte {
	frame := make([]byte, 13) // 帧头(9) + 窗口增量(4)
	// 长度: 4
	frame[0] = 0x00
	frame[1] = 0x00
	frame[2] = 0x04
	// 类型: WINDOW_UPDATE (8)
	frame[3] = 0x08
	// 标志: 0
	frame[4] = 0x00
	// 流ID: 0 (连接级别)
	frame[5] = 0x00
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x00
	// 窗口大小增量: 65536
	binary.BigEndian.PutUint32(frame[9:13], 65536)
	return frame
}

// createGoAwayFrame 创建GOAWAY帧
func createGoAwayFrame() []byte {
	frame := make([]byte, 17) // 帧头(9) + 最后流ID(4) + 错误码(4)
	// 长度: 8
	frame[0] = 0x00
	frame[1] = 0x00
	frame[2] = 0x08
	// 类型: GOAWAY (7)
	frame[3] = 0x07
	// 标志: 0
	frame[4] = 0x00
	// 流ID: 0
	frame[5] = 0x00
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x00
	// 最后流ID: 1
	binary.BigEndian.PutUint32(frame[9:13], 1)
	// 错误码: NO_ERROR (0)
	binary.BigEndian.PutUint32(frame[13:17], 0)
	return frame
}

// createDataFrame 创建DATA帧
func createDataFrame() []byte {
	data := []byte("Hello, HTTP/2 DATA frame!")
	frame := make([]byte, 9+len(data)) // 帧头(9) + 数据
	// 长度
	frame[0] = byte(len(data) >> 16)
	frame[1] = byte(len(data) >> 8)
	frame[2] = byte(len(data))
	// 类型: DATA (0)
	frame[3] = 0x00
	// 标志: END_STREAM (1)
	frame[4] = 0x01
	// 流ID: 1
	frame[5] = 0x00
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x01
	// 数据
	copy(frame[9:], data)
	return frame
}

// createHeadersFrame 创建HEADERS帧
func createHeadersFrame() []byte {
	// 简单的HEADERS帧，包含基本的HTTP头部
	headerBlock := []byte{
		0x82, // :method: GET (索引2)
		0x86, // :scheme: http (索引6)
		0x84, // :path: / (索引4)
		0x01, // :authority: (字面量)
		0x0f, // 长度: 15
	}
	headerBlock = append(headerBlock, []byte("www.example.com")...)
	
	frame := make([]byte, 9+len(headerBlock)) // 帧头(9) + 头部块
	// 长度
	frame[0] = byte(len(headerBlock) >> 16)
	frame[1] = byte(len(headerBlock) >> 8)
	frame[2] = byte(len(headerBlock))
	// 类型: HEADERS (1)
	frame[3] = 0x01
	// 标志: END_HEADERS (4)
	frame[4] = 0x04
	// 流ID: 1
	frame[5] = 0x00
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x01
	// 头部块
	copy(frame[9:], headerBlock)
	return frame
}

// createUnknownFrame 创建未知类型帧
func createUnknownFrame() []byte {
	frame := make([]byte, 13) // 帧头(9) + 数据(4)
	// 长度: 4
	frame[0] = 0x00
	frame[1] = 0x00
	frame[2] = 0x04
	// 类型: 255 (未知)
	frame[3] = 0xFF
	// 标志: 0
	frame[4] = 0x00
	// 流ID: 1
	frame[5] = 0x00
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x01
	// 数据: 4字节
	copy(frame[9:], []byte{0xDE, 0xAD, 0xBE, 0xEF})
	return frame
}