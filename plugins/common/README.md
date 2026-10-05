# 通用插件框架

本目录包含了所有插件通用的基础框架，用于减少代码重复，提高开发效率和系统一致性。

## 目录结构

```
plugins/common/
├── timeout/           # 超时控制框架
│   └── controller.go
├── websocket/         # WebSocket管理框架
│   ├── manager.go
│   └── connection.go
├── connection/        # 连接管理框架
│   └── manager.go
├── metrics/           # 监控指标框架
│   └── collector.go
└── README.md
```

## 框架介绍

### 1. 超时控制框架 (`timeout`)

统一管理各种操作的超时时间，提供标准化的超时配置和Context创建。

**主要特性：**
- 统一的超时配置管理
- 自动标准化超时时间（最小值、最大值限制）
- 类型化的超时控制（连接、读取、写入、查询等）
- Context创建和管理
- WebSocket特定超时支持

**使用示例：**
```go
import "github.com/coder-lulu/newbee-proxy/plugins/common/timeout"

// 创建超时控制器
config := timeout.DefaultTimeoutConfig()
ctrl := timeout.NewTimeoutController(config)

// 创建带超时的Context
ctx, cancel := ctrl.CreateTimeoutContext(context.Background(), 30*time.Second)
defer cancel()

// 根据类型创建超时Context
ctx, cancel = ctrl.CreateTypedTimeoutContext(context.Background(), timeout.TimeoutTypeQuery)
defer cancel()
```

### 2. WebSocket管理框架 (`websocket`)

提供完整的WebSocket连接管理功能，包括连接生命周期、消息处理、心跳检测等。

**主要特性：**
- 连接生命周期管理（创建、维护、清理）
- 自动心跳检测和超时处理
- 消息队列和异步处理
- 连接统计和监控
- 可插拔的事件处理器
- 自动清理过期连接

**使用示例：**
```go
import "github.com/coder-lulu/newbee-proxy/plugins/common/websocket"

// 创建WebSocket管理器
wsConfig := websocket.DefaultWebSocketConfig()
manager := websocket.NewWebSocketManager(wsConfig, timeoutConfig)

// 设置消息处理器
manager.SetMessageHandler(websocket.MessageHandlerFunc(func(conn *websocket.WebSocketConnection, msgType int, data []byte) error {
    // 处理收到的消息
    return nil
}))

// 升级HTTP连接为WebSocket
conn, err := manager.UpgradeConnection(w, r, sessionID, metadata)
if err != nil {
    return err
}

// 发送消息
conn.SendJSON(map[string]interface{}{
    "type": "response",
    "data": "Hello WebSocket",
})
```

### 3. 连接管理框架 (`connection`)

通用的连接生命周期管理，支持各种类型的连接（数据库、网络等）。

**主要特性：**
- 连接池管理和资源限制
- 自动健康检查和故障恢复
- 连接超时和自动清理
- 重试机制和退避算法
- 连接统计和监控
- 事件驱动的生命周期管理

**使用示例：**
```go
import "github.com/coder-lulu/newbee-proxy/plugins/common/connection"

// 创建连接管理器
config := connection.DefaultManagerConfig()
manager := connection.NewConnectionManager(config, timeoutConfig, logger)

// 添加连接
err := manager.AddConnection("db-1", "mysql", "192.168.1.100:3306", dbConn, metadata, healthChecker)

// 获取连接
conn, exists := manager.GetConnection("db-1")
if exists {
    // 使用连接
    dbConn := conn.Connection.(*sql.DB)
}

// 设置事件处理器
manager.SetEventHandlers(
    func(conn *connection.ManagedConnection) {
        // 连接建立事件
    },
    func(conn *connection.ManagedConnection) {
        // 连接断开事件
    },
    func(conn *connection.ManagedConnection, err error) {
        // 错误事件
    },
)
```

### 4. 监控指标框架 (`metrics`)

统一的指标收集和统计框架，用于监控系统性能和健康状态。

**主要特性：**
- 连接指标（活跃连接、空闲连接、失败连接等）
- 请求指标（成功率、响应时间、吞吐量等）
- 错误指标（错误类型、错误率、最近错误等）
- 性能指标（CPU、内存、网络IO等）
- 实时统计和历史数据

**使用示例：**
```go
import "github.com/coder-lulu/newbee-proxy/plugins/common/metrics"

// 创建指标收集器
collector := metrics.NewMetricsCollector()

// 记录连接事件
collector.RecordConnectionStart("websocket")
// ... 一段时间后
collector.RecordConnectionEnd("websocket", duration, true)

// 记录请求
collector.RecordRequest("query", responseTime, success)

// 记录错误
collector.RecordError("sql_error", "Connection timeout", "db_plugin", "warning")

// 获取指标
allMetrics := collector.GetAllMetrics()
connectionMetrics := collector.GetConnectionMetrics()
```

## 集成指南

### 在插件中使用框架

1. **导入框架包**：
```go
import (
    "github.com/coder-lulu/newbee-proxy/plugins/common/timeout"
    "github.com/coder-lulu/newbee-proxy/plugins/common/websocket"
    "github.com/coder-lulu/newbee-proxy/plugins/common/connection"
    "github.com/coder-lulu/newbee-proxy/plugins/common/metrics"
)
```

2. **创建管理器实例**：
```go
type MyPlugin struct {
    timeoutCtrl *timeout.TimeoutController
    wsManager   *websocket.WebSocketManager
    connManager *connection.ConnectionManager
    metrics     *metrics.MetricsCollector
}

func NewMyPlugin() *MyPlugin {
    timeoutConfig := timeout.DefaultTimeoutConfig()
    wsConfig := websocket.DefaultWebSocketConfig()
    connConfig := connection.DefaultManagerConfig()
    
    return &MyPlugin{
        timeoutCtrl: timeout.NewTimeoutController(timeoutConfig),
        wsManager:   websocket.NewWebSocketManager(wsConfig, timeoutConfig),
        connManager: connection.NewConnectionManager(connConfig, timeoutConfig, logger),
        metrics:     metrics.NewMetricsCollector(),
    }
}
```

3. **实现插件接口**：
```go
func (p *MyPlugin) Start() error {
    // 使用框架启动插件
    return nil
}

func (p *MyPlugin) Stop() error {
    // 关闭所有管理器
    p.wsManager.Shutdown()
    p.connManager.Shutdown()
    return nil
}
```

### 替换现有代码

对于现有插件，可以逐步替换重复的代码：

1. **识别重复模式**：
   - WebSocket连接管理 → 使用 `websocket.WebSocketManager`
   - 数据库连接池 → 使用 `connection.ConnectionManager`
   - 超时处理 → 使用 `timeout.TimeoutController`
   - 统计指标 → 使用 `metrics.MetricsCollector`

2. **渐进式迁移**：
   - 先迁移新功能
   - 逐步替换现有功能
   - 保持向后兼容性

3. **测试验证**：
   - 单元测试框架功能
   - 集成测试插件功能
   - 性能测试确保无回归

## 最佳实践

### 1. 配置管理
- 使用默认配置作为起点
- 根据插件需求调整配置
- 提供配置验证机制

### 2. 错误处理
- 统一错误类型和格式
- 使用框架的错误记录功能
- 实现优雅的降级策略

### 3. 日志记录
- 使用统一的日志接口
- 记录关键操作和状态变化
- 避免过度日志记录

### 4. 性能优化
- 合理设置缓冲区大小
- 优化连接池配置
- 监控关键性能指标

### 5. 测试策略
- 单元测试各框架组件
- 集成测试插件功能
- 性能测试和压力测试
- 故障注入测试

## 注意事项

1. **依赖管理**：确保所有依赖包正确导入
2. **版本兼容**：框架更新时保持向后兼容
3. **资源清理**：正确关闭所有管理器和连接
4. **并发安全**：所有框架都是线程安全的
5. **内存管理**：避免内存泄漏，合理设置限制

## 贡献指南

如果需要扩展或修改框架：

1. 保持接口向后兼容
2. 添加充分的测试覆盖
3. 更新相关文档
4. 考虑对现有插件的影响

## 待办事项

- [ ] 添加分布式追踪支持
- [ ] 实现配置热重载
- [ ] 添加更多监控指标
- [ ] 优化内存使用
- [ ] 添加更多示例代码 