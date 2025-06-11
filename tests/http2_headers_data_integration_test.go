package tests

import (
	"bytes"
	"testing"

	"github.com/danl5/htrack/parser"
	"golang.org/x/net/http2/hpack"
)

// TestHTTP2HeadersDataIntegration 测试HTTP/2 HEADERS帧和DATA帧之间的重组
func TestHTTP2HeadersDataIntegration(t *testing.T) {
	parser := parser.NewHTTP2Parser()

	// 测试响应的HEADERS和DATA帧重组
	t.Run("ResponseHeadersDataIntegration", func(t *testing.T) {
		// 1. 先发送响应HEADERS帧
		responseHeadersFrame := createResponseHeadersFrameD(t, 1, false, map[string]string{
			":status":        "200",
			"content-type":   "application/json",
			"content-length": "25",
			"server":         "test-server/1.0",
		})

		resps1, err := parser.ParseResponse("test-conn", responseHeadersFrame)
		if err != nil {
			t.Fatalf("解析响应HEADERS帧失败: %v", err)
		}
		// 根据HTTP/2协议，HEADERS帧没有END_STREAM标志时不应该返回响应对象
		if len(resps1) != 0 {
			t.Fatal("解析响应HEADERS帧应该返回空数组（没有END_STREAM标志）")
		}

		// 2. 然后发送响应DATA帧
		responseData := []byte(`{"message": "Hello World"}`)
		responseDataFrame := createResponseDataFrameID(t, 1, responseData, true)

		resps2, err := parser.ParseResponse("test-conn", responseDataFrame)
		if err != nil {
			t.Fatalf("解析响应DATA帧失败: %v", err)
		}
		if len(resps2) == 0 {
			t.Fatal("解析响应DATA帧返回空数组")
		}
		resp2 := resps2[0]
		if resp2 == nil {
			t.Fatal("解析响应DATA帧返回nil对象")
		}

		// 验证DATA帧解析结果 - 应该包含完整的头部和数据
		if resp2.StatusCode != 200 {
			t.Errorf("期望状态码为200，实际为: %d", resp2.StatusCode)
		}
		if resp2.Headers.Get("content-type") != "application/json" {
			t.Errorf("期望content-type为'application/json'，实际为: %s", resp2.Headers.Get("content-type"))
		}
		if resp2.Headers.Get("server") != "test-server/1.0" {
			t.Errorf("期望server为'test-server/1.0'，实际为: %s", resp2.Headers.Get("server"))
		}
		if !resp2.Complete {
			t.Error("DATA帧应该标记响应为完整（有END_STREAM标志）")
		}
		if string(resp2.Body) != string(responseData) {
			t.Errorf("期望响应体为'%s'，实际为: %s", string(responseData), string(resp2.Body))
		}

		t.Logf("HEADERS和DATA帧重组成功:")
		t.Logf("  状态码: %d", resp2.StatusCode)
		t.Logf("  头部数量: %d", len(resp2.Headers))
		t.Logf("  响应体: %s", string(resp2.Body))
		t.Logf("  完整性: %t", resp2.Complete)
	})

	// 测试请求的HEADERS和DATA帧重组
	t.Run("RequestHeadersDataIntegration", func(t *testing.T) {
		// 1. 先发送请求HEADERS帧
		requestHeadersFrame := createHeadersFrameWithStreamID(t, 3, false, true, map[string]string{
			"content-type":   "application/json",
			"content-length": "25",
			"user-agent":     "test-client/1.0",
		})

		reqs1, err := parser.ParseRequest("test-conn", requestHeadersFrame)
		if err != nil {
			t.Fatalf("解析请求HEADERS帧失败: %v", err)
		}
		// 根据HTTP/2协议，HEADERS帧没有END_STREAM标志时不应该返回请求对象
		// 因为请求还未完成，需要等待DATA帧
		if len(reqs1) != 0 {
			t.Error("HEADERS帧没有END_STREAM标志时不应该返回请求对象")
		}

		// 2. 然后发送请求DATA帧
		requestData := []byte(`{"query": "test data"}`)
		requestDataFrame := createDataFrameWithStreamID(t, 3, requestData, true)

		reqs2, err := parser.ParseRequest("test-conn", requestDataFrame)
		if err != nil {
			t.Fatalf("解析请求DATA帧失败: %v", err)
		}
		if len(reqs2) == 0 {
			t.Fatal("解析请求DATA帧返回空数组")
		}
		req2 := reqs2[0]
		if req2 == nil {
			t.Fatal("解析请求DATA帧返回nil对象")
		}

		// 验证DATA帧解析结果 - 应该包含完整的头部和数据
		if req2.Method != "GET" {
			t.Errorf("期望请求方法为GET，实际为: %s", req2.Method)
		}
		if req2.Headers.Get("content-type") != "application/json" {
			t.Errorf("期望content-type为'application/json'，实际为: %s", req2.Headers.Get("content-type"))
		}
		if req2.Headers.Get("user-agent") != "test-client/1.0" {
			t.Errorf("期望user-agent为'test-client/1.0'，实际为: %s", req2.Headers.Get("user-agent"))
		}
		if !req2.Complete {
			t.Error("DATA帧应该标记请求为完整（有END_STREAM标志）")
		}
		if string(req2.Body) != string(requestData) {
			t.Errorf("期望请求体为'%s'，实际为: %s", string(requestData), string(req2.Body))
		}

		t.Logf("请求HEADERS和DATA帧重组成功:")
		t.Logf("  方法: %s", req2.Method)
		t.Logf("  头部数量: %d", len(req2.Headers))
		t.Logf("  请求体: %s", string(req2.Body))
		t.Logf("  完整性: %t", req2.Complete)
	})
}

// 辅助函数：创建响应HEADERS帧
func createResponseHeadersFrameD(t *testing.T, streamID uint32, endStream bool, headers map[string]string) []byte {
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 编码响应头部
	for name, value := range headers {
		if err := encoder.WriteField(hpack.HeaderField{Name: name, Value: value}); err != nil {
			t.Fatalf("编码头部字段失败: %v", err)
		}
	}

	headerBlock := buf.Bytes()

	// 构建HEADERS帧
	frameHeader := make([]byte, 9)
	// Length (3 bytes)
	frameHeader[0] = byte(len(headerBlock) >> 16)
	frameHeader[1] = byte(len(headerBlock) >> 8)
	frameHeader[2] = byte(len(headerBlock))
	// Type (1 byte) - HEADERS = 1
	frameHeader[3] = 1
	// Flags (1 byte)
	flags := byte(0x04) // END_HEADERS
	if endStream {
		flags |= 0x01 // END_STREAM
	}
	frameHeader[4] = flags
	// Stream ID (4 bytes)
	frameHeader[5] = byte(streamID >> 24)
	frameHeader[6] = byte(streamID >> 16)
	frameHeader[7] = byte(streamID >> 8)
	frameHeader[8] = byte(streamID)

	// 组合帧头和数据
	frame := append(frameHeader, headerBlock...)
	return frame
}

// 辅助函数：创建响应DATA帧
func createResponseDataFrameID(t *testing.T, streamID uint32, data []byte, endStream bool) []byte {
	// 构建DATA帧
	frameHeader := make([]byte, 9)
	// Length (3 bytes)
	frameHeader[0] = byte(len(data) >> 16)
	frameHeader[1] = byte(len(data) >> 8)
	frameHeader[2] = byte(len(data))
	// Type (1 byte) - DATA = 0
	frameHeader[3] = 0
	// Flags (1 byte)
	flags := byte(0)
	if endStream {
		flags |= 0x01 // END_STREAM
	}
	frameHeader[4] = flags
	// Stream ID (4 bytes)
	frameHeader[5] = byte(streamID >> 24)
	frameHeader[6] = byte(streamID >> 16)
	frameHeader[7] = byte(streamID >> 8)
	frameHeader[8] = byte(streamID)

	// 组合帧头和数据
	frame := append(frameHeader, data...)
	return frame
}
