# RDP插件 - 基于Guacamole协议实现

## 概述

本RDP插件基于Apache Guacamole协议实现，参考了mayfly-go项目的架构设计，提供了完整的Windows远程桌面连接管理功能。

## 核心架构

### 1. 主要组件

```
RDPPlugin (主插件)
├── GuacdManager (Guacamole守护进程管理)
├── TunnelManager (隧道管理)
├── SessionStore (会话存储)
├── ConnectionPool (连接池)
└── MetricsCollector (指标收集)
```

### 2. 文件结构

```
agent/plugins/rdp/
├── rdp_plugin.go           # 主插件实现
├── rdp_config.go           # 配置管理和GuacdManager
├── tunnel_manager.go       # 隧道管理器
├── session_store.go        # 会话存储
├── guacamole_stream.go     # Guacamole数据流
├── guacamole_connection.go # Guacamole连接实现
└── README.md              # 本文档
```

## 核心特性

### 1. Guacamole协议支持
- 完整的Guacamole握手协议实现
- 指令解析和构建
- 数据流管理
- 连接状态跟踪

### 2. 连接管理
- 连接池管理
- 自动重连机制
- 健康检查
- 资源清理

### 3. 会话管理
- WebSocket会话支持
- 会话超时管理
- 自动清理过期会话

### 4. 隧道管理
- 简单隧道实现
- WebSocket隧道支持
- 数据转发机制
- 活动时间跟踪

## 配置说明

### 默认配置

```go
type RDPPluginConfig struct {
    MaxConnections    int              // 最大连接数: 20
    ConnectionTimeout time.Duration    // 连接超时: 60秒
    Guacd             GuacdConfig      // Guacamole配置
    Connection        ConnectionConfig // 连接配置
}

type GuacdConfig struct {
    Address             string        // guacd地址: localhost:4822
    Fallbacks           []string      // 备用地址
    ConnectTimeout      time.Duration // 连接超时: 10秒
    HealthCheckInterval time.Duration // 健康检查间隔: 30秒
}

type ConnectionConfig struct {
    DefaultWidth  int // 默认宽度: 1920
    DefaultHeight int // 默认高度: 1080
    ColorDepth    int // 颜色深度: 32
    DPI           int // DPI: 96
}
```

## 使用方法

### 1. 创建插件实例

```go
plugin := NewRDPPlugin()
```

### 2. 初始化插件

```go
config := map[string]interface{}{
    // 配置参数
}
err := plugin.Initialize(config)
```

### 3. 启动插件

```go
err := plugin.Start()
```

### 4. 创建RDP连接

```go
credentials := &common.Credentials{
    Username: "user",
    Password: "password",
    AuthType: "password",
}

conn, err := plugin.CreateConnection(ctx, "192.168.1.100:3389", credentials)
```

## Guacamole协议实现

### 1. 握手流程

1. **选择协议**: 发送`select`指令选择RDP协议
2. **获取参数**: 接收`args`指令获取所需参数列表
3. **建立连接**: 发送`connect`指令和连接参数
4. **确认连接**: 接收`ready`指令确认连接建立

### 2. 指令格式

Guacamole指令采用`LENGTH.VALUE`格式：
```
LENGTH.VALUE,LENGTH.VALUE,...;
```

例如：`4.size,1.0,4.1024,3.768;`

### 3. 性能优化参数

```go
parameters := map[string]string{
    "enable-wallpaper":           "true",
    "resize-method":              "display-update", 
    "enable-font-smoothing":      "true",
    "enable-desktop-composition": "false",
    "enable-menu-animations":     "false",
    "disable-bitmap-caching":     "true",
    "disable-offscreen-caching":  "true",
    "force-lossless":             "true",
    "color-depth":                "32",
    "ignore-cert":                "true",
}
```

## 监控和指标

### 1. 插件状态
- 运行状态
- 连接数量
- 启动/停止时间
- 配置信息

### 2. 连接指标
- 总连接数
- 活跃连接数
- 失败连接数
- 平均连接时间

### 3. 会话指标
- 总会话数
- 活跃会话数
- 平均会话时长

## 错误处理

### 1. 连接错误
- 网络连接失败
- 认证失败
- 超时错误
- 协议错误

### 2. 自动恢复
- 连接重试机制
- 故障转移支持
- 资源清理

## 扩展功能

### 1. WebSocket桥接
- 支持WebSocket客户端连接
- 数据格式转换
- 实时数据转发

### 2. 文件传输
- 支持文件上传下载
- 拖拽文件传输
- 进度监控

### 3. 剪贴板共享
- 文本剪贴板同步
- 格式化内容支持

## 依赖要求

### 1. 外部依赖
- Apache Guacamole守护进程 (guacd)
- Go 1.19+
- go-zero框架

### 2. 网络要求
- 到目标RDP服务器的网络连接
- 到guacd服务的网络连接
- WebSocket支持（可选）

## 部署说明

### 1. 安装guacd

```bash
# Ubuntu/Debian
sudo apt-get install guacd

# CentOS/RHEL
sudo yum install guacd

# Docker
docker run -d -p 4822:4822 guacamole/guacd
```

### 2. 配置guacd

```bash
# 启动guacd服务
sudo systemctl start guacd
sudo systemctl enable guacd

# 检查服务状态
sudo systemctl status guacd
```

### 3. 插件配置

在agent配置文件中添加RDP插件配置：

```yaml
plugins:
  rdp:
    max_connections: 20
    connection_timeout: 60s
    guacd:
      address: "localhost:4822"
      fallbacks: ["127.0.0.1:4822"]
      connect_timeout: 10s
      health_check_interval: 30s
    connection:
      default_width: 1920
      default_height: 1080
      color_depth: 32
      dpi: 96
```

## 故障排除

### 1. 常见问题

**连接失败**
- 检查guacd服务状态
- 验证网络连接
- 检查防火墙设置

**认证失败**
- 验证用户名密码
- 检查RDP服务配置
- 确认用户权限

**性能问题**
- 调整屏幕分辨率
- 优化网络带宽
- 调整压缩设置

### 2. 日志分析

插件提供详细的日志输出，包括：
- 连接建立过程
- 协议握手详情
- 错误信息和堆栈
- 性能指标

## 开发指南

### 1. 扩展插件

要扩展RDP插件功能，可以：
- 实现新的隧道类型
- 添加自定义指标收集
- 扩展配置选项
- 实现新的认证方式

### 2. 测试

```bash
# 运行单元测试
go test ./...

# 运行集成测试
go test -tags=integration ./...

# 性能测试
go test -bench=. ./...
```

## 参考资料

- [Apache Guacamole文档](https://guacamole.apache.org/doc/gug/)
- [Guacamole协议规范](https://guacamole.apache.org/doc/gug/protocol-reference.html)
- [mayfly-go项目](https://github.com/dromara/mayfly-go)
- [RDP协议文档](https://docs.microsoft.com/en-us/openspecs/windows_protocols/ms-rdp/) 