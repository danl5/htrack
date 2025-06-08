package tests

import (
	"bytes"
	"fmt"
	"testing"

	"golang.org/x/net/http2/hpack"
)

// HTTP2TestHelpers 提供HTTP/2测试相关的辅助函数

// createHeadersFrameWithStreamID 创建带指定流ID的HEADERS帧
func createHeadersFrameWithStreamID(t *testing.T, streamID uint32, endStream bool, endHeaders bool, customHeaders map[string]string) []byte {
	// 使用标准库HPACK编码器
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 编码HTTP/2伪头部
	encoder.WriteField(hpack.HeaderField{Name: ":method", Value: "GET"})
	encoder.WriteField(hpack.HeaderField{Name: ":scheme", Value: "http"})
	encoder.WriteField(hpack.HeaderField{Name: ":path", Value: "/"})
	encoder.WriteField(hpack.HeaderField{Name: ":authority", Value: "www.example.com"})

	// 编码自定义头部
	for name, value := range customHeaders {
		encoder.WriteField(hpack.HeaderField{Name: name, Value: value})
	}

	headerBlock := buf.Bytes()

	// 确保帧数据足够长
	minFrameSize := 24
	if len(headerBlock)+9 < minFrameSize {
		padding := make([]byte, minFrameSize-len(headerBlock)-9)
		headerBlock = append(headerBlock, padding...)
	}

	frame := make([]byte, 9+len(headerBlock))

	// 帧头
	frame[0] = byte(len(headerBlock) >> 16)
	frame[1] = byte(len(headerBlock) >> 8)
	frame[2] = byte(len(headerBlock))
	frame[3] = 0x01 // HEADERS

	// 设置标志
	flags := byte(0)
	if endStream {
		flags |= 0x01 // END_STREAM
	}
	if endHeaders {
		flags |= 0x04 // END_HEADERS
	}
	frame[4] = flags

	// 流ID
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)

	// 头部块
	copy(frame[9:], headerBlock)

	return frame
}

// createDataFrameWithStreamID 创建带指定流ID的DATA帧
func createDataFrameWithStreamID(t *testing.T, streamID uint32, data []byte, endStream bool) []byte {
	// 直接使用原始数据，不添加填充
	frameData := data

	frame := make([]byte, 9+len(frameData))

	// 帧头
	frame[0] = byte(len(frameData) >> 16)
	frame[1] = byte(len(frameData) >> 8)
	frame[2] = byte(len(frameData))
	frame[3] = 0x00 // DATA

	// 设置标志
	flags := byte(0)
	if endStream {
		flags |= 0x01 // END_STREAM
	}
	frame[4] = flags

	// 流ID
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)

	// 数据
	copy(frame[9:], frameData)

	return frame
}

// createResponseHeadersFrame 创建响应HEADERS帧
func createResponseHeadersFrame(t *testing.T, streamID uint32, statusCode int) []byte {
	// 使用标准库HPACK编码器创建响应头部
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 编码HTTP/2响应头部
	encoder.WriteField(hpack.HeaderField{Name: ":status", Value: fmt.Sprintf("%d", statusCode)})
	encoder.WriteField(hpack.HeaderField{Name: "content-type", Value: "text/html"})

	headerBlock := buf.Bytes()

	// 计算是否需要填充
	minFrameSize := 24
	frameDataSize := len(headerBlock)
	paddingLength := 0

	if frameDataSize+9 < minFrameSize {
		paddingLength = minFrameSize - frameDataSize - 9 - 1 // -1 for padding length field
		if paddingLength < 0 {
			paddingLength = 0
		}
	}

	var frameData []byte
	var flags byte = 0x04 // END_HEADERS

	if paddingLength > 0 {
		flags |= 0x08 // PADDED
		frameData = make([]byte, 1+len(headerBlock)+paddingLength)
		frameData[0] = byte(paddingLength) // padding length
		copy(frameData[1:], headerBlock)
		// padding bytes are already zero-initialized
	} else {
		frameData = headerBlock
	}

	frame := make([]byte, 9+len(frameData))

	// 帧头
	frame[0] = byte(len(frameData) >> 16)
	frame[1] = byte(len(frameData) >> 8)
	frame[2] = byte(len(frameData))
	frame[3] = 0x01 // HEADERS
	frame[4] = flags

	// 流ID
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)

	// 帧数据
	copy(frame[9:], frameData)

	return frame
}

// createContinuationFrame 创建CONTINUATION帧
func createContinuationFrame(t *testing.T, streamID uint32, endHeaders bool) []byte {
	// 使用标准库HPACK编码器创建剩余头部
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 编码剩余的头部（用于CONTINUATION帧）
	encoder.WriteField(hpack.HeaderField{Name: ":path", Value: "/"})
	encoder.WriteField(hpack.HeaderField{Name: ":authority", Value: "www.example.com"})

	headerBlock := buf.Bytes()

	// 确保帧数据足够长
	minFrameSize := 24
	if len(headerBlock)+9 < minFrameSize {
		padding := make([]byte, minFrameSize-len(headerBlock)-9)
		headerBlock = append(headerBlock, padding...)
	}

	frame := make([]byte, 9+len(headerBlock))

	// 帧头
	frame[0] = byte(len(headerBlock) >> 16)
	frame[1] = byte(len(headerBlock) >> 8)
	frame[2] = byte(len(headerBlock))
	frame[3] = 0x09 // CONTINUATION

	// 设置标志
	flags := byte(0)
	if endHeaders {
		flags |= 0x04 // END_HEADERS
	}
	frame[4] = flags

	// 流ID
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)

	// 头部块
	copy(frame[9:], headerBlock)

	return frame
}

// createHeadersFrameWithPriority 创建带优先级的HEADERS帧
func createHeadersFrameWithPriority(t *testing.T, streamID uint32, dependency uint32, weight uint8, exclusive bool) []byte {
	// 使用标准库HPACK编码器
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 编码HTTP/2伪头部
	encoder.WriteField(hpack.HeaderField{Name: ":method", Value: "GET"})
	encoder.WriteField(hpack.HeaderField{Name: ":scheme", Value: "http"})
	encoder.WriteField(hpack.HeaderField{Name: ":path", Value: "/"})
	encoder.WriteField(hpack.HeaderField{Name: ":authority", Value: "www.example.com"})

	headerBlock := buf.Bytes()

	// 优先级信息（5字节）
	priorityData := make([]byte, 5)
	if exclusive {
		dependency |= 0x80000000
	}
	priorityData[0] = byte(dependency >> 24)
	priorityData[1] = byte(dependency >> 16)
	priorityData[2] = byte(dependency >> 8)
	priorityData[3] = byte(dependency)
	priorityData[4] = weight

	// 组合优先级数据和头部块
	frameData := append(priorityData, headerBlock...)

	// 确保帧数据足够长
	minFrameSize := 24
	if len(frameData)+9 < minFrameSize {
		padding := make([]byte, minFrameSize-len(frameData)-9)
		frameData = append(frameData, padding...)
	}

	frame := make([]byte, 9+len(frameData))

	// 帧头
	frame[0] = byte(len(frameData) >> 16)
	frame[1] = byte(len(frameData) >> 8)
	frame[2] = byte(len(frameData))
	frame[3] = 0x01 // HEADERS
	frame[4] = 0x24 // END_HEADERS | PRIORITY

	// 流ID
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)

	// 帧数据
	copy(frame[9:], frameData)

	return frame
}

// createLargeHeadersFrame 创建包含大量头部的HEADERS帧（用于性能测试）
func createLargeHeadersFrame(t *testing.T, streamID uint32, endStream, endHeaders bool) []byte {
	// 创建包含大量头部的HEADERS帧
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 标准头部
	encoder.WriteField(hpack.HeaderField{Name: ":method", Value: "POST"})
	encoder.WriteField(hpack.HeaderField{Name: ":path", Value: "/api/v1/large-data"})
	encoder.WriteField(hpack.HeaderField{Name: ":scheme", Value: "https"})
	encoder.WriteField(hpack.HeaderField{Name: ":authority", Value: "example.com"})

	// 添加大量自定义头部
	for i := 0; i < 50; i++ {
		encoder.WriteField(hpack.HeaderField{
			Name:  fmt.Sprintf("x-custom-header-%d", i),
			Value: fmt.Sprintf("large-value-for-testing-performance-%d", i),
		})
	}

	headerBlock := buf.Bytes()
	frame := make([]byte, 9+len(headerBlock))

	// 帧头
	frame[0] = byte(len(headerBlock) >> 16)
	frame[1] = byte(len(headerBlock) >> 8)
	frame[2] = byte(len(headerBlock))
	frame[3] = 0x01 // HEADERS

	// 标志
	flags := byte(0)
	if endStream {
		flags |= 0x01 // END_STREAM
	}
	if endHeaders {
		flags |= 0x04 // END_HEADERS
	}
	frame[4] = flags

	// 流ID
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)

	// 头部块
	copy(frame[9:], headerBlock)

	return frame
}

// createLargeDataFrame 创建大型DATA帧（用于性能测试）
func createLargeDataFrame(t *testing.T, streamID uint32, size int, endStream bool) []byte {
	// 创建指定大小的数据
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	frame := make([]byte, 9+len(data))

	// 帧头
	frame[0] = byte(len(data) >> 16)
	frame[1] = byte(len(data) >> 8)
	frame[2] = byte(len(data))
	frame[3] = 0x00 // DATA

	// 标志
	flags := byte(0)
	if endStream {
		flags |= 0x01 // END_STREAM
	}
	frame[4] = flags

	// 流ID
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)

	// 数据
	copy(frame[9:], data)

	return frame
}

// createSimpleHeadersFrame 创建简单的HEADERS帧（用于基础测试）
func createSimpleHeadersFrame(t *testing.T) []byte {
	// 这是一个简化的HEADERS帧，包含基本的HTTP/2头部
	// 实际的HPACK编码会更复杂
	headerBlock := []byte{
		0x82, // :method: GET (索引2)
		0x86, // :scheme: http (索引6)
		0x84, // :path: / (索引4)
		0x01, // :authority: (字面量)
		0x0f, // 长度15
	}
	headerBlock = append(headerBlock, []byte("www.example.com")...)

	// 构建完整的HEADERS帧
	frame := make([]byte, 9+len(headerBlock))

	// 帧头
	frame[0] = byte(len(headerBlock) >> 16)
	frame[1] = byte(len(headerBlock) >> 8)
	frame[2] = byte(len(headerBlock))
	frame[3] = 0x01 // HEADERS
	frame[4] = 0x05 // END_STREAM | END_HEADERS
	frame[5] = 0x00
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x01 // Stream ID: 1

	// 头部块
	copy(frame[9:], headerBlock)

	return frame
}

// createResponseHeadersFrameWithCustomHeaders 创建带自定义头部的响应HEADERS帧
func createResponseHeadersFrameWithCustomHeaders(t *testing.T, streamID uint32, endStream bool, headers map[string]string) []byte {
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 编码响应头部
	for name, value := range headers {
		if err := encoder.WriteField(hpack.HeaderField{Name: name, Value: value}); err != nil {
			if t != nil {
				t.Fatalf("编码头部字段失败: %v", err)
			}
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

// createContinuationFrameWithRemainingHeaders 创建包含剩余头部的CONTINUATION帧
func createContinuationFrameWithRemainingHeaders(t *testing.T, streamID uint32, endHeaders bool, customHeaders map[string]string) []byte {
	// 使用标准库HPACK编码器创建剩余头部
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 编码剩余的自定义头部（跳过前两个，使用排序确保一致性）
	var keys []string
	for name := range customHeaders {
		keys = append(keys, name)
	}

	// 对键进行排序以确保一致的分片行为
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	// 编码剩余的头部（从第3个开始）
	for i := 2; i < len(keys); i++ {
		encoder.WriteField(hpack.HeaderField{Name: keys[i], Value: customHeaders[keys[i]]})
	}

	headerBlock := buf.Bytes()

	// 确保帧数据足够长
	minFrameSize := 24
	if len(headerBlock)+9 < minFrameSize {
		padding := make([]byte, minFrameSize-len(headerBlock)-9)
		headerBlock = append(headerBlock, padding...)
	}

	frame := make([]byte, 9+len(headerBlock))

	// 帧头
	frame[0] = byte(len(headerBlock) >> 16)
	frame[1] = byte(len(headerBlock) >> 8)
	frame[2] = byte(len(headerBlock))
	frame[3] = 0x09 // CONTINUATION

	// 设置标志
	flags := byte(0)
	if endHeaders {
		flags |= 0x04 // END_HEADERS
	}
	frame[4] = flags

	// 流ID
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)

	// 头部块
	copy(frame[9:], headerBlock)

	return frame
}

// createResponseDataFrame 创建响应DATA帧
func createResponseDataFrame(t *testing.T, streamID uint32, data []byte, endStream bool) []byte {
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

// 辅助函数：创建无效填充的HEADERS帧
func createInvalidPaddedHeadersFrame(t *testing.T, streamID uint32) []byte {
	headerBlock := []byte{0x82} // 简单的头部块

	// 创建无效的填充（填充长度大于数据长度）
	frameData := []byte{255} // 填充长度255，但数据很短
	frameData = append(frameData, headerBlock...)

	// 确保帧数据足够长
	minFrameSize := 24
	if len(frameData)+9 < minFrameSize {
		padding := make([]byte, minFrameSize-len(frameData)-9)
		frameData = append(frameData, padding...)
	}

	frame := make([]byte, 9+len(frameData))

	// 帧头
	frame[0] = byte(len(frameData) >> 16)
	frame[1] = byte(len(frameData) >> 8)
	frame[2] = byte(len(frameData))
	frame[3] = 0x01 // HEADERS
	frame[4] = 0x0C // END_HEADERS | PADDED

	// 流ID
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)

	// 帧数据
	copy(frame[9:], frameData)

	return frame
}

// 辅助函数：创建不带END_HEADERS的HEADERS帧
func createHeadersFrameWithoutEndHeaders(t *testing.T, streamID uint32, customHeaders map[string]string) []byte {
	// 使用标准库HPACK编码器创建部分头部
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 只编码部分头部（用于测试CONTINUATION）
	encoder.WriteField(hpack.HeaderField{Name: ":method", Value: "GET"})
	encoder.WriteField(hpack.HeaderField{Name: ":scheme", Value: "http"})

	// 编码部分自定义头部
	for name, value := range customHeaders {
		encoder.WriteField(hpack.HeaderField{Name: name, Value: value})
		break // 只编码一个自定义头部，其余留给CONTINUATION
	}

	headerBlock := buf.Bytes()

	// 确保帧数据足够长
	minFrameSize := 24
	if len(headerBlock)+9 < minFrameSize {
		padding := make([]byte, minFrameSize-len(headerBlock)-9)
		headerBlock = append(headerBlock, padding...)
	}

	frame := make([]byte, 9+len(headerBlock))

	// 帧头
	frame[0] = byte(len(headerBlock) >> 16)
	frame[1] = byte(len(headerBlock) >> 8)
	frame[2] = byte(len(headerBlock))
	frame[3] = 0x01 // HEADERS
	frame[4] = 0x00 // 没有END_HEADERS标志

	// 流ID
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)

	// 头部块
	copy(frame[9:], headerBlock)

	return frame
}

// 辅助函数：创建分片的HEADERS帧
func createFragmentedHeadersFrame(t *testing.T, streamID uint32, endStream bool, customHeaders map[string]string) []byte {
	// 使用标准库HPACK编码器创建部分头部
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 编码所有必需的伪头部和部分自定义头部
	encoder.WriteField(hpack.HeaderField{Name: ":method", Value: "POST"})
	encoder.WriteField(hpack.HeaderField{Name: ":scheme", Value: "https"})
	encoder.WriteField(hpack.HeaderField{Name: ":path", Value: "/test"})
	encoder.WriteField(hpack.HeaderField{Name: ":authority", Value: "example.com"})

	// 编码部分自定义头部（使用排序确保一致性）
	var keys []string
	for name := range customHeaders {
		keys = append(keys, name)
	}
	// 对键进行排序以确保一致的分片行为
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	// 只编码前两个头部，其余留给CONTINUATION
	for i := 0; i < 2 && i < len(keys); i++ {
		encoder.WriteField(hpack.HeaderField{Name: keys[i], Value: customHeaders[keys[i]]})
	}

	headerBlock := buf.Bytes()

	// 确保帧数据足够长
	minFrameSize := 24
	if len(headerBlock)+9 < minFrameSize {
		padding := make([]byte, minFrameSize-len(headerBlock)-9)
		headerBlock = append(headerBlock, padding...)
	}

	frame := make([]byte, 9+len(headerBlock))

	// 帧头
	frame[0] = byte(len(headerBlock) >> 16)
	frame[1] = byte(len(headerBlock) >> 8)
	frame[2] = byte(len(headerBlock))
	frame[3] = 0x01 // HEADERS

	// 设置标志（不设置END_HEADERS，表示后续有CONTINUATION帧）
	flags := byte(0)
	if endStream {
		flags |= 0x01 // END_STREAM
	}
	// 注意：不设置END_HEADERS标志
	frame[4] = flags

	// 流ID
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)

	// 头部块
	copy(frame[9:], headerBlock)

	return frame
}
