# Agent OPS集成说明

本文档说明如何在Agent中集成和使用OPS流式客户端和任务处理器。

## 核心组件

### 1. OpsTaskHandler (`internal/handlers/ops_task_handler.go`)

实现了 `TaskHandler` 接口的OPS任务处理器，用于处理从OPS服务接收到的各种任务。

**主要功能：**
- 远程命令执行 (`remote_execution`)
- 文件传输 (`file_transfer`)
- 健康检查 (`health_check`)
- 配置更新 (`config_update`)

**接口定义：**
```go
type TaskHandler interface {
    HandleTask(taskData map[string]interface{}) error
}
```

### 2. OpsCenter HTTP/PSK（推荐）

已弃用 gRPC OpsRpc/OpsStreamClient，统一通过 OpsCenter HTTP/PSK 完成注册、心跳与任务结果上报：

- 注册与心跳：internal/svc/proxy_registration_manager.go 调用 internal/client/ops_center_client.go
- 任务结果上报：EnhancedTaskExecutor 失败时落 Outbox，重放亦通过 OpsCenterClient.ReportTaskResult

### 3. FileTransferRequest (`internal/types/executor.go`)

新增的文件传输请求类型，支持SSH/SFTP文件传输操作。

## 使用方式

### 1. 基本集成

```go
import (
    "github.com/coder-lulu/newbee-proxy/internal/client"
    "github.com/coder-lulu/newbee-proxy/internal/config"
    "github.com/coder-lulu/newbee-proxy/internal/handlers"
    "github.com/coder-lulu/newbee-proxy/internal/svc"
)

// 创建服务上下文
svcCtx := svc.NewServiceContext(config)

// 创建OPS任务处理器
opsTaskHandler := handlers.NewOpsTaskHandler(svcCtx)

// 创建OPS流式客户端
opsStreamClient := client.NewOpsStreamClient(&config, opsTaskHandler)

// 启动客户端
err := opsStreamClient.Start()
```

### 3. 配置设置

在 `etc/proxy.yaml` 中配置 OpsCenter 与 PSK，建议将 Security.SkipPaths 仅包含只读探活：

```yaml
OpsCenter:
  Enabled: true
  Endpoints: ["http://127.0.0.1:9601"]
  HeartbeatSeconds: 30
  WorkerID: "proxy-worker-001"
  PSK: "dev-psk"

Security:
  JWT: { Enabled: false }
  SkipPaths: ["/health", "/status", "/metrics"]
```

### 3. 任务类型示例

#### 远程执行任务
```json
{
  "task_id": "cmd_001",
  "task_type": "remote_execution",
  "target": {
    "host": "192.168.1.100",
    "port": 22,
    "protocol": "ssh",
    "credentials": {
      "username": "admin",
      "password": "password123"
    }
  },
  "command": {
    "content": "ls -la /tmp",
    "timeout": 30
  },
  "options": {
    "capture_output": true
  }
}
```

#### 文件传输任务
```json
{
  "task_id": "transfer_001",
  "task_type": "file_transfer",
  "target": {
    "host": "192.168.1.100",
    "port": 22,
    "protocol": "ssh",
    "credentials": {
      "username": "admin",
      "password": "password123"
    }
  },
  "command": {
    "src_path": "/local/file.txt",
    "dst_path": "/remote/file.txt",
    "direction": "upload",
    "timeout": 120
  }
}
```

#### 健康检查任务
```json
{
  "task_id": "health_001",
  "task_type": "health_check"
}
```

#### 配置更新任务
```json
{
  "task_id": "config_001",
  "task_type": "config_update",
  "config_updates": {
    "log_level": "debug",
    "max_connections": 100
  }
}
```

## 运行示例

### 完整示例程序

参考 `examples/ops_integration/usage_demo.go` 文件，展示了完整的集成流程。

**运行方式：**
```bash
cd agent/examples/ops_integration
go run usage_demo.go
```

### 在现有Agent中集成

如果要在现有的Agent服务中集成OPS功能：

1. **修改服务上下文** (`internal/svc/service_context.go`)：
```go
type ServiceContext struct {
    // ... 现有字段
    
    // 新增OPS组件
    OpsStreamClient *client.OpsStreamClient
    OpsTaskHandler  *handlers.OpsTaskHandler
}

func NewServiceContext(c config.Config) *ServiceContext {
    // ... 现有初始化代码
    
    // 初始化OPS组件
    svcCtx.OpsTaskHandler = handlers.NewOpsTaskHandler(svcCtx)
    svcCtx.OpsStreamClient = client.NewOpsStreamClient(&c, svcCtx.OpsTaskHandler)
    
    return svcCtx
}

func (svc *ServiceContext) Start() error {
    // ... 现有启动代码
    
    // 启动OPS客户端
    if svc.Config.OpsRpc.Enabled {
        if err := svc.OpsStreamClient.Start(); err != nil {
            return fmt.Errorf("failed to start OPS stream client: %w", err)
        }
    }
    
    return nil
}
```

2. **更新主程序** (`cmd/agent/main.go`)：
无需修改，因为OPS集成已在服务上下文中处理。

## 扩展开发

### 添加新的任务类型

1. **在OpsTaskHandler中添加新的处理逻辑：**
```go
func (h *OpsTaskHandler) HandleTask(taskData map[string]interface{}) error {
    switch taskType {
    case "remote_execution":
        return h.handleRemoteExecution(taskID, taskData)
    case "file_transfer":
        return h.handleFileTransfer(taskID, taskData)
    case "new_task_type":  // 新增任务类型
        return h.handleNewTaskType(taskID, taskData)
    // ...
    }
}

func (h *OpsTaskHandler) handleNewTaskType(taskID string, taskData map[string]interface{}) error {
    // 实现新任务的处理逻辑
    return nil
}
```

2. **在任务管理器中添加任务创建方法：**
```go
func (tm *TaskManager) CreateNewTaskType(taskID string, params...string) *TaskAssignment {
    return &TaskAssignment{
        TaskID:   taskID,
        TaskType: "new_task_type",
        // 填充其他字段
    }
}
```

### 自定义TaskHandler

如果需要完全自定义的任务处理逻辑：

```go
type CustomTaskHandler struct {
    logger logx.Logger
    // 自定义字段
}

func (h *CustomTaskHandler) HandleTask(taskData map[string]interface{}) error {
    // 自定义任务处理逻辑
    return nil
}

// 使用自定义处理器
customHandler := &CustomTaskHandler{logger: logger}
opsStreamClient := client.NewOpsStreamClient(&config, customHandler)
```

## 故障排除

### 常见问题

1. **连接失败**
   - 检查OPS服务是否运行
   - 验证网络连接和端口
   - 检查TLS配置

2. **任务执行失败**
   - 查看Agent日志
   - 验证目标主机连接性
   - 检查认证信息

3. **编译错误**
   - 确保所有依赖已安装
   - 检查导入路径
   - 验证Go模块配置

### 调试技巧

1. **启用详细日志**：
```yaml
Log:
  Level: debug
```

2. **监控连接状态**：
```go
if opsStreamClient.IsConnected() {
    log.Println("Connected to OPS")
} else {
    log.Println("Not connected to OPS")
}
```

3. **任务执行追踪**：
查看Agent的任务结果目录 `task_results/` 获取详细执行信息。

## 总结

OPS集成为Agent提供了强大的远程任务执行能力，支持实时任务分发、双向通信和多种协议操作。通过模块化设计，可以轻松扩展新的任务类型和功能。 
