package tests

import (
	"testing"

	"github.com/danl5/htrack/parser"
	"github.com/danl5/htrack/types"
)

// TestTLSGenericParser_DetectVersion 测试TLS版本检测
func TestTLSGenericParser_DetectVersion(t *testing.T) {
	p := parser.NewTLSGenericParser()

	tests := []struct {
		name     string
		data     []byte
		expected types.HTTPVersion
	}{
		{
			name:     "Valid TLS 1.2 Handshake",
			data:     []byte{0x16, 0x03, 0x03, 0x00, 0x10, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
			expected: types.TLS_OTHER,
		},
		{
			name:     "Valid TLS 1.3 Application Data",
			data:     []byte{0x17, 0x03, 0x04, 0x00, 0x08, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
			expected: types.TLS_OTHER,
		},
		{
			name:     "Valid TLS Alert",
			data:     []byte{0x15, 0x03, 0x03, 0x00, 0x02, 0x01, 0x00},
			expected: types.TLS_OTHER,
		},
		{
			name:     "Insufficient Data",
			data:     []byte{0x16, 0x03},
			expected: types.Unknown,
		},
		{
			name:     "Invalid Record Type",
			data:     []byte{0xFF, 0x03, 0x03, 0x00, 0x10},
			expected: types.Unknown,
		},
		{
			name:     "Invalid TLS Version",
			data:     []byte{0x16, 0xFF, 0xFF, 0x00, 0x10},
			expected: types.Unknown,
		},
		{
			name:     "Length Too Large",
			data:     []byte{0x16, 0x03, 0x03, 0xFF, 0xFF},
			expected: types.Unknown,
		},
		{
			name:     "HTTP Request (should not match)",
			data:     []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
			expected: types.Unknown,
		},
		{
			name:     "Empty Data",
			data:     []byte{},
			expected: types.Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.DetectVersion(tt.data)
			if result != tt.expected {
				t.Errorf("DetectVersion() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestTLSGenericParser_ParseTLSRecord 测试TLS记录解析
func TestTLSGenericParser_ParseTLSRecord(t *testing.T) {
	p := parser.NewTLSGenericParser()

	tests := []struct {
		name            string
		data            []byte
		expectError     bool
		expectedType    uint8
		expectedVersion uint16
		expectedLength  uint16
	}{
		{
			name:            "Valid Handshake Record",
			data:            []byte{0x16, 0x03, 0x03, 0x00, 0x04, 0x01, 0x02, 0x03, 0x04},
			expectError:     false,
			expectedType:    0x16,
			expectedVersion: 0x0303,
			expectedLength:  4,
		},
		{
			name:        "Insufficient Header Data",
			data:        []byte{0x16, 0x03},
			expectError: true,
		},
		{
			name:        "Insufficient Payload Data",
			data:        []byte{0x16, 0x03, 0x03, 0x00, 0x10, 0x01, 0x02},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, err := p.ParseTLSRecord(tt.data)
			if tt.expectError {
				if err == nil {
					t.Errorf("ParseTLSRecord() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseTLSRecord() unexpected error: %v", err)
				return
			}

			if record.Type != tt.expectedType {
				t.Errorf("ParseTLSRecord() Type = %v, want %v", record.Type, tt.expectedType)
			}
			if record.Version != tt.expectedVersion {
				t.Errorf("ParseTLSRecord() Version = %v, want %v", record.Version, tt.expectedVersion)
			}
			if record.Length != tt.expectedLength {
				t.Errorf("ParseTLSRecord() Length = %v, want %v", record.Length, tt.expectedLength)
			}
		})
	}
}

// TestTLSGenericParser_ParseRequest 测试TLS请求解析
func TestTLSGenericParser_ParseRequest(t *testing.T) {
	p := parser.NewTLSGenericParser()

	// 创建测试数据
	tlsHandshake := []byte{0x16, 0x03, 0x03, 0x00, 0x04, 0x01, 0x02, 0x03, 0x04}
	packetInfo := &types.PacketInfo{
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.1",
			SrcPort: 12345,
			DstIP:   "192.168.1.2",
			DstPort: 443,
		},
		Direction: types.DirectionRequest,
		TimeDiff:  0,
	}

	requests, err := p.ParseRequest("test-conn", tlsHandshake, packetInfo)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if len(requests) != 1 {
		t.Fatalf("ParseRequest() returned %d requests, want 1", len(requests))
	}

	req := requests[0]
	if req.Method != "TLS" {
		t.Errorf("ParseRequest() Method = %v, want TLS", req.Method)
	}
	if req.Proto != "TLS/Other" {
		t.Errorf("ParseRequest() Proto = %v, want TLS/Other", req.Proto)
	}
	if !req.Complete {
		t.Errorf("ParseRequest() Complete = %v, want true", req.Complete)
	}

	// 检查TLS特定的头部
	if req.Headers["TLS-Record-Type"][0] != "22" { // 0x16 = 22
		t.Errorf("ParseRequest() TLS-Record-Type = %v, want 22", req.Headers["TLS-Record-Type"][0])
	}
	if req.Headers["TLS-Version"][0] != "0x0303" {
		t.Errorf("ParseRequest() TLS-Version = %v, want 0x0303", req.Headers["TLS-Version"][0])
	}
}

// TestTLSGenericParser_ParseResponse 测试TLS响应解析
func TestTLSGenericParser_ParseResponse(t *testing.T) {
	p := parser.NewTLSGenericParser()

	// 创建测试数据
	tlsApplicationData := []byte{0x17, 0x03, 0x03, 0x00, 0x08, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	packetInfo := &types.PacketInfo{
		TCPTuple: &types.TCPTuple{
			SrcIP:   "192.168.1.2",
			SrcPort: 443,
			DstIP:   "192.168.1.1",
			DstPort: 12345,
		},
		Direction: types.DirectionResponse,
		TimeDiff:  0,
	}

	responses, err := p.ParseResponse("test-conn", tlsApplicationData, packetInfo)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("ParseResponse() returned %d responses, want 1", len(responses))
	}

	resp := responses[0]
	if resp.Status != "TLS Record" {
		t.Errorf("ParseResponse() Status = %v, want TLS Record", resp.Status)
	}
	if resp.StatusCode != 23 { // 0x17 = 23
		t.Errorf("ParseResponse() StatusCode = %v, want 23", resp.StatusCode)
	}
	if resp.Proto != "TLS/Other" {
		t.Errorf("ParseResponse() Proto = %v, want TLS/Other", resp.Proto)
	}
	if !resp.Complete {
		t.Errorf("ParseResponse() Complete = %v, want true", resp.Complete)
	}

	// 检查TLS特定的头部
	if resp.Headers["TLS-Record-Type"][0] != "23" { // 0x17 = 23
		t.Errorf("ParseResponse() TLS-Record-Type = %v, want 23", resp.Headers["TLS-Record-Type"][0])
	}
	if resp.Headers["TLS-Version"][0] != "0x0303" {
		t.Errorf("ParseResponse() TLS-Version = %v, want 0x0303", resp.Headers["TLS-Version"][0])
	}
}

// TestTLSGenericParser_IsComplete 测试TLS数据完整性检查
func TestTLSGenericParser_IsComplete(t *testing.T) {
	p := parser.NewTLSGenericParser()

	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{
			name:     "Complete TLS Record",
			data:     []byte{0x16, 0x03, 0x03, 0x00, 0x04, 0x01, 0x02, 0x03, 0x04},
			expected: true,
		},
		{
			name:     "Incomplete Header",
			data:     []byte{0x16, 0x03},
			expected: false,
		},
		{
			name:     "Incomplete Payload",
			data:     []byte{0x16, 0x03, 0x03, 0x00, 0x10, 0x01, 0x02},
			expected: false,
		},
		{
			name:     "Empty Data",
			data:     []byte{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.IsComplete(tt.data)
			if result != tt.expected {
				t.Errorf("IsComplete() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestTLSGenericParser_GetRequiredBytes 测试获取所需字节数
func TestTLSGenericParser_GetRequiredBytes(t *testing.T) {
	p := parser.NewTLSGenericParser()

	tests := []struct {
		name     string
		data     []byte
		expected int
	}{
		{
			name:     "Complete Record",
			data:     []byte{0x16, 0x03, 0x03, 0x00, 0x04, 0x01, 0x02, 0x03, 0x04},
			expected: 0,
		},
		{
			name:     "Need Header Bytes",
			data:     []byte{0x16, 0x03},
			expected: 3,
		},
		{
			name:     "Need Payload Bytes",
			data:     []byte{0x16, 0x03, 0x03, 0x00, 0x10, 0x01, 0x02},
			expected: 14, // 需要16字节payload，已有2字节
		},
		{
			name:     "Empty Data",
			data:     []byte{},
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.GetRequiredBytes(tt.data)
			if result != tt.expected {
				t.Errorf("GetRequiredBytes() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestTLSHelperFunctions 测试TLS辅助函数
func TestTLSHelperFunctions(t *testing.T) {
	// 测试记录类型名称
	tests := []struct {
		recordType uint8
		expected   string
	}{
		{0x14, "ChangeCipherSpec"},
		{0x15, "Alert"},
		{0x16, "Handshake"},
		{0x17, "ApplicationData"},
		{0x18, "Heartbeat"},
		{0xFF, "Unknown"},
	}

	for _, tt := range tests {
		result := parser.GetTLSRecordTypeName(tt.recordType)
		if result != tt.expected {
			t.Errorf("GetTLSRecordTypeName(%d) = %v, want %v", tt.recordType, result, tt.expected)
		}
	}

	// 测试版本名称
	versionTests := []struct {
		version  uint16
		expected string
	}{
		{0x0301, "TLS 1.0"},
		{0x0302, "TLS 1.1"},
		{0x0303, "TLS 1.2"},
		{0x0304, "TLS 1.3"},
		{0x0305, "TLS 0x0305"},
	}

	for _, tt := range versionTests {
		result := parser.GetTLSVersionName(tt.version)
		if result != tt.expected {
			t.Errorf("GetTLSVersionName(0x%04x) = %v, want %v", tt.version, result, tt.expected)
		}
	}
}
