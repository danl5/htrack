# HTrack - HTTP通讯生命周期管理器

## 项目简介

HTrack是一个专门用于管理HTTP通讯生命周期的Go语言项目，主要功能包括：

- **协议解析**: 自动解析HTTP数据包的明文内容
- **版本检测**: 智能判断HTTP版本（HTTP/1.x 和 HTTP/2）
- **状态机管理**: 完整的HTTP通讯状态跟踪
- **元数据管理**: 支持HTTP/2流管理等高级特性
- **数据包重组**: 处理分片和乱序的数据包
- **完整输出**: 提供完整的Request和Response内容

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

适用于需要深度分析HTTP通讯的场景：
- 网络监控和分析
- API调试和测试
- 性能分析
- 安全审计

## 快速开始

```go
package main

import (
    "fmt"
    "htrack"
)

func main() {
    tracker := htrack.NewTracker()
    
    // 处理HTTP数据包
    result, err := tracker.ProcessPacket(data)
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("Request: %s\n", result.Request)
    fmt.Printf("Response: %s\n", result.Response)
}
```

## 项目结构

```
htrack/
├── README.md
├── go.mod
├── tracker.go          # 主要跟踪器
├── parser/             # 协议解析器
│   ├── http1.go       # HTTP/1.x解析
│   ├── http2.go       # HTTP/2解析
│   └── common.go      # 通用解析逻辑
├── state/             # 状态机管理
│   ├── machine.go     # 状态机核心
│   └── states.go      # 状态定义
├── stream/            # 流管理（HTTP/2）
│   ├── manager.go     # 流管理器
│   └── stream.go      # 单个流处理
└── types/             # 类型定义
    ├── request.go     # 请求类型
    ├── response.go    # 响应类型
    └── metadata.go    # 元数据类型
```