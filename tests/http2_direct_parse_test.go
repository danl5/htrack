package tests

import (
	"testing"
	"time"

	"github.com/danl5/htrack"
	"github.com/danl5/htrack/types"
)

// TestHTTP2DirectParseFrames 测试HTTP/2直接解析模式下的帧级别解析
func TestHTTP2DirectParseFrames(t *testing.T) {
	// 创建配置，启用HTTP/2和channel输出
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:        10000,
		MaxTransactions:    10000,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        false,
		EnableHTTP2:        true,
		EnableChannels:     true,
		ChannelBufferSize:  10,
	})
	defer hTrack.Close()

	// 设置事件处理器
	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			t.Logf("收到HTTP/2请求帧: %s, Stream ID: %d, 帧类型: %s", 
				request.Method, 
				*request.StreamID, 
				request.Headers.Get("X-HTTP2-Frame-Type"))
		},
		OnResponseParsed: func(response *types.HTTPResponse) {
			t.Logf("收到HTTP/2响应帧: %s, Stream ID: %d, 帧类型: %s", 
				response.Status, 
				*response.StreamID, 
				response.Headers.Get("X-HTTP2-Frame-Type"))
		},
		OnError: func(err error) {
			t.Errorf("解析错误: %v", err)
		},
	})

	// 构造一个简单的HTTP/2 SETTINGS帧
	// 帧格式: [长度(3字节)] [类型(1字节)] [标志(1字节)] [流ID(4字节)] [载荷]
	settingsFrame := []byte{
		// 帧头 (9字节)
		0x00, 0x00, 0x00, // 长度: 0 (SETTINGS ACK帧没有载荷)
		0x04,             // 类型: SETTINGS (4)
		0x01,             // 标志: ACK (1)
		0x00, 0x00, 0x00, 0x00, // 流ID: 0 (连接级别)
		// 没有载荷数据
	}

	packetInfo := &types.PacketInfo{
		Data:      settingsFrame,
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

	// 使用空connectionID进行直接解析（不重组）
	t.Logf("发送HTTP/2 SETTINGS帧进行直接解析")
	err := hTrack.ProcessPacket("", packetInfo)
	if err != nil {
		t.Fatalf("直接解析HTTP/2帧失败: %v", err)
	}

	// 等待一小段时间让异步处理完成
	time.Sleep(100 * time.Millisecond)

	// 检查是否收到了请求
	select {
	case request := <-hTrack.GetRequestChan():
		t.Logf("✅ 成功收到HTTP/2帧请求")
		
		// 验证帧信息
		if request.Method != "HTTP2_FRAME_SETTINGS" {
			t.Errorf("期望方法为HTTP2_FRAME_SETTINGS，实际为: %s", request.Method)
		}
		
		if *request.StreamID != 0 {
			t.Errorf("期望流ID为0，实际为: %d", *request.StreamID)
		}
		
		frameType := request.Headers.Get("X-HTTP2-Frame-Type")
		if frameType != "SETTINGS" {
			t.Errorf("期望帧类型为SETTINGS，实际为: %s", frameType)
		}
		
		frameFlags := request.Headers.Get("X-HTTP2-Frame-Flags")
		if frameFlags != "1" {
			t.Errorf("期望帧标志为1 (ACK)，实际为: %s", frameFlags)
		}
		
		if !request.Complete {
			t.Errorf("期望帧标记为完整")
		}
		
	case <-time.After(2 * time.Second):
		t.Errorf("超时：未收到HTTP/2帧请求")
	}

	t.Logf("✅ HTTP/2直接解析帧级别测试成功")
}

// TestHTTP2DirectParseMultipleFrames 测试HTTP/2直接解析模式下的多帧解析
func TestHTTP2DirectParseMultipleFrames(t *testing.T) {
	// 创建配置
	hTrack := htrack.New(&htrack.Config{
		MaxSessions:        10000,
		MaxTransactions:    10000,
		TransactionTimeout: 60 * time.Second,
		BufferSize:         64 * 1024,
		EnableHTTP1:        false,
		EnableHTTP2:        true,
		EnableChannels:     true,
		ChannelBufferSize:  20,
	})
	defer hTrack.Close()

	// 设置事件处理器
	frameCount := 0
	hTrack.SetEventHandlers(&htrack.EventHandlers{
		OnRequestParsed: func(request *types.HTTPRequest) {
			frameCount++
			t.Logf("收到第%d个HTTP/2帧: %s, Stream ID: %d", 
				frameCount, 
				request.Headers.Get("X-HTTP2-Frame-Type"), 
				*request.StreamID)
		},
		OnResponseParsed: func(response *types.HTTPResponse) {
			// 响应处理
		},
		OnError: func(err error) {
			t.Errorf("解析错误: %v", err)
		},
	})

	// 构造包含多个帧的数据包
	// 第一个帧: SETTINGS帧
	settingsFrame := []byte{
		0x00, 0x00, 0x00, // 长度: 0
		0x04,             // 类型: SETTINGS
		0x00,             // 标志: 0
		0x00, 0x00, 0x00, 0x00, // 流ID: 0
	}

	// 第二个帧: PING帧
	pingFrame := []byte{
		0x00, 0x00, 0x08, // 长度: 8
		0x06,             // 类型: PING
		0x00,             // 标志: 0
		0x00, 0x00, 0x00, 0x00, // 流ID: 0
		// PING载荷 (8字节)
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	}

	// 合并两个帧
	multiFrameData := append(settingsFrame, pingFrame...)

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
	t.Logf("发送包含多个HTTP/2帧的数据包")
	err := hTrack.ProcessPacket("", packetInfo)
	if err != nil {
		t.Fatalf("直接解析多帧数据失败: %v", err)
	}

	// 等待处理完成
	time.Sleep(200 * time.Millisecond)

	// 应该收到两个独立的帧请求
	receivedFrames := 0
	for i := 0; i < 2; i++ {
		select {
		case request := <-hTrack.GetRequestChan():
			receivedFrames++
			frameType := request.Headers.Get("X-HTTP2-Frame-Type")
			t.Logf("收到帧 %d: 类型=%s, 流ID=%d", receivedFrames, frameType, *request.StreamID)
			
			// 验证帧类型
			if receivedFrames == 1 && frameType != "SETTINGS" {
				t.Errorf("第一个帧应该是SETTINGS，实际为: %s", frameType)
			}
			if receivedFrames == 2 && frameType != "PING" {
				t.Errorf("第二个帧应该是PING，实际为: %s", frameType)
			}
			
		case <-time.After(2 * time.Second):
			t.Errorf("超时：未收到第%d个帧", i+1)
			return
		}
	}

	if receivedFrames != 2 {
		t.Errorf("期望收到2个帧，实际收到: %d", receivedFrames)
	}

	t.Logf("✅ HTTP/2多帧直接解析测试成功")
}