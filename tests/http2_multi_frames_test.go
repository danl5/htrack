package tests

import (
	"testing"
	"time"

	"github.com/danl5/htrack"
	"github.com/danl5/htrack/types"
)

// TestHTTP2MultipleFramesParsing 测试多个帧合并发送时的解析能力
func TestHTTP2MultipleFramesParsing(t *testing.T) {
	// 创建配置
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:       10000,
		EnableHTTP1:       false,
		EnableHTTP2:       true,
		EnableChannels:    true,
		ChannelBufferSize: 100,
	})

	// 收集接收到的帧
	var receivedFrames []*types.HTTPRequest

	// 设置事件处理器
	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			if request != nil && request.Complete {
				frameType := request.Headers.Get("X-HTTP2-Frame-Type")
				streamID := request.Headers.Get("X-HTTP2-Stream-ID")
				t.Logf("收到HTTP/2帧: %s, Stream ID: %s, 方法: %s", frameType, streamID, request.Method)
				receivedFrames = append(receivedFrames, request)
			}
		},
		OnError: func(err error) {
			t.Logf("解析错误: %v", err)
		},
	})

	// 测试用例：不同的多帧组合
	testCases := []struct {
		name           string
		frames         [][]byte
		expectedCount  int
		expectedTypes  []string
		expectedStreamIDs []string
	}{
		{
			name: "两个控制帧",
			frames: [][]byte{
				createSettingsFrame(),
				createPingFrame(),
			},
			expectedCount: 2,
			expectedTypes: []string{"SETTINGS", "PING"},
			expectedStreamIDs: []string{"0", "0"},
		},
		{
			name: "三个不同类型帧",
			frames: [][]byte{
				createSettingsFrame(),
				createPriorityFrame(),
				createWindowUpdateFrame(),
			},
			expectedCount: 3,
			expectedTypes: []string{"SETTINGS", "PRIORITY", "WINDOW_UPDATE"},
			expectedStreamIDs: []string{"0", "1", "0"},
		},
		{
			name: "数据帧和控制帧混合",
			frames: [][]byte{
				createDataFrame(),
				createSettingsFrame(),
				createHeadersFrame(),
				createPingFrame(),
			},
			expectedCount: 4,
			expectedTypes: []string{"DATA", "SETTINGS", "HEADERS", "PING"},
			expectedStreamIDs: []string{"1", "0", "1", "0"},
		},
		{
			name: "五个帧的复杂组合",
			frames: [][]byte{
				createSettingsFrame(),
				createHeadersFrame(),
				createDataFrame(),
				createPingFrame(),
				createGoAwayFrame(),
			},
			expectedCount: 5,
			expectedTypes: []string{"SETTINGS", "HEADERS", "DATA", "PING", "GOAWAY"},
			expectedStreamIDs: []string{"0", "1", "1", "0", "0"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 重置接收到的帧
			receivedFrames = nil

			// 合并所有帧数据
			var multiFrameData []byte
			for _, frame := range tc.frames {
				multiFrameData = append(multiFrameData, frame...)
			}

			// 创建数据包信息
			packetInfo := &types.PacketInfo{
				Data:      multiFrameData,
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
				ProcessName: "test-http2-multi",
			}

			// 使用空connectionID进行直接解析
			err := hTrack.ProcessPacket("", packetInfo)
			if err != nil {
				t.Fatalf("直接解析%s失败: %v", tc.name, err)
			}

			// 等待一小段时间让异步处理完成
			time.Sleep(100 * time.Millisecond)

			// 验证接收到的帧数量
			if len(receivedFrames) != tc.expectedCount {
				t.Fatalf("期望收到%d个帧，实际收到: %d", tc.expectedCount, len(receivedFrames))
			}

			// 验证每个帧的类型和流ID
			for i, frame := range receivedFrames {
				frameType := frame.Headers.Get("X-HTTP2-Frame-Type")
				streamID := frame.Headers.Get("X-HTTP2-Stream-ID")

				if frameType != tc.expectedTypes[i] {
					t.Errorf("帧%d类型不匹配: 期望=%s, 实际=%s", i+1, tc.expectedTypes[i], frameType)
				}

				if streamID != tc.expectedStreamIDs[i] {
					t.Errorf("帧%d流ID不匹配: 期望=%s, 实际=%s", i+1, tc.expectedStreamIDs[i], streamID)
				}

				// 验证帧是否完整
				if !frame.Complete {
					t.Errorf("帧%d应该是完整的", i+1)
				}
			}

			t.Logf("✅ %s测试成功，正确解析了%d个帧", tc.name, len(receivedFrames))
		})
	}
}

// TestHTTP2LargeMultiFramePacket 测试包含大量帧的数据包
func TestHTTP2LargeMultiFramePacket(t *testing.T) {
	// 创建配置
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:       10000,
		EnableHTTP1:       false,
		EnableHTTP2:       true,
		EnableChannels:    true,
		ChannelBufferSize: 200, // 增大缓冲区
	})

	// 收集接收到的帧
	var receivedFrames []*types.HTTPRequest

	// 设置事件处理器
	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			if request != nil && request.Complete {
				receivedFrames = append(receivedFrames, request)
			}
		},
		OnError: func(err error) {
			t.Logf("解析错误: %v", err)
		},
	})

	// 创建包含10个帧的数据包
	var multiFrameData []byte
	expectedFrameCount := 10

	// 添加不同类型的帧
	for i := 0; i < expectedFrameCount; i++ {
		switch i % 4 {
		case 0:
			multiFrameData = append(multiFrameData, createSettingsFrame()...)
		case 1:
			multiFrameData = append(multiFrameData, createPingFrame()...)
		case 2:
			multiFrameData = append(multiFrameData, createPriorityFrame()...)
		case 3:
			multiFrameData = append(multiFrameData, createWindowUpdateFrame()...)
		}
	}

	// 创建数据包信息
	packetInfo := &types.PacketInfo{
		Data:      multiFrameData,
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
		ProcessName: "test-http2-large-multi",
	}

	// 使用空connectionID进行直接解析
	err := hTrack.ProcessPacket("", packetInfo)
	if err != nil {
		t.Fatalf("直接解析大量帧数据失败: %v", err)
	}

	// 等待处理完成
	time.Sleep(200 * time.Millisecond)

	// 验证接收到的帧数量
	if len(receivedFrames) != expectedFrameCount {
		t.Fatalf("期望收到%d个帧，实际收到: %d", expectedFrameCount, len(receivedFrames))
	}

	// 验证每个帧都是完整的
	for i, frame := range receivedFrames {
		if !frame.Complete {
			t.Errorf("帧%d应该是完整的", i+1)
		}
		frameType := frame.Headers.Get("X-HTTP2-Frame-Type")
		t.Logf("帧%d: 类型=%s", i+1, frameType)
	}

	t.Logf("✅ 大量帧测试成功，正确解析了%d个帧", len(receivedFrames))
}