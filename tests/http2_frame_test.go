package tests

import (
	"encoding/hex"
	"testing"

	"github.com/danl5/htrack/parser"
	"github.com/danl5/htrack/parser/http2"
)

// TestFrameHeaderParsing 测试帧头解析
func TestFrameHeaderParsing(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected http2.FrameHeader
		wantErr  bool
	}{
		{
			name: "SETTINGS帧",
			data: []byte{0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: http2.FrameHeader{
				Length:   0,
				Type:     http2.FrameSettings,
				Flags:    0,
				StreamID: 0,
			},
			wantErr: false,
		},
		{
			name: "HEADERS帧带END_STREAM标志",
			data: []byte{0x00, 0x00, 0x0C, 0x01, 0x05, 0x00, 0x00, 0x00, 0x01},
			expected: http2.FrameHeader{
				Length:   12,
				Type:     http2.FrameHeaders,
				Flags:    0x05, // END_STREAM | END_HEADERS
				StreamID: 1,
			},
			wantErr: false,
		},
		{
			name: "DATA帧",
			data: []byte{0x00, 0x00, 0x08, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01},
			expected: http2.FrameHeader{
				Length:   8,
				Type:     http2.FrameData,
				Flags:    0x01, // END_STREAM
				StreamID: 1,
			},
			wantErr: false,
		},
		{
			name:    "数据不足",
			data:    []byte{0x00, 0x00, 0x08},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header, err := http2.ParseFrameHeader(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFrameHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if header.Length != tt.expected.Length {
					t.Errorf("Length = %v, want %v", header.Length, tt.expected.Length)
				}
				if header.Type != tt.expected.Type {
					t.Errorf("Type = %v, want %v", header.Type, tt.expected.Type)
				}
				if header.Flags != tt.expected.Flags {
					t.Errorf("Flags = %v, want %v", header.Flags, tt.expected.Flags)
				}
				if header.StreamID != tt.expected.StreamID {
					t.Errorf("StreamID = %v, want %v", header.StreamID, tt.expected.StreamID)
				}
			}
		})
	}
}

// TestFrameTypeIdentification 测试帧类型识别
func TestFrameTypeIdentification(t *testing.T) {
	tests := []struct {
		frameType http2.FrameType
		expected  string
	}{
		{http2.FrameData, "DATA"},
		{http2.FrameHeaders, "HEADERS"},
		{http2.FramePriority, "PRIORITY"},
		{http2.FrameRSTStream, "RST_STREAM"},
		{http2.FrameSettings, "SETTINGS"},
		{http2.FramePushPromise, "PUSH_PROMISE"},
		{http2.FramePing, "PING"},
		{http2.FrameGoAway, "GOAWAY"},
		{http2.FrameWindowUpdate, "WINDOW_UPDATE"},
		{http2.FrameContinuation, "CONTINUATION"},
		{http2.FrameType(255), "UNKNOWN(255)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.frameType.String(); got != tt.expected {
				t.Errorf("FrameType.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestHTTP2ParserFrameProcessing 测试HTTP/2解析器的帧处理
func TestHTTP2ParserFrameProcessing(t *testing.T) {
	parser := parser.NewHTTP2Parser()

	// 测试连接前导
	preface := "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
	reqs, err := parser.ParseRequest("test-conn", []byte(preface))
	if err != nil {
		t.Fatalf("解析连接前导失败: %v", err)
	}
	// 连接前导不再返回PRI请求对象
	if len(reqs) != 0 {
		t.Errorf("连接前导应该返回空数组，但返回了 %d 个请求", len(reqs))
	}

	// 测试SETTINGS帧
	settingsFrame := []byte{
		0x00, 0x00, 0x00, // Length: 0
		0x04,                   // Type: SETTINGS
		0x00,                   // Flags: 0
		0x00, 0x00, 0x00, 0x00, // Stream ID: 0
	}

	reqs, err = parser.ParseRequest("test-conn", settingsFrame)
	if err != nil {
		t.Fatalf("解析SETTINGS帧失败: %v", err)
	}
	if len(reqs) != 0 {
		t.Errorf("SETTINGS帧不应该返回请求对象")
	}

	// 测试简单的HEADERS帧（模拟GET请求）
	headersData := createSimpleHeadersFrame(t)
	reqs, err = parser.ParseRequest("test-conn", headersData)
	if err != nil {
		t.Fatalf("解析HEADERS帧失败: %v", err)
	}
	if len(reqs) == 0 {
		t.Fatal("HEADERS帧应该返回请求对象")
	}
	req := reqs[0]
	_ = req // 使用req变量避免未使用警告
}

// TestFrameHeaderWriting 测试帧头写入
func TestFrameHeaderWriting(t *testing.T) {
	header := http2.FrameHeader{
		Length:   100,
		Type:     http2.FrameData,
		Flags:    http2.FlagDataEndStream,
		StreamID: 1,
	}

	buf := make([]byte, 9)
	err := http2.WriteFrameHeader(header, buf)
	if err != nil {
		t.Fatalf("WriteFrameHeader() error = %v", err)
	}

	// 验证写入的数据
	parsed, err := http2.ParseFrameHeader(buf)
	if err != nil {
		t.Fatalf("ParseFrameHeader() error = %v", err)
	}

	if parsed.Length != header.Length {
		t.Errorf("Length = %v, want %v", parsed.Length, header.Length)
	}
	if parsed.Type != header.Type {
		t.Errorf("Type = %v, want %v", parsed.Type, header.Type)
	}
	if parsed.Flags != header.Flags {
		t.Errorf("Flags = %v, want %v", parsed.Flags, header.Flags)
	}
	if parsed.StreamID != header.StreamID {
		t.Errorf("StreamID = %v, want %v", parsed.StreamID, header.StreamID)
	}
}

// TestRealWorldFrames 测试真实世界的帧数据
func TestRealWorldFrames(t *testing.T) {
	// 这些是从实际HTTP/2通信中捕获的帧数据
	tests := []struct {
		name         string
		hex          string
		expectedType http2.FrameType
	}{
		{
			name:         "SETTINGS帧",
			hex:          "000012040000000000000200000000000300000064000400100000000500004000",
			expectedType: http2.FrameSettings,
		},
		{
			name:         "WINDOW_UPDATE帧",
			hex:          "000004080000000000000001",
			expectedType: http2.FrameWindowUpdate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := hex.DecodeString(tt.hex)
			if err != nil {
				t.Fatalf("解码十六进制数据失败: %v", err)
			}

			header, err := http2.ParseFrameHeader(data)
			if err != nil {
				t.Fatalf("解析帧头失败: %v", err)
			}

			if header.Type != tt.expectedType {
				t.Errorf("帧类型 = %v, 期望 %v", header.Type, tt.expectedType)
			}

			t.Logf("成功解析 %s: Length=%d, Type=%s, Flags=0x%02x, StreamID=%d",
				tt.name, header.Length, header.Type.String(), header.Flags, header.StreamID)
		})
	}
}

// BenchmarkFrameHeaderParsing 帧头解析性能测试
func BenchmarkFrameHeaderParsing(b *testing.B) {
	data := []byte{0x00, 0x00, 0x0C, 0x01, 0x05, 0x00, 0x00, 0x00, 0x01}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := http2.ParseFrameHeader(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}
