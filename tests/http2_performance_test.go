package tests

import (
	"bytes"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/danl5/htrack/parser"
	"github.com/danl5/htrack/types"
	"golang.org/x/net/http2/hpack"
)

// TestLargeDataFrameProcessing 测试大量数据帧的处理
func TestLargeDataFrameProcessing(t *testing.T) {
	parser := parser.NewHTTP2Parser()
	connID := "large-data-test"

	// 测试不同大小的数据帧（限制在HTTP/2最大帧大小16KB内）
	dataSizes := []int{1024, 8192, 16000} // 1KB, 8KB, ~16KB

	for i, size := range dataSizes {
		t.Run(fmt.Sprintf("DataSize_%dB", size), func(t *testing.T) {
			// 为每个子测试使用不同的streamID避免冲突
			streamID := uint32(i + 1)
			// 创建HEADERS帧（不带END_STREAM），然后创建DATA帧（带END_STREAM）
			headersFrame := createLargeHeadersFrame(t, streamID, false, true)
			_, err := parser.ParseRequest(connID, headersFrame, &types.PacketInfo{
				Data:      headersFrame,
				Direction: types.DirectionRequest,
				TCPTuple:  &types.TCPTuple{},
			})
			if err != nil {
				t.Fatalf("解析HEADERS帧失败: %v", err)
			}

			// 创建大数据帧测试数据处理性能
			dataFrame := createLargeDataFrame(t, streamID, size, true)
			start := time.Now()
			reqs, err := parser.ParseRequest(connID, dataFrame, &types.PacketInfo{
				Data:      dataFrame,
				Direction: types.DirectionRequest,
				TCPTuple:  &types.TCPTuple{},
			})
			duration := time.Since(start)
			if err != nil {
				t.Fatalf("解析大数据帧失败 (size=%d): %v", size, err)
			}
			if len(reqs) == 0 {
				t.Fatalf("大数据帧解析返回空数组 (size=%d)", size)
			}

			t.Logf("处理 %d 字节数据帧耗时: %v", size, duration)

			// 验证性能要求：16KB数据应在10ms内处理完成
			if size >= 16000 && duration > 10*time.Millisecond {
				t.Errorf("大数据帧处理性能不足: %v > 10ms", duration)
			}
		})
	}
}

// TestFragmentedFrameReassembly 测试分片帧的重组
func TestFragmentedFrameReassembly(t *testing.T) {
	parser := parser.NewHTTP2Parser()
	connID := "fragment-test"

	// 测试HEADERS帧分片重组
	t.Run("HeadersFragmentation", func(t *testing.T) {
		// 创建分片的HEADERS帧
		headersFrame := createFragmentedHeadersFramePerf(t, 1, false, false)
		continuationFrames := createMultipleContinuationFrames(t, 1, 5) // 5个CONTINUATION帧

		// 解析初始HEADERS帧
		reqs, err := parser.ParseRequest(connID, headersFrame, &types.PacketInfo{
			Data:      headersFrame,
			Direction: types.DirectionRequest,
			TCPTuple:  &types.TCPTuple{},
		})
		if err != nil {
			t.Fatalf("解析分片HEADERS帧失败: %v", err)
		}
		// 分片HEADERS帧应该返回空数组
		if len(reqs) != 0 {
			t.Errorf("分片HEADERS帧应该返回空数组，实际返回%d个请求", len(reqs))
		}

		// 逐个解析CONTINUATION帧
		for i, contFrame := range continuationFrames {
			reqs, err = parser.ParseRequest(connID, contFrame, &types.PacketInfo{
				Data:      contFrame,
				Direction: types.DirectionRequest,
				TCPTuple:  &types.TCPTuple{},
			})
			if err != nil {
				t.Fatalf("解析CONTINUATION帧 %d 失败: %v", i, err)
			}
			// CONTINUATION帧没有END_STREAM标志，应该返回空数组
			if len(reqs) != 0 {
				t.Errorf("CONTINUATION帧 %d 应该返回空数组，实际返回%d个请求", i, len(reqs))
			}
		}

		// 发送DATA帧完成请求
		data := []byte("test data")
		dataFrame := createDataFrameWithStreamID(t, 1, data, true)
		reqs, err = parser.ParseRequest(connID, dataFrame, &types.PacketInfo{
			Data:      dataFrame,
			Direction: types.DirectionRequest,
			TCPTuple:  &types.TCPTuple{},
		})
		if err != nil {
			t.Fatalf("解析DATA帧失败: %v", err)
		}
		if len(reqs) == 0 {
			t.Fatal("分片重组后应返回完整请求")
		}
		req := reqs[0]
		if *req.StreamID != 1 {
			t.Errorf("期望流ID为1，实际为: %d", *req.StreamID)
		}
	})

	// 测试DATA帧分片重组
	t.Run("DataFragmentation", func(t *testing.T) {
		// 先建立流（不带END_STREAM标志）
		headersFrame := createLargeHeadersFrame(t, 3, false, true)
		reqs, err := parser.ParseRequest(connID, headersFrame, &types.PacketInfo{
			Data:      headersFrame,
			Direction: types.DirectionRequest,
			TCPTuple:  &types.TCPTuple{},
		})
		if err != nil {
			t.Fatalf("建立流失败: %v", err)
		}
		// HEADERS帧没有END_STREAM标志，应该返回空数组
		if len(reqs) != 0 {
			t.Errorf("HEADERS帧应该返回空数组，实际返回%d个请求", len(reqs))
		}

		// 创建多个DATA帧片段
		dataFragments := createDataFragments(t, 3, 10, 1024) // 10个1KB的片段
		var totalData []byte

		var requests []*types.HTTPRequest
		for i, fragment := range dataFragments {
			isLast := i == len(dataFragments)-1
			if isLast {
				// 最后一个片段设置END_STREAM标志
				fragment[4] |= 0x01
			}

			reqs, err := parser.ParseRequest(connID, fragment, &types.PacketInfo{
				Data:      fragment,
				Direction: types.DirectionRequest,
				TCPTuple:  &types.TCPTuple{},
			})
			if err != nil {
				t.Fatalf("解析DATA片段 %d 失败: %v", i, err)
			}

			// 累积数据用于验证
			totalData = append(totalData, fragment[9:]...)

			// 只有最后一个DATA帧（带END_STREAM标志）应该返回完整请求
			if isLast {
				if len(reqs) == 0 {
					t.Error("最后的DATA片段应返回完整请求")
				} else {
					requests = append(requests, reqs[0])
				}
			} else {
				// 中间的DATA帧不应该返回请求
				if len(reqs) != 0 {
					t.Errorf("DATA片段 %d 应该返回空数组，实际返回%d个请求", i, len(reqs))
				}
			}
		}

		if len(requests) != 1 {
			t.Errorf("期望 1 个完整请求，实际 %d 个", len(requests))
		}
	})
}

// TestConcurrentStreamProcessing 测试并发流处理
func TestConcurrentStreamProcessing(t *testing.T) {
	// 使用共享的parser实例来测试真正的并发功能
	sharedParser := parser.NewHTTP2Parser()
	connID := "concurrent-test"
	numStreams := 100
	numGoroutines := 10

	var wg sync.WaitGroup
	errorChan := make(chan error, numStreams)
	successChan := make(chan int, numStreams)

	// 启动多个goroutine并发处理流
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			streamsPerGoroutine := numStreams / numGoroutines

			for i := 0; i < streamsPerGoroutine; i++ {
				// 使用全局唯一的流ID避免冲突
				streamID := uint32(goroutineID*streamsPerGoroutine*2 + i*2 + 1) // 确保是奇数流ID（客户端发起）

				// 创建HEADERS帧
				headersFrame := createConcurrentTestFrame(t, streamID, true, true, goroutineID, i)
				reqs, err := sharedParser.ParseRequest(connID, headersFrame, &types.PacketInfo{
					Data:      headersFrame,
					Direction: types.DirectionRequest,
					TCPTuple:  &types.TCPTuple{},
				})
				if err != nil {
					errorChan <- fmt.Errorf("goroutine %d, stream %d: %v", goroutineID, streamID, err)
					return
				}
				if len(reqs) == 0 {
					errorChan <- fmt.Errorf("goroutine %d, stream %d: 返回空数组", goroutineID, streamID)
					return
				}
				req := reqs[0]
				if *req.StreamID != streamID {
					errorChan <- fmt.Errorf("goroutine %d: 期望流ID %d，实际 %d", goroutineID, streamID, *req.StreamID)
					return
				}

				successChan <- int(streamID)
			}
		}(g)
	}

	// 等待所有goroutine完成
	wg.Wait()
	close(errorChan)
	close(successChan)

	// 检查错误
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		t.Errorf("并发处理出现 %d 个错误:", len(errors))
		for _, err := range errors {
			t.Errorf("  %v", err)
		}
	}

	// 检查成功处理的流数量
	successCount := 0
	for range successChan {
		successCount++
	}

	if successCount != numStreams {
		t.Errorf("期望处理 %d 个流，实际处理 %d 个", numStreams, successCount)
	}

	t.Logf("成功并发处理 %d 个流", successCount)
}

// TestMemoryUsageUnderLoad 测试高负载下的内存使用
func TestMemoryUsageUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过内存压力测试")
	}

	parser := parser.NewHTTP2Parser()
	connID := "memory-test"

	// 记录初始内存
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// 创建大量流和数据
	numStreams := 1000
	dataSize := 8192 // 8KB per stream

	for i := 0; i < numStreams; i++ {
		streamID := uint32(i*2 + 1)

		// 创建HEADERS帧
		headersFrame := createLargeHeadersFrame(t, streamID, false, true)
		_, err := parser.ParseRequest(connID, headersFrame, &types.PacketInfo{
			Data:      headersFrame,
			Direction: types.DirectionRequest,
			TCPTuple:  &types.TCPTuple{},
		})
		if err != nil {
			t.Fatalf("流 %d HEADERS解析失败: %v", streamID, err)
		}

		// 创建DATA帧
		dataFrame := createLargeDataFrame(t, streamID, dataSize, true)
		_, err = parser.ParseResponse(connID, dataFrame, &types.PacketInfo{
			Data:      dataFrame,
			Direction: types.DirectionResponse,
			TCPTuple:  &types.TCPTuple{},
		})
		if err != nil {
			t.Fatalf("流 %d DATA解析失败: %v", streamID, err)
		}
	}

	// 记录处理后内存
	runtime.GC()
	runtime.ReadMemStats(&m2)

	// 安全计算内存增长，避免uint64下溢
	var memoryIncrease uint64
	if m2.Alloc > m1.Alloc {
		memoryIncrease = m2.Alloc - m1.Alloc
	} else {
		// 如果内存减少了，说明GC回收了更多内存，设为0
		memoryIncrease = 0
	}

	var memoryPerStream uint64
	if numStreams > 0 {
		memoryPerStream = memoryIncrease / uint64(numStreams)
	}

	t.Logf("处理 %d 个流，内存增长: %d bytes (%.2f KB)", numStreams, memoryIncrease, float64(memoryIncrease)/1024)
	t.Logf("平均每流内存使用: %d bytes", memoryPerStream)

	// 验证内存使用合理性（每流不应超过100KB）
	// 只有在实际有内存增长时才检查
	if memoryIncrease > 0 && memoryPerStream > 100*1024 {
		t.Errorf("每流内存使用过高: %d bytes > 100KB", memoryPerStream)
	}
}

// BenchmarkLargeFrameProcessing 大帧处理性能基准测试
func BenchmarkLargeFrameProcessing(b *testing.B) {
	sizes := []int{1024, 8192, 65536, 1048576}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size_%dB", size), func(b *testing.B) {
			parser := parser.NewHTTP2Parser()
			headersFrame := createLargeHeadersFrame(nil, 1, false, true)
			dataFrame := createLargeDataFrame(nil, 1, size, true)

			b.ResetTimer()
			b.SetBytes(int64(size))

			for i := 0; i < b.N; i++ {
				// 每次迭代使用新的连接ID避免状态干扰
				connID := fmt.Sprintf("bench-%d", i)
				_, err := parser.ParseRequest(connID, headersFrame, &types.PacketInfo{
				Data:      headersFrame,
				Direction: types.DirectionRequest,
				TCPTuple:  &types.TCPTuple{},
			})
				if err != nil {
					b.Fatal(err)
				}
				_, err = parser.ParseResponse(connID, dataFrame, &types.PacketInfo{
				Data:      dataFrame,
				Direction: types.DirectionResponse,
				TCPTuple:  &types.TCPTuple{},
			})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkConcurrentProcessing 并发处理性能基准测试
func BenchmarkConcurrentProcessing(b *testing.B) {
	concurrencyLevels := []int{1, 2, 4, 8, 16}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(b *testing.B) {
			headersFrame := createLargeHeadersFrame(nil, 1, true, true)

			b.ResetTimer()
			b.SetParallelism(concurrency)
			b.RunParallel(func(pb *testing.PB) {
				// 每个goroutine使用独立的解析器实例
				parser := parser.NewHTTP2Parser()
				i := 0
				for pb.Next() {
					connID := fmt.Sprintf("bench-concurrent-%d", i)
					_, err := parser.ParseRequest(connID, headersFrame, &types.PacketInfo{
					Data:      headersFrame,
					Direction: types.DirectionRequest,
					TCPTuple:  &types.TCPTuple{},
				})
					if err != nil {
						b.Fatal(err)
					}
					i++
				}
			})
		})
	}
}

// BenchmarkFrameReassembly 帧重组性能基准测试
func BenchmarkFrameReassembly(b *testing.B) {
	fragmentCounts := []int{2, 5, 10, 20}

	for _, count := range fragmentCounts {
		b.Run(fmt.Sprintf("Fragments_%d", count), func(b *testing.B) {
			headersFrame := createFragmentedHeadersFramePerf(nil, 1, false, false)
			continuationFrames := createMultipleContinuationFrames(nil, 1, count-1)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				// 每次迭代使用新的解析器实例
				parser := parser.NewHTTP2Parser()
				connID := fmt.Sprintf("bench-reassembly-%d", i)

				// 解析HEADERS帧
				_, err := parser.ParseRequest(connID, headersFrame, &types.PacketInfo{
				Data:      headersFrame,
				Direction: types.DirectionRequest,
				TCPTuple:  &types.TCPTuple{},
			})
				if err != nil {
					b.Fatal(err)
				}

				// 解析CONTINUATION帧
				for _, contFrame := range continuationFrames {
					_, err := parser.ParseRequest(connID, contFrame, &types.PacketInfo{
					Data:      contFrame,
					Direction: types.DirectionRequest,
					TCPTuple:  &types.TCPTuple{},
				})
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// 辅助函数：创建分片的HEADERS帧（性能测试版本）
func createFragmentedHeadersFramePerf(t *testing.T, streamID uint32, endStream, endHeaders bool) []byte {
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 只编码部分头部
	encoder.WriteField(hpack.HeaderField{Name: ":method", Value: "GET"})
	encoder.WriteField(hpack.HeaderField{Name: ":path", Value: "/fragmented"})

	headerBlock := buf.Bytes()
	frame := make([]byte, 9+len(headerBlock))

	// 帧头
	frame[0] = byte(len(headerBlock) >> 16)
	frame[1] = byte(len(headerBlock) >> 8)
	frame[2] = byte(len(headerBlock))
	frame[3] = 0x01 // HEADERS

	// 标志（不设置END_HEADERS）
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

// 辅助函数：创建多个CONTINUATION帧
func createMultipleContinuationFrames(t *testing.T, streamID uint32, count int) [][]byte {
	frames := make([][]byte, count)

	for i := 0; i < count; i++ {
		var buf bytes.Buffer
		encoder := hpack.NewEncoder(&buf)

		// 每个CONTINUATION帧包含一些头部
		encoder.WriteField(hpack.HeaderField{
			Name:  fmt.Sprintf("x-continuation-%d", i),
			Value: fmt.Sprintf("value-%d", i),
		})

		headerBlock := buf.Bytes()
		frame := make([]byte, 9+len(headerBlock))

		// 帧头
		frame[0] = byte(len(headerBlock) >> 16)
		frame[1] = byte(len(headerBlock) >> 8)
		frame[2] = byte(len(headerBlock))
		frame[3] = 0x09 // CONTINUATION

		// 标志（只有最后一个设置END_HEADERS）
		flags := byte(0)
		if i == count-1 {
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

		frames[i] = frame
	}

	return frames
}

// 辅助函数：创建DATA帧片段
func createDataFragments(t *testing.T, streamID uint32, count, fragmentSize int) [][]byte {
	frames := make([][]byte, count)

	for i := 0; i < count; i++ {
		// 创建随机数据
		data := make([]byte, fragmentSize)
		rand.Read(data)

		frame := make([]byte, 9+fragmentSize)

		// 帧头
		frame[0] = byte(fragmentSize >> 16)
		frame[1] = byte(fragmentSize >> 8)
		frame[2] = byte(fragmentSize)
		frame[3] = 0x00 // DATA

		// 标志（最后一个片段会在测试中设置END_STREAM）
		frame[4] = 0x00

		// 流ID
		frame[5] = byte(streamID >> 24)
		frame[6] = byte(streamID >> 16)
		frame[7] = byte(streamID >> 8)
		frame[8] = byte(streamID)

		// 数据
		copy(frame[9:], data)

		frames[i] = frame
	}

	return frames
}

// 辅助函数：创建并发测试帧
func createConcurrentTestFrame(t *testing.T, streamID uint32, endStream, endHeaders bool, goroutineID, index int) []byte {
	var buf bytes.Buffer
	encoder := hpack.NewEncoder(&buf)

	// 标准头部
	encoder.WriteField(hpack.HeaderField{Name: ":method", Value: "GET"})
	encoder.WriteField(hpack.HeaderField{Name: ":path", Value: fmt.Sprintf("/concurrent/%d/%d", goroutineID, index)})
	encoder.WriteField(hpack.HeaderField{Name: ":scheme", Value: "https"})
	encoder.WriteField(hpack.HeaderField{Name: ":authority", Value: "test.example.com"})

	// 添加标识头部
	encoder.WriteField(hpack.HeaderField{Name: "x-goroutine-id", Value: fmt.Sprintf("%d", goroutineID)})
	encoder.WriteField(hpack.HeaderField{Name: "x-request-index", Value: fmt.Sprintf("%d", index)})
	encoder.WriteField(hpack.HeaderField{Name: "x-stream-id", Value: fmt.Sprintf("%d", streamID)})

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
