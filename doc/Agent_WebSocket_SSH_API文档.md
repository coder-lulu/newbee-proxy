# Agent WebSocket SSH连接API文档

> **Version**: 1.0.0  
> **Last Update**: 2024-12-20  
> **Author**: @newbee-team  

## 概述

本文档详细描述了NewBee Agent通过WebSocket方式进行SSH连接的API接口，包括连接建立、数据传输、会话管理等完整功能。

## 架构说明

### 系统架构
- **Agent服务**: 基于go-zero框架的微服务，提供WebSocket接口
- **插件系统**: 模块化的协议插件，支持SSH、Telnet、RDP等协议
- **WebSocket桥接**: 实时双向数据传输，支持终端交互
- **会话管理**: 完整的连接生命周期管理

### 核心组件
- **SSH插件** (`plugins/ssh/`): 处理SSH协议连接和认证
- **WebSocket处理器** (`internal/handlers/websocket_handler.go`): 处理WebSocket升级和消息路由
- **连接管理器**: 管理活跃连接和会话状态
- **安全模块**: 处理认证、授权和数据加密

---

## 1. SSH WebSocket连接接口

### 1.1 建立SSH WebSocket连接

**接口路径**: `GET /ws/ssh`

**协议升级**: HTTP -> WebSocket

**连接URL示例**:
```
ws://agent-host:8889/ws/ssh
```

### 1.2 连接建立流程

#### 步骤1: WebSocket握手
客户端发起WebSocket连接请求，服务端升级协议为WebSocket。

#### 步骤2: 发送SSH连接请求
连接建立后，客户端立即发送SSH连接配置信息。

**请求格式** (JSON):
```json
{
    "target": "192.168.1.100",          // SSH服务器地址
    "port": 22,                         // SSH端口号
    "username": "root",                 // 用户名
    "password": "password123",          // 密码（可选）
    "private_key": "",                  // 私钥内容（可选）
    "auth_type": "password",            // 认证类型: password/key/mixed
    "cols": 80,                         // 终端列数
    "rows": 24,                         // 终端行数
    "session_id": "sess_20241220_001"   // 会话ID（可选）
}
```

#### 步骤3: 接收连接响应
服务端处理连接请求并返回结果。

**成功响应**:
```json
{
    "success": true,
    "message": "SSH tunnel established",
    "connection_id": "ssh_1703001234567_abc123",
    "session_id": "sess_20241220_001"
}
```

**失败响应**:
```json
{
    "success": false,
    "message": "SSH connection failed: Authentication failed"
}
```

---

## 2. 数据传输协议

### 2.1 终端数据传输

连接建立成功后，WebSocket进入数据透传模式：

- **客户端 -> 服务端**: 键盘输入、命令等
- **服务端 -> 客户端**: 终端输出、响应等

### 2.2 数据格式

#### 输入数据（客户端发送）
直接发送原始数据，如：
```
ls -la\n
pwd\n
cat /etc/hosts\n
```

#### 输出数据（服务端发送）
直接返回SSH终端的输出，如：
```
total 48
drwxr-xr-x 2 root root 4096 Dec 20 10:30 .
drwxr-xr-x 3 root root 4096 Dec 20 10:25 ..
-rw-r--r-- 1 root root  220 Dec 20 10:25 .bash_logout
```

### 2.3 特殊控制序列

#### 终端大小调整
通过HTTP POST接口调整终端大小：

**接口路径**: `POST /api/ssh/resize`

**请求参数**:
```json
{
    "connection_id": "ssh_1703001234567_abc123",
    "cols": 120,
    "rows": 30
}
```

#### 信号发送
通过WebSocket发送特殊字符序列：
- `Ctrl+C`: 发送字节 `\x03`
- `Ctrl+D`: 发送字节 `\x04`
- `Ctrl+Z`: 发送字节 `\x1A`

---

## 3. 认证方式

### 3.1 密码认证

```json
{
    "auth_type": "password",
    "username": "root",
    "password": "yourpassword"
}
```

### 3.2 密钥认证

```json
{
    "auth_type": "key",
    "username": "root",
    "private_key": "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----"
}
```

### 3.3 混合认证

```json
{
    "auth_type": "mixed",
    "username": "root",
    "password": "keypassword",
    "private_key": "-----BEGIN RSA PRIVATE KEY-----\n..."
}
```

---

## 4. 错误处理

### 4.1 常见错误代码

| 错误类型 | 错误信息 | 解决方案 |
|---------|---------|---------|
| 连接超时 | "SSH connection failed: timeout" | 检查网络连接和目标主机状态 |
| 认证失败 | "SSH connection failed: Authentication failed" | 验证用户名密码或密钥 |
| 主机不可达 | "SSH connection failed: host unreachable" | 检查目标主机IP和端口 |
| 插件未启动 | "SSH plugin not available" | 重启Agent服务 |

### 4.2 错误响应格式

```json
{
    "success": false,
    "message": "具体错误信息",
    "error_code": "ERROR_TYPE",
    "details": {
        "target": "192.168.1.100:22",
        "auth_method": "password",
        "retry_allowed": true
    }
}
```

---

## 5. 配置说明

### 5.1 Agent配置文件 (etc/agent.yaml)

```yaml
# SSH插件配置
Plugins:
  SSH:
    MaxConnections: 100           # 最大并发连接数
    KeepAliveInterval: 30         # 心跳间隔（秒）
    ConnectionTimeout: 30         # 连接超时（秒）
    BufferSize: 4096             # 缓冲区大小
    EnableCompression: false      # 是否启用压缩
    SupportedCiphers:            # 支持的加密算法
      - "aes128-ctr"
      - "aes192-ctr"
      - "aes256-ctr"
```

### 5.2 WebSocket配置

```yaml
# REST服务配置
Host: 0.0.0.0
Port: 8889

# 日志配置
Log:
  ServiceName: agent
  Mode: console
  Level: info
```

---

## 6. 使用示例

### 6.1 JavaScript客户端示例

```javascript
// 建立WebSocket连接
const ws = new WebSocket('ws://localhost:8889/ws/ssh');

// 连接打开后发送SSH配置
ws.onopen = function() {
    const sshConfig = {
        target: "192.168.1.100",
        port: 22,
        username: "root",
        password: "password123",
        auth_type: "password",
        cols: 80,
        rows: 24,
        session_id: "demo_session_001"
    };
    
    ws.send(JSON.stringify(sshConfig));
};

// 处理连接响应和数据
ws.onmessage = function(event) {
    const data = event.data;
    
    // 尝试解析为JSON（连接响应）
    try {
        const response = JSON.parse(data);
        if (response.success) {
            console.log('SSH连接成功:', response.connection_id);
            // 连接成功后可以发送命令
            ws.send('ls -la\n');
        } else {
            console.error('SSH连接失败:', response.message);
        }
    } catch (e) {
        // 普通终端输出
        console.log('终端输出:', data);
        // 在终端界面显示数据
        displayTerminalOutput(data);
    }
};

// 发送命令到SSH终端
function sendCommand(command) {
    if (ws.readyState === WebSocket.OPEN) {
        ws.send(command + '\n');
    }
}

// 调整终端大小
function resizeTerminal(cols, rows) {
    fetch('/api/ssh/resize', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({
            connection_id: currentConnectionId,
            cols: cols,
            rows: rows
        })
    });
}
```

### 6.2 curl测试示例

由于WebSocket需要特殊处理，建议使用专门的WebSocket测试工具：

```bash
# 使用wscat测试（需要先安装: npm install -g wscat）
wscat -c ws://localhost:8889/ws/ssh

# 连接建立后发送配置
{"target":"192.168.1.100","port":22,"username":"root","password":"password123","auth_type":"password","cols":80,"rows":24}

# 然后可以发送命令
ls -la
pwd
whoami
```

### 6.3 Python客户端示例

```python
import asyncio
import websockets
import json

async def ssh_client():
    uri = "ws://localhost:8889/ws/ssh"
    
    async with websockets.connect(uri) as websocket:
        # 发送SSH连接配置
        ssh_config = {
            "target": "192.168.1.100",
            "port": 22,
            "username": "root",
            "password": "password123",
            "auth_type": "password",
            "cols": 80,
            "rows": 24,
            "session_id": "python_client_001"
        }
        
        await websocket.send(json.dumps(ssh_config))
        
        # 接收连接响应
        response = await websocket.recv()
        print(f"连接响应: {response}")
        
        # 解析响应
        try:
            resp_data = json.loads(response)
            if resp_data.get('success'):
                print(f"SSH连接成功，连接ID: {resp_data.get('connection_id')}")
                
                # 发送命令
                await websocket.send("ls -la\n")
                
                # 接收输出
                while True:
                    output = await websocket.recv()
                    print(f"终端输出: {output}")
                    
        except json.JSONDecodeError:
            print(f"终端数据: {response}")

# 运行客户端
asyncio.run(ssh_client())
```

---

## 7. 安全考虑

### 7.1 网络安全
- 建议在生产环境中使用WSS（WebSocket over TLS）
- 实施IP白名单限制
- 配置防火墙规则限制访问

### 7.2 认证安全
- 避免在代码中硬编码密码
- 使用密钥认证替代密码认证
- 实施会话超时机制

### 7.3 数据安全
- 敏感数据传输加密
- 避免在日志中记录密码
- 实施访问审计日志

---

## 8. 监控和调试

### 8.1 健康检查接口

```bash
# 检查Agent状态
curl http://localhost:8889/health

# 检查插件状态
curl http://localhost:8889/plugins

# 查看详细状态
curl http://localhost:8889/status
```

### 8.2 日志配置

```yaml
Log:
  ServiceName: agent
  Mode: console
  Level: debug    # 调试时使用debug级别
```

### 8.3 常见调试方法

1. **检查WebSocket连接**: 使用浏览器开发者工具Network面板
2. **查看Agent日志**: 观察连接建立和数据传输日志
3. **验证SSH连接**: 直接使用SSH客户端测试目标主机
4. **网络排查**: 使用telnet测试端口连通性

---

## 9. 性能优化

### 9.1 连接池配置

```yaml
Plugins:
  SSH:
    MaxConnections: 100      # 根据服务器性能调整
    KeepAliveInterval: 30    # 心跳间隔
    ConnectionTimeout: 30    # 连接超时
```

### 9.2 缓冲区优化

```yaml
Plugins:
  SSH:
    BufferSize: 4096        # 缓冲区大小，影响数据传输效率
```

### 9.3 并发限制

```yaml
Limits:
  MaxConcurrentSessions: 1000   # 最大并发会话数
  SessionTimeoutSeconds: 3600   # 会话超时时间
```

---

## 10. 故障排查

### 10.1 连接问题

| 问题症状 | 可能原因 | 解决方案 |
|---------|---------|---------|
| WebSocket连接失败 | 端口被占用或防火墙阻拦 | 检查端口状态和防火墙配置 |
| SSH认证失败 | 用户名密码错误 | 验证凭据正确性 |
| 连接超时 | 网络延迟或目标主机不可达 | 检查网络连通性 |
| 数据传输中断 | WebSocket连接断开 | 实现重连机制 |

### 10.2 性能问题

| 问题症状 | 可能原因 | 解决方案 |
|---------|---------|---------|
| 响应延迟高 | 缓冲区太小或网络延迟 | 调整缓冲区大小 |
| 内存占用高 | 连接泄漏 | 检查连接关闭逻辑 |
| CPU占用高 | 处理逻辑效率低 | 优化数据处理算法 |

---

## 11. 版本兼容性

### 11.1 当前版本特性
- **v1.0.0**: 基础SSH WebSocket连接功能
- 支持密码和密钥认证
- 支持终端大小调整
- 支持会话管理

### 11.2 后续版本规划
- **v1.1.0**: 支持SFTP文件传输
- **v1.2.0**: 支持会话录制和回放
- **v1.3.0**: 支持多跳SSH连接

---

## 联系方式

如有疑问或需要技术支持，请联系NewBee开发团队。

**文档最后更新时间**: 2024-12-20 