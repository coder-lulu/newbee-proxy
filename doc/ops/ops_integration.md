# NewBee Agent 与 OPS 服务集成文档

> **Version**: 1.0.0  
> **Last Update**: 2024-12-19  
> **Author**: @newbee-team  

## 概述

本文档详细描述了 NewBee Agent 与 OPS (Operations Service) 服务的集成方案，包括通信协议、数据格式、部署配置和故障处理等内容。

## 集成架构

### 整体架构图
```
┌─────────────────────────────────────────────────────────────────┐
│                        OPS Service                             │
├─────────────────────────────────────────────────────────────────┤
│  Agent Manager  │  Task Scheduler  │  Resource Monitor        │
├─────────────────────────────────────────────────────────────────┤
│                    gRPC/HTTP API                               │
└─────────────────┬───────────────────────────────────────────────┘
                  │
                  │ Bidirectional Communication
                  │
┌─────────────────┴───────────────────────────────────────────────┐
│                    NewBee Agent                                │
├─────────────────────────────────────────────────────────────────┤
│  OPS Client     │  Task Executor   │  Status Reporter         │
├─────────────────────────────────────────────────────────────────┤
│     SSH Plugin  │    RDP Plugin    │   Telnet Plugin          │
└─────────────────────────────────────────────────────────────────┘
```

### 通信模式
1. **Agent 注册**: Agent 启动时向 OPS 注册
2. **心跳保持**: 定期发送心跳维持连接
3. **任务分发**: OPS 向 Agent 分发执行任务
4. **状态上报**: Agent 向 OPS 上报执行状态
5. **结果回传**: Agent 向 OPS 回传执行结果

## 通信协议

### 1. Agent 注册协议

#### 注册请求
```json
{
  "agent_id": "agent_001",
  "agent_name": "Production Agent 01",
  "version": "1.0.0",
  "capabilities": [
    "ssh",
    "rdp", 
    "telnet",
    "db_connection",
    "file_transfer"
  ],
  "system_info": {
    "os": "linux",
    "arch": "amd64",
    "hostname": "agent-node-01",
    "ip_address": "192.168.1.100",
    "cpu_cores": 4,
    "memory_gb": 8
  },
  "config": {
    "max_concurrent_sessions": 50,
    "max_concurrent_tasks": 10,
    "supported_protocols": ["ssh", "rdp", "telnet"],
    "plugin_versions": {
      "ssh": "1.0.0",
      "rdp": "1.0.0", 
      "telnet": "1.0.0"
    }
  },
  "timestamp": "2024-12-19T10:30:00Z"
}
```

#### 注册响应
```json
{
  "status": "success",
  "agent_id": "agent_001",
  "assigned_region": "region_east",
  "heartbeat_interval": 30,
  "task_poll_interval": 5,
  "config_updates": {
    "log_level": "info",
    "max_session_duration": 3600,
    "enable_metrics": true
  },
  "ops_endpoints": {
    "task_api": "https://ops.example.com/api/v1/tasks",
    "status_api": "https://ops.example.com/api/v1/status",
    "metrics_api": "https://ops.example.com/api/v1/metrics"
  },
  "timestamp": "2024-12-19T10:30:01Z"
}
```

### 2. 心跳协议

#### 心跳请求
```json
{
  "agent_id": "agent_001",
  "status": "running",
  "uptime": 86400,
  "active_sessions": 15,
  "active_tasks": 3,
  "resource_usage": {
    "cpu_percent": 25.5,
    "memory_percent": 45.2,
    "disk_percent": 60.1,
    "network_in_mbps": 10.5,
    "network_out_mbps": 8.3
  },
  "plugin_status": {
    "ssh": {
      "status": "running",
      "connections": 10,
      "sessions": 8
    },
    "rdp": {
      "status": "running", 
      "connections": 5,
      "sessions": 3
    }
  },
  "timestamp": "2024-12-19T10:30:30Z"
}
```

#### 心跳响应
```json
{
  "status": "acknowledged",
  "next_heartbeat": 30,
  "pending_tasks": 2,
  "config_version": "1.0.1",
  "commands": [
    {
      "type": "config_update",
      "data": {
        "log_level": "debug"
      }
    }
  ],
  "timestamp": "2024-12-19T10:30:31Z"
}
```

### 3. 任务分发协议

#### 任务分配
```json
{
  "task_id": "task_20241219_001",
  "command_id": "cmd_001",
  "task_type": "remote_execution",
  "priority": "normal",
  "target": {
    "protocol": "ssh",
    "host": "192.168.1.200",
    "port": 22,
    "credentials": {
      "username": "admin",
      "auth_method": "password",
      "password_encrypted": "encrypted_password_data"
    }
  },
  "command": {
    "type": "shell",
    "content": "systemctl status nginx",
    "timeout": 30,
    "working_directory": "/tmp"
  },
  "options": {
    "capture_output": true,
    "return_exit_code": true,
    "environment": {
      "LANG": "en_US.UTF-8"
    }
  },
  "metadata": {
    "user_id": "user_001",
    "session_id": "session_001",
    "request_id": "req_001"
  },
  "created_at": "2024-12-19T10:31:00Z",
  "expires_at": "2024-12-19T10:36:00Z"
}
```

#### 任务确认
```json
{
  "task_id": "task_20241219_001",
  "agent_id": "agent_001",
  "status": "accepted",
  "estimated_duration": 30,
  "assigned_worker": "worker_001",
  "timestamp": "2024-12-19T10:31:01Z"
}
```

### 4. 状态上报协议

#### 任务状态更新
```json
{
  "task_id": "task_20241219_001",
  "agent_id": "agent_001",
  "status": "running",
  "progress": 50,
  "current_step": "executing_command",
  "output": {
    "stdout": "● nginx.service - A high performance web server\n",
    "stderr": "",
    "exit_code": null
  },
  "resource_usage": {
    "cpu_percent": 15.2,
    "memory_mb": 128
  },
  "timestamp": "2024-12-19T10:31:15Z"
}
```

#### 任务完成报告
```json
{
  "task_id": "task_20241219_001",
  "agent_id": "agent_001", 
  "status": "completed",
  "result": {
    "success": true,
    "exit_code": 0,
    "output": {
      "stdout": "● nginx.service - A high performance web server\n   Loaded: loaded (/lib/systemd/system/nginx.service; enabled; vendor preset: enabled)\n   Active: active (running) since Wed 2024-12-19 10:25:00 UTC; 6min ago\n",
      "stderr": "",
      "output_size": 256
    },
    "execution_time": 1.5,
    "resource_usage": {
      "peak_cpu_percent": 20.1,
      "peak_memory_mb": 150,
      "network_bytes_sent": 1024,
      "network_bytes_received": 2048
    }
  },
  "metadata": {
    "connection_id": "conn_001",
    "session_duration": 2.1,
    "retry_count": 0
  },
  "started_at": "2024-12-19T10:31:01Z",
  "completed_at": "2024-12-19T10:31:02Z"
}
```

## API 接口规范

### 1. Agent 管理接口

#### 注册 Agent
```http
POST /api/v1/agents/register
Content-Type: application/json
Authorization: Bearer <agent_token>

{
  "agent_id": "agent_001",
  "agent_name": "Production Agent 01",
  ...
}
```

#### 更新 Agent 状态
```http
PUT /api/v1/agents/{agent_id}/status
Content-Type: application/json
Authorization: Bearer <agent_token>

{
  "status": "running",
  "uptime": 86400,
  ...
}
```

#### 获取 Agent 配置
```http
GET /api/v1/agents/{agent_id}/config
Authorization: Bearer <agent_token>

Response:
{
  "config_version": "1.0.1",
  "settings": {
    "log_level": "info",
    "max_sessions": 50
  }
}
```

### 2. 任务管理接口

#### 获取待执行任务
```http
GET /api/v1/agents/{agent_id}/tasks/pending
Authorization: Bearer <agent_token>

Response:
{
  "tasks": [
    {
      "task_id": "task_001",
      "task_type": "remote_execution",
      ...
    }
  ],
  "total": 1
}
```

#### 提交任务结果
```http
POST /api/v1/tasks/{task_id}/result
Content-Type: application/json
Authorization: Bearer <agent_token>

{
  "status": "completed",
  "result": {
    "success": true,
    "output": "command output"
  }
}
```

#### 更新任务状态
```http
PUT /api/v1/tasks/{task_id}/status
Content-Type: application/json
Authorization: Bearer <agent_token>

{
  "status": "running",
  "progress": 50,
  "current_step": "executing"
}
```

### 3. 监控接口

#### 上报指标数据
```http
POST /api/v1/agents/{agent_id}/metrics
Content-Type: application/json
Authorization: Bearer <agent_token>

{
  "timestamp": "2024-12-19T10:30:00Z",
  "metrics": {
    "cpu_percent": 25.5,
    "memory_percent": 45.2,
    "active_sessions": 15
  }
}
```

#### 获取健康检查
```http
GET /api/v1/agents/{agent_id}/health
Authorization: Bearer <agent_token>

Response:
{
  "status": "healthy",
  "checks": {
    "connectivity": "ok",
    "plugins": "ok",
    "resources": "ok"
  }
}
```

## 安全机制

### 1. 认证授权

#### Token 认证
```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

#### 双向 TLS 认证
```yaml
tls:
  enabled: true
  cert_file: "/etc/ssl/certs/agent.crt"
  key_file: "/etc/ssl/private/agent.key"
  ca_file: "/etc/ssl/certs/ca.crt"
  verify_client: true
```

### 2. 数据加密

#### 传输加密
- 所有通信使用 TLS 1.3 加密
- 支持证书固定 (Certificate Pinning)
- 定期轮换加密密钥

#### 敏感数据加密
```json
{
  "credentials": {
    "username": "admin",
    "password_encrypted": "AES256:base64_encrypted_data",
    "encryption_key_id": "key_001"
  }
}
```

### 3. 访问控制

#### RBAC 权限模型
```yaml
roles:
  - name: "agent_executor"
    permissions:
      - "tasks:execute"
      - "status:report"
      - "metrics:submit"
  - name: "agent_admin"
    permissions:
      - "agent:register"
      - "config:update"
      - "plugins:manage"
```

## 配置管理

### 1. Agent 配置

#### 基础配置 (agent.yaml)
```yaml
ops:
  # OPS 服务连接配置
  endpoint: "https://ops.example.com"
  api_version: "v1"
  timeout: 30s
  retry_count: 3
  retry_interval: 5s
  
  # 认证配置
  auth:
    type: "token"  # token, mtls, oauth2
    token: "${OPS_AGENT_TOKEN}"
    
  # TLS 配置
  tls:
    enabled: true
    insecure_skip_verify: false
    cert_file: "/etc/ssl/certs/agent.crt"
    key_file: "/etc/ssl/private/agent.key"
    ca_file: "/etc/ssl/certs/ca.crt"

agent:
  # Agent 基础信息
  id: "${AGENT_ID}"
  name: "${AGENT_NAME}"
  region: "${AGENT_REGION}"
  
  # 心跳配置
  heartbeat:
    interval: 30s
    timeout: 10s
    max_failures: 3
    
  # 任务配置
  tasks:
    poll_interval: 5s
    max_concurrent: 10
    default_timeout: 300s
    result_retention: 24h
    
  # 资源限制
  resources:
    max_sessions: 50
    max_memory_mb: 2048
    max_cpu_percent: 80
```

### 2. 动态配置更新

#### 配置版本控制
```json
{
  "config_version": "1.0.1",
  "updated_at": "2024-12-19T10:30:00Z",
  "changes": [
    {
      "path": "agent.tasks.max_concurrent",
      "old_value": 10,
      "new_value": 15,
      "reason": "Increased capacity"
    }
  ]
}
```

#### 热重载机制
```go
// 配置更新处理
func (c *OPSClient) handleConfigUpdate(update *ConfigUpdate) error {
    // 验证配置
    if err := c.validateConfig(update.Config); err != nil {
        return err
    }
    
    // 应用配置
    if err := c.applyConfig(update.Config); err != nil {
        return err
    }
    
    // 确认更新
    return c.confirmConfigUpdate(update.Version)
}
```

## 部署配置

### 1. 环境变量

```bash
# Agent 标识
export AGENT_ID="agent_001"
export AGENT_NAME="Production Agent 01"
export AGENT_REGION="region_east"

# OPS 服务配置
export OPS_ENDPOINT="https://ops.example.com"
export OPS_AGENT_TOKEN="your_agent_token_here"

# 日志配置
export LOG_LEVEL="info"
export LOG_FORMAT="json"

# 资源限制
export MAX_SESSIONS="50"
export MAX_CONCURRENT_TASKS="10"
```

### 2. Docker 部署

#### Dockerfile
```dockerfile
FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY . .
RUN go mod tidy && go build -o agent ./cmd/agent

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/agent .
COPY --from=builder /app/etc/agent.yaml ./etc/

CMD ["./agent", "-f", "etc/agent.yaml"]
```

#### Docker Compose
```yaml
version: '3.8'

services:
  newbee-agent:
    build: .
    environment:
      - AGENT_ID=agent_001
      - AGENT_NAME=Production Agent 01
      - OPS_ENDPOINT=https://ops.example.com
      - OPS_AGENT_TOKEN=${OPS_AGENT_TOKEN}
    volumes:
      - ./etc:/root/etc
      - ./logs:/root/logs
      - /etc/ssl/certs:/etc/ssl/certs:ro
    ports:
      - "8889:8889"
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8889/health"]
      interval: 30s
      timeout: 10s
      retries: 3
```

### 3. Kubernetes 部署

#### Deployment
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: newbee-agent
  namespace: ops
spec:
  replicas: 3
  selector:
    matchLabels:
      app: newbee-agent
  template:
    metadata:
      labels:
        app: newbee-agent
    spec:
      containers:
      - name: agent
        image: newbee/agent:1.0.0
        env:
        - name: AGENT_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: OPS_ENDPOINT
          value: "https://ops.example.com"
        - name: OPS_AGENT_TOKEN
          valueFrom:
            secretKeyRef:
              name: ops-credentials
              key: agent-token
        ports:
        - containerPort: 8889
        livenessProbe:
          httpGet:
            path: /health
            port: 8889
          initialDelaySeconds: 30
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /health
            port: 8889
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "2Gi"
            cpu: "1000m"
```

## 监控和告警

### 1. 指标收集

#### 系统指标
```json
{
  "system_metrics": {
    "cpu_percent": 25.5,
    "memory_percent": 45.2,
    "disk_percent": 60.1,
    "network_in_mbps": 10.5,
    "network_out_mbps": 8.3,
    "load_average": [1.2, 1.1, 1.0]
  }
}
```

#### 业务指标
```json
{
  "business_metrics": {
    "active_sessions": 15,
    "active_tasks": 3,
    "completed_tasks_1h": 120,
    "failed_tasks_1h": 2,
    "avg_task_duration": 5.2,
    "connection_success_rate": 98.5
  }
}
```

### 2. 告警规则

#### Prometheus 告警规则
```yaml
groups:
- name: newbee-agent
  rules:
  - alert: AgentDown
    expr: up{job="newbee-agent"} == 0
    for: 1m
    labels:
      severity: critical
    annotations:
      summary: "NewBee Agent is down"
      
  - alert: HighCPUUsage
    expr: agent_cpu_percent > 80
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High CPU usage on agent {{ $labels.agent_id }}"
      
  - alert: TaskFailureRate
    expr: rate(agent_tasks_failed_total[5m]) > 0.1
    for: 2m
    labels:
      severity: warning
    annotations:
      summary: "High task failure rate on agent {{ $labels.agent_id }}"
```

## 故障处理

### 1. 常见问题

#### 连接问题
```yaml
问题: Agent 无法连接到 OPS 服务
原因:
  - 网络连接问题
  - 认证凭据错误
  - TLS 证书问题
解决:
  - 检查网络连通性
  - 验证认证令牌
  - 检查证书有效性
```

#### 任务执行问题
```yaml
问题: 任务执行失败
原因:
  - 目标主机不可达
  - 认证失败
  - 资源不足
解决:
  - 检查目标主机状态
  - 验证认证信息
  - 检查资源使用情况
```

### 2. 故障恢复

#### 自动重连机制
```go
func (c *OPSClient) maintainConnection() {
    for {
        if !c.isConnected() {
            if err := c.reconnect(); err != nil {
                c.logger.Errorf("重连失败: %v", err)
                time.Sleep(c.config.RetryInterval)
                continue
            }
        }
        time.Sleep(c.config.HeartbeatInterval)
    }
}
```

#### 任务重试机制
```go
func (te *TaskExecutor) executeWithRetry(task *Task) error {
    var lastErr error
    for i := 0; i < task.MaxRetries; i++ {
        if err := te.execute(task); err != nil {
            lastErr = err
            time.Sleep(time.Duration(i+1) * time.Second)
            continue
        }
        return nil
    }
    return lastErr
}
```

## 性能优化

### 1. 连接池优化

```go
type ConnectionPool struct {
    maxSize     int
    idleTimeout time.Duration
    connections chan *Connection
    active      map[string]*Connection
    mutex       sync.RWMutex
}

func (p *ConnectionPool) Get(target string) (*Connection, error) {
    p.mutex.RLock()
    if conn, exists := p.active[target]; exists && conn.IsValid() {
        p.mutex.RUnlock()
        return conn, nil
    }
    p.mutex.RUnlock()
    
    return p.createConnection(target)
}
```

### 2. 批量处理

```go
func (c *OPSClient) batchSubmitResults(results []*TaskResult) error {
    if len(results) == 0 {
        return nil
    }
    
    batch := &BatchResultSubmission{
        AgentID: c.agentID,
        Results: results,
        Timestamp: time.Now(),
    }
    
    return c.submitBatch(batch)
}
```

### 3. 缓存优化

```go
type ConfigCache struct {
    cache      map[string]*Config
    expiration map[string]time.Time
    mutex      sync.RWMutex
    ttl        time.Duration
}

func (c *ConfigCache) Get(key string) (*Config, bool) {
    c.mutex.RLock()
    defer c.mutex.RUnlock()
    
    if exp, exists := c.expiration[key]; exists && time.Now().Before(exp) {
        if config, exists := c.cache[key]; exists {
            return config, true
        }
    }
    
    return nil, false
}
```

## 最佳实践

### 1. 错误处理
- 实现指数退避重试机制
- 记录详细的错误上下文
- 提供有意义的错误信息

### 2. 资源管理
- 使用连接池减少资源开销
- 实现超时和取消机制
- 定期清理过期资源

### 3. 安全实践
- 使用强加密算法
- 定期轮换认证凭据
- 实施最小权限原则

### 4. 监控实践
- 收集关键业务指标
- 设置合理的告警阈值
- 实现健康检查机制

---

**注意**: 本集成方案遵循企业级标准，确保高可用性、安全性和可扩展性。所有接口都支持版本控制和向后兼容。 