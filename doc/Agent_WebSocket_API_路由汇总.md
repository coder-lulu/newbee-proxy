# Agent WebSocket API路由汇总文档

> **Version**: 1.0.0  
> **Last Update**: 2024-12-20  
> **Author**: @newbee-team  

## 概述

本文档汇总了NewBee Agent提供的所有WebSocket API路由，包括SSH、Telnet、RDP、VNC等协议的连接接口。

---

## 1. SSH连接接口

### 1.1 原生SSH WebSocket

**接口路径**: `GET /ws/ssh`

**描述**: 基于SSH插件的原生WebSocket连接

**连接URL**:
```
ws://agent-host:8889/ws/ssh
```

**初始化消息格式**:
```json
{
    "target": "192.168.1.100",
    "port": 22,
    "username": "root",
    "password": "password123",
    "auth_type": "password",
    "cols": 80,
    "rows": 24,
    "session_id": "ssh_session_001"
}
```

### 1.2 Guacamole SSH WebSocket

**接口路径**: `GET /api/ssh/websocket`

**描述**: 通过Guacamole守护进程的SSH连接

**连接URL**:
```
ws://agent-host:8889/api/ssh/websocket?hostname=192.168.1.100&port=22&username=root&password=password123
```

**查询参数**:
- `hostname`: SSH服务器地址
- `port`: SSH端口号
- `username`: 用户名
- `password`: 密码
- `private-key`: 私钥内容（可选）
- `passphrase`: 私钥密码（可选）
- `font-name`: 字体名称（可选，默认：monospace）
- `font-size`: 字体大小（可选，默认：12）
- `color-scheme`: 颜色方案（可选）
- `enable-sftp`: 启用SFTP（可选，true/false）

---

## 2. Telnet连接接口

### 2.1 原生Telnet WebSocket

**接口路径**: `GET /ws/telnet`

**描述**: 基于Telnet插件的WebSocket连接

**连接URL**:
```
ws://agent-host:8889/ws/telnet
```

**初始化消息格式**:
```json
{
    "target": "192.168.1.100",
    "port": 23,
    "username": "admin",
    "password": "admin123",
    "cols": 80,
    "rows": 24,
    "session_id": "telnet_session_001"
}
```

### 2.2 Guacamole Telnet WebSocket

**接口路径**: `GET /api/telnet/websocket`

**描述**: 通过Guacamole守护进程的Telnet连接

**连接URL**:
```
ws://agent-host:8889/api/telnet/websocket?hostname=192.168.1.100&port=23&username=admin&password=admin123
```

---

## 3. RDP连接接口

### 3.1 Guacamole RDP WebSocket

**接口路径**: `GET /api/rdp/websocket`

**描述**: 通过Guacamole守护进程的RDP连接

**连接URL**:
```
ws://agent-host:8889/api/rdp/websocket?hostname=192.168.1.100&port=3389&username=Administrator&password=admin123
```

**查询参数**:
- `hostname`: RDP服务器地址
- `port`: RDP端口号（默认：3389）
- `username`: 用户名
- `password`: 密码
- `domain`: 域名（可选）
- `width`: 屏幕宽度（可选，默认：1920）
- `height`: 屏幕高度（可选，默认：1080）
- `color-depth`: 颜色深度（可选，默认：32）
- `dpi`: DPI设置（可选，默认：96）
- `ignore-cert`: 忽略证书（可选，true/false）
- `enable-wallpaper`: 启用壁纸（可选，true/false）
- `enable-theming`: 启用主题（可选，true/false）
- `enable-font-smoothing`: 启用字体平滑（可选，true/false）
- `enable-desktop-composition`: 启用桌面组合（可选，true/false）
- `enable-menu-animations`: 启用菜单动画（可选，true/false）
- `disable-bitmap-caching`: 禁用位图缓存（可选，true/false）
- `disable-offscreen-caching`: 禁用屏幕外缓存（可选，true/false）

### 3.2 RDP兼容接口

**接口路径**: `GET /api/rdp/guacamole`

**描述**: RDP连接的兼容性接口，指向同一个处理器

---

## 4. VNC连接接口

### 4.1 Guacamole VNC WebSocket

**接口路径**: `GET /api/vnc/websocket`

**描述**: 通过Guacamole守护进程的VNC连接

**连接URL**:
```
ws://agent-host:8889/api/vnc/websocket?hostname=192.168.1.100&port=5901&password=vncpass
```

**查询参数**:
- `hostname`: VNC服务器地址
- `port`: VNC端口号（默认：5900）
- `password`: VNC密码
- `width`: 屏幕宽度（可选）
- `height`: 屏幕高度（可选）
- `color-depth`: 颜色深度（可选）
- `read-only`: 只读模式（可选，true/false）
- `swap-red-blue`: 交换红蓝色（可选，true/false）
- `cursor`: 光标模式（可选：local/remote/dot）
- `encodings`: 编码方式（可选）

---

## 5. 统一WebSocket接口

### 5.1 通用协议接口

**接口路径**: `GET /guacamole`

**描述**: 统一的多协议WebSocket接口，通过参数指定协议类型

**连接URL**:
```
ws://agent-host:8889/guacamole?scheme=ssh&hostname=192.168.1.100&port=22&username=root&password=password123
```

**通用查询参数**:
- `scheme`: 协议类型（ssh/rdp/vnc/telnet）
- `hostname`: 目标主机地址
- `port`: 目标端口
- `username`: 用户名
- `password`: 密码

---

## 6. 辅助接口

### 6.1 终端大小调整

**接口路径**: `POST /api/ssh/resize`

**请求方式**: HTTP POST

**请求参数**:
```json
{
    "connection_id": "ssh_1703001234567_abc123",
    "cols": 120,
    "rows": 30
}
```

**响应格式**:
```json
{
    "success": true,
    "message": "Terminal resized successfully"
}
```

### 6.2 健康检查接口

**接口路径**: `GET /health`

**请求方式**: HTTP GET

**响应格式**:
```json
{
    "status": "healthy",
    "timestamp": 1703001234567,
    "version": "v1.0.0",
    "uptime": 3600
}
```

### 6.3 插件状态接口

**接口路径**: `GET /plugins`

**请求方式**: HTTP GET

**响应格式**:
```json
{
    "plugins": [
        {
            "name": "ssh",
            "version": "v1.0.0",
            "status": "running",
            "connections": 5
        },
        {
            "name": "rdp",
            "version": "v1.0.0", 
            "status": "running",
            "connections": 2
        }
    ]
}
```

### 6.4 系统状态接口

**接口路径**: `GET /status`

**请求方式**: HTTP GET

**响应格式**:
```json
{
    "agent": {
        "id": "agent-001",
        "version": "v1.0.0",
        "region": "cn-beijing",
        "status": "running"
    },
    "connections": {
        "active": 7,
        "total": 15
    },
    "resources": {
        "cpu_percent": 15.5,
        "memory_mb": 256,
        "goroutines": 45
    }
}
```

---

## 7. 任务执行接口

### 7.1 执行命令

**接口路径**: `POST /api/task/command`

**请求方式**: HTTP POST

**请求参数**:
```json
{
    "command": "ls -la /tmp",
    "target": {
        "host": "192.168.1.100",
        "port": 22,
        "protocol": "ssh",
        "credentials": {
            "username": "root",
            "password": "password123"
        }
    },
    "options": {
        "timeout": 30,
        "capture_output": true
    }
}
```

### 7.2 执行脚本

**接口路径**: `POST /api/task/script`

**请求方式**: HTTP POST

**请求参数**:
```json
{
    "script_content": "#!/bin/bash\necho 'Hello World'\ndate",
    "script_type": "bash",
    "target": {
        "host": "192.168.1.100",
        "port": 22,
        "protocol": "ssh",
        "credentials": {
            "username": "root",
            "password": "password123"
        }
    }
}
```

### 7.3 查询任务状态

**接口路径**: `GET /api/task/status/:taskId`

**请求方式**: HTTP GET

**响应格式**:
```json
{
    "task_id": "task_1703001234567",
    "status": "completed",
    "start_time": 1703001234567,
    "end_time": 1703001244567,
    "result": {
        "exit_code": 0,
        "stdout": "Hello World\nFri Dec 20 10:30:00 UTC 2024",
        "stderr": ""
    }
}
```

---

## 8. 配置参数说明

### 8.1 通用连接参数

| 参数名 | 类型 | 必填 | 描述 | 默认值 |
|--------|------|------|------|--------|
| hostname | string | 是 | 目标主机地址 | - |
| port | int | 是 | 目标端口 | - |
| username | string | 是 | 用户名 | - |
| password | string | 否 | 密码 | - |

### 8.2 SSH特定参数

| 参数名 | 类型 | 必填 | 描述 | 默认值 |
|--------|------|------|------|--------|
| private-key | string | 否 | 私钥内容 | - |
| passphrase | string | 否 | 私钥密码 | - |
| enable-sftp | boolean | 否 | 启用SFTP | false |
| font-name | string | 否 | 字体名称 | monospace |
| font-size | int | 否 | 字体大小 | 12 |

### 8.3 RDP特定参数

| 参数名 | 类型 | 必填 | 描述 | 默认值 |
|--------|------|------|------|--------|
| domain | string | 否 | 域名 | - |
| width | int | 否 | 屏幕宽度 | 1920 |
| height | int | 否 | 屏幕高度 | 1080 |
| color-depth | int | 否 | 颜色深度 | 32 |
| dpi | int | 否 | DPI设置 | 96 |
| ignore-cert | boolean | 否 | 忽略证书 | true |

### 8.4 VNC特定参数

| 参数名 | 类型 | 必填 | 描述 | 默认值 |
|--------|------|------|------|--------|
| read-only | boolean | 否 | 只读模式 | false |
| swap-red-blue | boolean | 否 | 交换红蓝色 | false |
| cursor | string | 否 | 光标模式 | local |
| encodings | string | 否 | 编码方式 | - |

---

## 9. 错误代码

### 9.1 WebSocket连接错误

| 错误代码 | HTTP状态码 | 描述 |
|---------|-----------|------|
| WS_UPGRADE_FAILED | 400 | WebSocket升级失败 |
| WS_INVALID_REQUEST | 400 | 无效的WebSocket请求 |
| WS_AUTH_FAILED | 401 | WebSocket认证失败 |

### 9.2 协议连接错误

| 错误代码 | 描述 |
|---------|------|
| SSH_CONN_FAILED | SSH连接失败 |
| SSH_AUTH_FAILED | SSH认证失败 |
| SSH_TIMEOUT | SSH连接超时 |
| RDP_CONN_FAILED | RDP连接失败 |
| VNC_CONN_FAILED | VNC连接失败 |
| TELNET_CONN_FAILED | Telnet连接失败 |

### 9.3 任务执行错误

| 错误代码 | 描述 |
|---------|------|
| TASK_CREATE_FAILED | 任务创建失败 |
| TASK_EXEC_FAILED | 任务执行失败 |
| TASK_TIMEOUT | 任务执行超时 |
| TASK_NOT_FOUND | 任务不存在 |

---

## 10. 使用示例

### 10.1 JavaScript WebSocket连接示例

```javascript
// SSH连接示例
function connectSSH() {
    const ws = new WebSocket('ws://localhost:8889/ws/ssh');
    
    ws.onopen = function() {
        const config = {
            target: "192.168.1.100",
            port: 22,
            username: "root",
            password: "password123",
            auth_type: "password",
            cols: 80,
            rows: 24
        };
        ws.send(JSON.stringify(config));
    };
    
    ws.onmessage = function(event) {
        console.log('收到数据:', event.data);
    };
}

// RDP连接示例
function connectRDP() {
    const params = new URLSearchParams({
        hostname: '192.168.1.100',
        port: '3389',
        username: 'Administrator',
        password: 'admin123',
        width: '1920',
        height: '1080'
    });
    
    const ws = new WebSocket(`ws://localhost:8889/api/rdp/websocket?${params}`);
    
    ws.onmessage = function(event) {
        // 处理Guacamole协议消息
        console.log('RDP数据:', event.data);
    };
}
```

### 10.2 Python WebSocket客户端示例

```python
import asyncio
import websockets
import json

async def connect_ssh():
    uri = "ws://localhost:8889/ws/ssh"
    
    async with websockets.connect(uri) as websocket:
        # 发送SSH配置
        config = {
            "target": "192.168.1.100",
            "port": 22,
            "username": "root",
            "password": "password123",
            "auth_type": "password",
            "cols": 80,
            "rows": 24
        }
        
        await websocket.send(json.dumps(config))
        
        # 接收响应
        response = await websocket.recv()
        print(f"连接响应: {response}")

asyncio.run(connect_ssh())
```

---

## 11. 注意事项

### 11.1 安全考虑
- 在生产环境中使用HTTPS/WSS
- 不要在URL中暴露敏感信息
- 实施适当的认证和授权机制
- 定期更新Agent版本

### 11.2 性能考虑
- 合理设置连接池大小
- 监控内存和CPU使用情况
- 实施连接超时和清理机制
- 根据网络情况调整缓冲区大小

### 11.3 兼容性考虑
- 确保客户端支持WebSocket协议
- 注意不同浏览器的WebSocket实现差异
- 考虑网络代理和防火墙的影响

---

## 联系方式

如有疑问或需要技术支持，请联系NewBee开发团队。

**文档最后更新时间**: 2024-12-20 