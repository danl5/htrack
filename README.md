# HTrack - HTTP协议数据解析器

## 项目简介

HTrack是一个专门用于解析HTTP协议数据的Go语言库，主要功能包括：

- **协议解析**: 将原始HTTP数据包解析为结构化的请求和响应对象
- **版本检测**: 智能识别HTTP版本（HTTP/1.x 和 HTTP/2）
- **数据重组**: 处理分片、乱序和不完整的HTTP数据包
- **流式处理**: 支持HTTP/2多路复用流的解析和管理
- **事件驱动**: 提供丰富的回调接口和Channel输出
- **会话管理**: 通过会话ID关联同一连接的请求响应对

## 核心特性

### 支持的HTTP版本
- HTTP/1.0
- HTTP/1.1
- HTTP/2

### 数据包处理能力
- 数据包重组
- 乱序处理
- 流式解析
- 状态机驱动

### 输出格式
- 完整的HTTP请求内容
- 完整的HTTP响应内容
- 通讯元数据信息
- 流管理状态（HTTP/2）

## 使用场景

适用于需要解析和分析HTTP协议数据的场景：
- 网络流量分析和监控
- HTTP协议调试工具
- API请求响应分析
- 网络安全审计
- 数据包捕获分析
- 性能监控和诊断

## 快速开始

### 基本用法

```go
package main

import (
    "fmt"
    "github.com/danl5/htrack"
    "github.com/danl5/htrack/types"
)

func main() {
    // 创建解析器实例
    parser := htrack.New(nil)
    defer parser.Close()
    
    // 设置事件处理器
    parser.SetEventHandlers(&htrack.EventHandlers{
        OnRequestParsed: func(request *types.HTTPRequest) {
            fmt.Printf("解析到请求: %s %s\n", request.Method, request.URL)
        },
        OnResponseParsed: func(response *types.HTTPResponse) {
            fmt.Printf("解析到响应: %d %s\n", response.StatusCode, response.Status)
        },
    })
    
    // 处理HTTP数据包
    err := parser.ProcessPacket("session-1", httpData, types.DirectionRequest)
    if err != nil {
        panic(err)
    }
}
```

### 便捷函数

```go
// 解析单个HTTP消息
request, response, err := htrack.ParseHTTPMessage(data)
if err != nil {
    panic(err)
}
```

## 项目结构

```
htrack/
├── README.md
├── go.mod
├── htrack.go           # 主要解析器接口
├── connection/         # 会话管理
│   └── manager.go     # 会话管理器
├── parser/             # 协议解析器
│   ├── http1.go       # HTTP/1.x解析
│   ├── http2.go       # HTTP/2解析
│   ├── common.go      # 通用解析逻辑
│   └── http2/         # HTTP/2相关
│       ├── frame.go   # 帧处理
│       ├── settings.go # 设置管理
│       └── errors.go  # 错误定义
├── state/             # 状态机管理
│   ├── machine.go     # 状态机核心
│   └── states.go      # 状态定义
├── stream/            # 流管理（HTTP/2）
│   ├── manager.go     # 流管理器
│   └── stream.go      # 单个流处理
├── types/             # 类型定义
│   ├── request.go     # 请求类型
│   ├── response.go    # 响应类型
│   ├── metadata.go    # 元数据类型
│   └── utils.go       # 工具函数
├── examples/          # 使用示例
│   ├── basic/         # 基础示例
│   ├── advanced/      # 高级示例
│   └── channel/       # Channel使用示例
└── tests/             # 测试文件
    ├── http2_frame_test.go
    ├── http2_performance_test.go
    └── http2_stream_test.go
```

## 核心概念

### 会话（Session）
会话是一个逻辑概念，用于关联同一连接中的请求和响应。通过会话ID，HTrack可以正确地将请求与对应的响应进行匹配。

### 事务（Transaction）
事务代表一个完整的HTTP请求-响应对。每个事务都有唯一的ID，并包含完整的请求和响应信息。

### 流（Stream）
在HTTP/2中，流是多路复用的基本单位。HTrack能够正确处理HTTP/2的流管理和帧重组。