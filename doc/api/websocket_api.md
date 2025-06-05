# NewBee Agent WebSocket API 文档

> **Version**: 1.0.0  
> **Last Update**: 2024-12-19  
> **Author**: @newbee-team  

## 概述

NewBee Agent 提供基于 WebSocket 的实时双向通信接口，支持多种远程连接协议的数据传输。本文档详细描述了 WebSocket API 的使用方法和协议规范。

## 连接建立

### 基础连接
```
ws://[agent-host]:[port]/ws/connect
```

### 带参数连接
```
ws://[agent-host]:[port]/ws/connect?protocol=ssh&target=192.168.1.100:22&session_id=xxx
```

### 连接参数

| 参数名 | 类型 | 必填 | 描述 |
|--------|------|------|------|
| protocol | string | 是 | 连接协议 (ssh/rdp/telnet/vnc) |
| target | string | 是 | 目标地址 (host:port) |
| session_id | string | 否 | 会话ID，用于恢复连接 |
| encoding | string | 否 | 字符编码 (utf-8/gbk/ascii) |
| compression | boolean | 否 | 是否启用压缩 |

## 消息格式

### 基础消息结构
```json
{
  "type": "message_type",
  "session_id": "session_identifier",
  "timestamp": 1703001234567,
  "data": {},
  "metadata": {}
}
```

### 消息类型

#### 1. 连接控制消息

##### 连接请求 (connect)
```json
{
  "type": "connect",
  "data": {
    "protocol": "ssh",
    "target": "192.168.1.100:22",
    "credentials": {
      "username": "admin",
      "password": "password",
      "private_key": "-----BEGIN RSA PRIVATE KEY-----...",
      "auth_method": "password"
    },
    "options": {
      "terminal_type": "xterm-256color",
      "width": 80,
      "height": 24,
      "encoding": "utf-8"
    }
  }
}
```

##### 连接响应 (connect_response)
```json
{
  "type": "connect_response",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "status": "success",
    "connection_id": "conn_1703001234567_def456",
    "protocol": "ssh",
    "target": "192.168.1.100:22",
    "server_info": {
      "hostname": "server01",
      "os": "Linux",
      "version": "Ubuntu 20.04"
    }
  }
}
```

##### 连接错误 (connect_error)
```json
{
  "type": "connect_error",
  "data": {
    "error_code": "AUTH_FAILED",
    "error_message": "Authentication failed: Invalid username or password",
    "details": {
      "target": "192.168.1.100:22",
      "auth_method": "password",
      "retry_allowed": true
    }
  }
}
```

#### 2. 数据传输消息

##### 数据输入 (data_input)
```json
{
  "type": "data_input",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "content": "ls -la\n",
    "encoding": "utf-8",
    "binary": false
  }
}
```

##### 数据输出 (data_output)
```json
{
  "type": "data_output",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "content": "total 48\ndrwxr-xr-x 2 admin admin 4096 Dec 19 10:30 .\n",
    "encoding": "utf-8",
    "binary": false,
    "stream": "stdout"
  }
}
```

##### 二进制数据 (binary_data)
```json
{
  "type": "binary_data",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "content": "base64_encoded_binary_data",
    "size": 1024,
    "checksum": "sha256_hash"
  }
}
```

#### 3. 终端控制消息

##### 终端调整 (terminal_resize)
```json
{
  "type": "terminal_resize",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "width": 120,
    "height": 30
  }
}
```

##### 终端信号 (terminal_signal)
```json
{
  "type": "terminal_signal",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "signal": "SIGINT",
    "key_code": 3
  }
}
```

#### 4. 会话管理消息

##### 心跳 (heartbeat)
```json
{
  "type": "heartbeat",
  "session_id": "sess_1703001234567_abc123",
  "timestamp": 1703001234567
}
```

##### 心跳响应 (heartbeat_response)
```json
{
  "type": "heartbeat_response",
  "session_id": "sess_1703001234567_abc123",
  "timestamp": 1703001234567,
  "data": {
    "server_time": 1703001234567,
    "latency": 15
  }
}
```

##### 会话状态 (session_status)
```json
{
  "type": "session_status",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "status": "active",
    "uptime": 3600,
    "bytes_sent": 1024,
    "bytes_received": 2048,
    "last_activity": 1703001234567
  }
}
```

##### 断开连接 (disconnect)
```json
{
  "type": "disconnect",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "reason": "user_requested",
    "message": "Connection closed by user"
  }
}
```

## 协议特定消息

### SSH 协议

#### SSH 认证挑战 (ssh_auth_challenge)
```json
{
  "type": "ssh_auth_challenge",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "challenge_type": "keyboard_interactive",
    "prompt": "Please enter your OTP:",
    "echo": false
  }
}
```

#### SSH 认证响应 (ssh_auth_response)
```json
{
  "type": "ssh_auth_response",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "response": "123456"
  }
}
```

### RDP 协议

#### RDP 屏幕更新 (rdp_screen_update)
```json
{
  "type": "rdp_screen_update",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "image_data": "base64_encoded_image",
    "format": "png",
    "x": 0,
    "y": 0,
    "width": 1920,
    "height": 1080,
    "incremental": false
  }
}
```

#### RDP 鼠标事件 (rdp_mouse_event)
```json
{
  "type": "rdp_mouse_event",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "x": 100,
    "y": 200,
    "button": "left",
    "action": "click"
  }
}
```

#### RDP 键盘事件 (rdp_keyboard_event)
```json
{
  "type": "rdp_keyboard_event",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "key_code": 65,
    "action": "press",
    "modifiers": ["ctrl", "shift"]
  }
}
```

## 错误处理

### 错误代码

| 错误代码 | 描述 | 处理建议 |
|----------|------|----------|
| INVALID_PROTOCOL | 不支持的协议 | 检查协议参数 |
| AUTH_FAILED | 认证失败 | 检查用户名密码 |
| CONNECTION_TIMEOUT | 连接超时 | 检查网络连接 |
| TARGET_UNREACHABLE | 目标不可达 | 检查目标地址 |
| SESSION_EXPIRED | 会话过期 | 重新建立连接 |
| RESOURCE_LIMIT | 资源限制 | 等待或联系管理员 |
| INTERNAL_ERROR | 内部错误 | 联系技术支持 |

### 错误消息格式
```json
{
  "type": "error",
  "session_id": "sess_1703001234567_abc123",
  "data": {
    "error_code": "CONNECTION_TIMEOUT",
    "error_message": "Connection to target host timed out",
    "details": {
      "target": "192.168.1.100:22",
      "timeout": 30,
      "retry_count": 3
    },
    "timestamp": 1703001234567
  }
}
```

## 客户端实现示例

### JavaScript 客户端
```javascript
class NewBeeAgentClient {
  constructor(wsUrl) {
    this.wsUrl = wsUrl;
    this.ws = null;
    this.sessionId = null;
    this.callbacks = {};
  }

  connect(protocol, target, credentials) {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(this.wsUrl);
      
      this.ws.onopen = () => {
        const connectMsg = {
          type: 'connect',
          data: {
            protocol: protocol,
            target: target,
            credentials: credentials
          }
        };
        this.ws.send(JSON.stringify(connectMsg));
      };

      this.ws.onmessage = (event) => {
        const message = JSON.parse(event.data);
        this.handleMessage(message, resolve, reject);
      };

      this.ws.onerror = (error) => {
        reject(error);
      };
    });
  }

  handleMessage(message, resolve, reject) {
    switch (message.type) {
      case 'connect_response':
        this.sessionId = message.session_id;
        resolve(message.data);
        break;
      case 'connect_error':
        reject(new Error(message.data.error_message));
        break;
      case 'data_output':
        this.onDataOutput(message.data);
        break;
      // 处理其他消息类型...
    }
  }

  sendInput(data) {
    if (this.ws && this.sessionId) {
      const message = {
        type: 'data_input',
        session_id: this.sessionId,
        data: {
          content: data,
          encoding: 'utf-8',
          binary: false
        }
      };
      this.ws.send(JSON.stringify(message));
    }
  }

  disconnect() {
    if (this.ws && this.sessionId) {
      const message = {
        type: 'disconnect',
        session_id: this.sessionId,
        data: {
          reason: 'user_requested'
        }
      };
      this.ws.send(JSON.stringify(message));
      this.ws.close();
    }
  }
}
```

### Python 客户端
```python
import asyncio
import websockets
import json

class NewBeeAgentClient:
    def __init__(self, ws_url):
        self.ws_url = ws_url
        self.ws = None
        self.session_id = None

    async def connect(self, protocol, target, credentials):
        self.ws = await websockets.connect(self.ws_url)
        
        connect_msg = {
            "type": "connect",
            "data": {
                "protocol": protocol,
                "target": target,
                "credentials": credentials
            }
        }
        
        await self.ws.send(json.dumps(connect_msg))
        
        response = await self.ws.recv()
        message = json.loads(response)
        
        if message["type"] == "connect_response":
            self.session_id = message["session_id"]
            return message["data"]
        elif message["type"] == "connect_error":
            raise Exception(message["data"]["error_message"])

    async def send_input(self, data):
        if self.ws and self.session_id:
            message = {
                "type": "data_input",
                "session_id": self.session_id,
                "data": {
                    "content": data,
                    "encoding": "utf-8",
                    "binary": False
                }
            }
            await self.ws.send(json.dumps(message))

    async def listen(self):
        async for message in self.ws:
            data = json.loads(message)
            await self.handle_message(data)

    async def handle_message(self, message):
        if message["type"] == "data_output":
            print(message["data"]["content"], end="")
        # 处理其他消息类型...

    async def disconnect(self):
        if self.ws and self.session_id:
            message = {
                "type": "disconnect",
                "session_id": self.session_id,
                "data": {
                    "reason": "user_requested"
                }
            }
            await self.ws.send(json.dumps(message))
            await self.ws.close()
```

## 性能优化

### 1. 连接复用
- 支持多个会话共享同一个 WebSocket 连接
- 通过 session_id 区分不同的会话

### 2. 数据压缩
- 支持 gzip 压缩减少传输数据量
- 可通过连接参数启用压缩

### 3. 批量传输
- 支持批量发送多个消息
- 减少网络往返次数

### 4. 心跳机制
- 定期发送心跳保持连接活跃
- 检测连接状态和网络延迟

## 安全考虑

### 1. 认证授权
- 支持多种认证方式
- 会话令牌验证

### 2. 数据加密
- WebSocket 连接支持 TLS 加密
- 敏感数据传输加密

### 3. 访问控制
- 基于角色的访问控制
- 连接数量限制

### 4. 审计日志
- 记录所有连接和操作
- 支持审计追踪

## 故障排查

### 常见问题

1. **连接失败**
   - 检查网络连接
   - 验证目标地址和端口
   - 确认认证信息

2. **数据传输异常**
   - 检查字符编码设置
   - 验证消息格式
   - 查看错误日志

3. **会话断开**
   - 检查网络稳定性
   - 验证心跳机制
   - 查看超时设置

### 调试工具

1. **浏览器开发者工具**
   - 查看 WebSocket 连接状态
   - 监控消息收发

2. **网络抓包工具**
   - Wireshark 分析网络流量
   - tcpdump 捕获数据包

3. **日志分析**
   - Agent 服务日志
   - 客户端错误日志

---

**注意**: 本 API 文档遵循 RESTful 设计原则，支持版本控制和向后兼容。所有接口都经过充分测试，确保稳定性和可靠性。 