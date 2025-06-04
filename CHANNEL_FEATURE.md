# HTrack Channel 输出功能

## 概述

HTrack 现在支持通过 Go channel 的方式输出解析完成的 HTTP 请求和响应。当请求或响应完全解析完毕后，会自动发送到对应的 channel 中，方便用户进行实时处理。

## 功能特性

- ✅ **异步输出**: 请求和响应解析完成后立即通过 channel 输出
- ✅ **非阻塞设计**: Channel 满时自动丢弃数据，不会阻塞解析流程
- ✅ **可配置缓冲区**: 支持自定义 channel 缓冲区大小
- ✅ **可选启用**: 默认禁用，需要显式启用
- ✅ **自动清理**: HTrack 关闭时自动关闭 channels
- ✅ **线程安全**: 支持多 goroutine 并发读取

## 配置选项

在 `Config` 结构体中新增了以下配置项：

```go
type Config struct {
    // ... 其他配置项
    
    // Channel配置
    ChannelBufferSize int  `json:"channel_buffer_size"` // Channel缓冲区大小
    EnableChannels    bool `json:"enable_channels"`     // 是否启用Channel输出
}
```

### 配置说明

- `EnableChannels`: 是否启用 channel 输出功能（默认: `false`）
- `ChannelBufferSize`: Channel 缓冲区大小（默认: `100`）

## 使用方法

### 1. 启用 Channel 功能

```go
config := &htrack.Config{
    // ... 其他配置
    EnableChannels:    true,  // 启用channel输出
    ChannelBufferSize: 200,   // 设置缓冲区大小
}

ht := htrack.New(config)
defer ht.Close()
```

### 2. 监听请求 Channel

```go
go func() {
    for request := range ht.GetRequestChan() {
        fmt.Printf("收到请求: %s %s\n", request.Method, request.URL.String())
        fmt.Printf("请求头: %+v\n", request.Headers)
        fmt.Printf("请求体大小: %d bytes\n", len(request.Body))
        
        // 处理请求...
    }
}()
```

### 3. 监听响应 Channel

```go
go func() {
    for response := range ht.GetResponseChan() {
        fmt.Printf("收到响应: %d %s\n", response.StatusCode, response.Status)
        fmt.Printf("响应头: %+v\n", response.Headers)
        fmt.Printf("响应体大小: %d bytes\n", len(response.Body))
        
        // 处理响应...
    }
}()
```

### 4. 处理数据包

```go
// 正常处理数据包，解析完成的请求/响应会自动发送到channel
err := ht.ProcessPacket("conn-1", requestData, types.DirectionRequest)
if err != nil {
    log.Printf("处理请求失败: %v", err)
}

err = ht.ProcessPacket("conn-1", responseData, types.DirectionResponse)
if err != nil {
    log.Printf("处理响应失败: %v", err)
}
```

## API 参考

### 新增方法

#### `GetRequestChan() <-chan *types.HTTPRequest`
获取请求 channel（只读）。当 `EnableChannels` 为 `false` 时返回 `nil`。

#### `GetResponseChan() <-chan *types.HTTPResponse`
获取响应 channel（只读）。当 `EnableChannels` 为 `false` 时返回 `nil`。

#### `CloseChannels()`
手动关闭所有 channels。通常不需要手动调用，`Close()` 方法会自动调用。

### 数据结构

发送到 channel 的请求和响应对象包含完整的解析信息：

```go
// HTTPRequest 包含的主要字段
type HTTPRequest struct {
    Method        string      // HTTP方法
    URL           *url.URL    // 请求URL
    Headers       http.Header // 请求头
    Body          []byte      // 请求体
    Timestamp     time.Time   // 解析时间戳
    StreamID      *uint32     // HTTP/2流ID（如果适用）
    Complete      bool        // 是否解析完成（channel中的都是true）
    // ... 其他字段
}

// HTTPResponse 包含的主要字段
type HTTPResponse struct {
    StatusCode    int         // 状态码
    Status        string      // 状态文本
    Headers       http.Header // 响应头
    Body          []byte      // 响应体
    Timestamp     time.Time   // 解析时间戳
    StreamID      *uint32     // HTTP/2流ID（如果适用）
    Complete      bool        // 是否解析完成（channel中的都是true）
    // ... 其他字段
}
```

## 注意事项

### 1. 缓冲区溢出处理

当 channel 缓冲区满时，新的数据会被丢弃（非阻塞模式），不会影响解析性能。建议根据实际处理能力设置合适的缓冲区大小。

### 2. 内存管理

- Channel 中的对象包含完整的请求/响应数据，注意内存使用
- 及时处理 channel 中的数据，避免积压
- 程序退出前确保调用 `Close()` 方法

### 3. 并发安全

- 多个 goroutine 可以安全地从同一个 channel 读取数据
- 每个数据只会被一个 goroutine 接收到
- 建议使用单独的 goroutine 处理 channel 数据

### 4. 性能考虑

- Channel 输出是可选功能，不启用时不会有性能影响
- 启用时会有轻微的内存和 CPU 开销
- 缓冲区大小影响内存使用，建议根据实际需求调整

## 示例代码

完整的使用示例请参考：
- `examples/channel/main.go` - 基本使用示例
- `tests/channel_test.go` - 功能测试示例

## 兼容性

- ✅ 向后兼容：现有代码无需修改
- ✅ 支持 HTTP/1.1 和 HTTP/2
- ✅ 支持所有现有的事件处理器
- ✅ 可与现有的回调函数同时使用