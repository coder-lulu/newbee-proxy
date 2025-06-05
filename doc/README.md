# 🚀 企业级运维Agent服务

企业级边缘运维管理Agent，提供WebSocket SSH隧道、一次性命令执行、脚本分发等核心功能。

## ✨ 核心特性

### 🔒 WebSocket SSH隧道
- **实时终端访问**：基于WebSocket的SSH连接，支持全屏终端操作
- **编辑器支持**：完美支持vi/nano等全屏编辑器
- **并发写入保护**：解决WebSocket并发写入问题，连接稳定可靠
- **ANSI序列处理**：正确处理终端控制序列，避免JavaScript解析错误

### ⚡ 企业级任务执行系统（2025-05-31 重大更新）

#### 🎯 并发控制与性能保障
- **智能并发管理**：支持最大50个并发任务，超出限制自动排队重试
- **资源保护机制**：防止系统资源耗尽，确保服务稳定性
- **任务队列优化**：1000个任务缓冲区，支持大规模批量操作
- **性能监控**：实时统计任务执行情况和系统资源使用

#### 💾 结果持久化与内存管理
- **文件存储架构**：任务结果自动保存到本地文件系统
- **按日期组织**：结果文件按日期分目录存储，便于管理和查找
- **内存优化**：TaskInfo只保留基本信息，详细结果存储在文件中
- **智能清理**：30分钟后自动清理内存中的任务信息，定时清理过期文件

#### 🔄 OPS服务集成
- **异步结果回传**：独立结果发送队列，不阻塞任务执行
- **重试机制**：3次重试确保结果可靠传递给OPS服务
- **容错设计**：OPS服务不可用时，结果安全保存在本地

#### 📊 企业级监控
- **系统统计API**：实时获取任务执行统计、性能指标
- **状态分析**：按任务类型、状态进行分类统计
- **内存使用监控**：跟踪内存使用情况，避免内存泄漏

### 🛠 多协议命令与脚本执行

#### 🔐 SSH协议支持（Linux/Unix服务器）
- **基于golang.org/x/crypto/ssh**：可靠的SSH连接实现
- **SFTP文件传输**：脚本自动上传、执行、清理
- **多种认证方式**：密码认证、私钥认证
- **权限控制**：支持sudo执行、环境变量设置

#### 🌐 Telnet协议支持（网络设备）
- **网络设备专用**：针对路由器、交换机等网络设备优化
- **状态机认证**：智能识别login、password、命令提示符
- **协议字符处理**：自动过滤IAC等Telnet控制字符
- **脚本逐行执行**：适配Telnet协议限制的脚本执行方式

#### 🖥️ RDP协议支持（Windows服务器） 🆕
- **Windows远程桌面**：支持Windows服务器的RDP连接
- **图形界面访问**：通过WebSocket实现Web端远程桌面
- **命令执行**：PowerShell、CMD命令执行支持
- **多脚本类型**：PowerShell、批处理、Python脚本执行
- **实时交互**：鼠标、键盘事件的实时处理

#### ⚡ 统一超时控制机制
- **连接超时**：SSH 15秒，Telnet 10秒，RDP 30秒建立连接
- **命令超时**：精确的命令执行时间控制
- **脚本超时**：文件传输+执行的总时间管理
- **优雅终止**：SIGTERM/SIGKILL信号机制，正确清理资源

## 📋 API接口

### 基础服务接口
- `GET /health` - 健康检查
- `GET /status` - Agent状态信息
- `GET /metrics` - 监控指标
- `GET /plugins` - 插件状态

### WebSocket隧道接口
- `GET /ws/ssh` - WebSocket SSH隧道连接
- `POST /api/ssh/resize` - 终端大小调整
- `GET /api/rdp/websocket` - RDP WebSocket连接 🆕

### 任务执行接口
- `POST /api/task/command` - 执行命令
- `POST /api/task/script` - 执行脚本
- `GET /api/task/status/:taskId` - 获取任务状态
- `GET /api/task/result/:taskId` - 获取任务执行结果 🆕
- `GET /api/task/active` - 获取活跃任务列表
- `GET /api/task/stats` - 获取系统统计信息 🆕

## 🏗 系统架构

### 任务执行流程
1. **任务接收**：HTTP API接收任务请求
2. **并发控制**：信号量控制同时执行任务数量
3. **任务执行**：SSH执行器处理具体任务
4. **结果存储**：JSON格式保存到文件系统
5. **状态更新**：更新内存中的任务状态
6. **结果回传**：异步发送结果到OPS服务

### 存储结构
```
task_results/
├── 2025-05-31/
│   ├── cmd_1748632449108132500.json
│   ├── script_1748632449179861500.json
│   └── ...
├── 2025-06-01/
│   └── ...
```

### 内存管理策略
- **TaskInfo轻量化**：仅保存任务ID、状态、时间等基本信息
- **定时清理**：每10分钟清理过期任务，30分钟后删除内存记录
- **文件缓存**：详细结果存储在文件中，按需读取

## 🚀 快速开始

### 启动服务
```bash
./agent.exe -f etc/agent.yaml
```

### 执行命令示例

#### SSH命令执行
```bash
curl -X POST http://localhost:8889/api/task/command \
  -H "Content-Type: application/json" \
  -d '{
    "target": "192.168.1.100",
    "protocol": "ssh",
    "username": "root", 
    "password": "password",
    "command": "ls -la /home"
  }'
```

#### Telnet命令执行
```bash
curl -X POST http://localhost:8889/api/task/command \
  -H "Content-Type: application/json" \
  -d '{
    "target": "192.168.1.1",
    "protocol": "telnet",
    "username": "admin",
    "password": "admin123",
    "command": "show version"
  }'
```

#### RDP命令执行 🆕
```bash
curl -X POST http://localhost:8889/api/task/command \
  -H "Content-Type: application/json" \
  -d '{
    "target": "192.168.1.200",
    "protocol": "rdp",
    "username": "Administrator",
    "password": "Password123",
    "command": "Get-Process | Select-Object -First 10"
  }'
```

### 执行脚本示例
```bash
curl -X POST http://localhost:8889/api/task/script \
  -H "Content-Type: application/json" \
  -d '{
    "target": "192.168.1.100",
    "username": "root",
    "password": "password", 
    "script_content": "#!/bin/bash\necho \"Hello World\"\ndate",
    "script_type": "bash"
  }'
```

### 获取任务结果
```bash
# 获取任务状态
curl http://localhost:8889/api/task/status/cmd_1748632449108132500

# 获取详细结果
curl http://localhost:8889/api/task/result/cmd_1748632449108132500

# 获取系统统计
curl http://localhost:8889/api/task/stats
```

## 🧪 测试验证

### 基础功能测试
```bash
# 命令执行测试
./test_execute_command.bat

# 任务状态查询测试  
./test_task_status.bat

# 任务结果获取测试
./test_task_result.bat
```

### 企业级功能测试
```bash
# 综合功能测试（批量任务、持久化、内存管理）
./test_enhanced_features.bat
```

## 🔧 配置说明

### agent.yaml配置
```yaml
Agent:
  ID: "agent-001"
  Version: "v1.2.0"
  Name: "Enterprise-Agent"

RestConf:
  Port: 8889
  Host: "0.0.0.0"

Log:
  Level: "info"
  Encoding: "json"
```

### 性能参数
- **最大并发任务**：50个
- **任务队列大小**：1000个
- **结果发送队列**：500个
- **内存清理间隔**：10分钟
- **任务保留时间**：30分钟

## 💡 生产环境建议

### 可靠性保障
1. **监控告警**：使用`/api/task/stats`监控系统状态
2. **日志收集**：收集Agent日志用于问题排查
3. **存储管理**：定期清理过期的结果文件
4. **网络容错**：确保与OPS服务的网络连接稳定

### 性能优化
1. **合理批量**：避免同时提交过多任务
2. **超时设置**：根据实际情况设置合理的任务超时
3. **存储监控**：监控磁盘空间，防止结果文件过多
4. **并发调优**：根据服务器资源调整最大并发数

## 📈 版本历史

### v1.2.0 (2025-05-31) 🎉 企业级升级
- ✅ **并发控制**：智能任务并发管理，支持大规模批量操作
- ✅ **结果持久化**：文件存储系统，解决内存泄漏问题
- ✅ **内存优化**：轻量化TaskInfo结构，30分钟自动清理
- ✅ **OPS集成**：异步结果回传，3次重试机制
- ✅ **企业监控**：系统统计API，性能指标监控
- ✅ **API增强**：新增结果查询、统计接口

### v1.1.0 (2025-05-30)
- ✅ **命令执行**：SSH命令执行，支持sudo和环境变量
- ✅ **脚本执行**：SFTP上传+执行，支持多种脚本类型
- ✅ **任务管理**：任务队列、状态跟踪、超时控制

### v1.0.0 (2025-05-29)  
- ✅ **WebSocket SSH隧道**：解决并发写入问题
- ✅ **终端支持**：完美支持vi/nano编辑器
- ✅ **基础API**：健康检查、状态监控接口

## 🤝 技术支持

这是一个企业级的Agent服务，已在生产环境中验证。如有问题，请检查日志文件或联系技术支持。 

## 🚀 核心功能

### 1. 命令执行功能

支持通过HTTP API在远程主机上执行命令，支持多种协议：

#### 支持的协议
- **SSH协议** (端口22)：完整的SSH协议支持，包括密码和密钥认证
- **Telnet协议** (端口23)：适用于网络设备和传统系统的Telnet连接

**API接口**: `POST /api/task/command`

**请求示例**:
```json
{
    "target": "192.168.1.100",
    "port": 22,
    "protocol": "ssh",
    "username": "admin", 
    "password": "password",
    "command": "ls -la /home",
    "working_dir": "/tmp",
    "timeout": 30,
    "use_sudo": false,
    "environment": {
        "PATH": "/usr/local/bin:/usr/bin:/bin"
    }
}
```

**Telnet协议示例**:
```json
{
    "target": "192.168.1.1",
    "port": 23,
    "protocol": "telnet",
    "username": "admin",
    "password": "admin123",
    "command": "show version",
    "timeout": 30
}
```

**响应示例**:
```json
{
    "success": true,
    "task_id": "cmd_1748632449108132500",
    "message": "任务已提交"
}
```

### 2. 脚本执行功能

支持多种脚本类型的执行，包括批处理脚本和逐行执行：

#### 协议支持
- **SSH协议**：上传脚本文件到远程主机执行，支持文件清理
- **Telnet协议**：逐行发送脚本内容执行，适用于网络设备配置

**API接口**: `POST /api/task/script`

**SSH脚本执行示例**:
```json
{
    "target": "192.168.1.100",
    "port": 22,
    "protocol": "ssh",
    "username": "root",
    "password": "password",
    "script_content": "#!/bin/bash\necho \"Hello World\"\ndate\nwhoami",
    "script_type": "bash",
    "cleanup_after": true,
    "timeout": 60
}
```

**Telnet脚本执行示例** (网络设备配置):
```json
{
    "target": "192.168.1.1",
    "port": 23,
    "protocol": "telnet",
    "username": "admin",
    "password": "admin123",
    "script_content": "show version\nshow interface status\nshow ip route",
    "script_type": "cisco",
    "timeout": 60
}
```

#### Telnet协议特性
- **逐行执行**：脚本内容按行分割，逐条发送到远程设备
- **即时响应**：每行命令执行后立即获取输出
- **网络设备兼容**：适配Cisco、华为等网络设备的命令格式
- **协议清理**：自动过滤Telnet协议字符和控制序列
- **错误容错**：单行失败不影响后续命令执行
- **智能超时控制**：🆕 分层超时管理，精确控制执行时间
  - **连接超时**：最大10秒建立连接，无效主机快速失败
  - **认证超时**：最大30秒完成登录认证
  - **命令超时**：可配置单命令执行时间限制
  - **脚本总超时**：整体脚本执行时间控制，防止长时间阻塞
  - **动态分配**：脚本中每行命令合理分配时间，确保公平执行 