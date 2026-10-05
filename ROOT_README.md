# NewBee Agent - 企业级远程连接代理服务

> **Version**: 1.0.0  
> **Last Update**: 2024-12-19  
> **Author**: @newbee-team  

## 项目框架

NewBee Agent 是一个企业级的远程连接代理服务，基于 Go 语言开发，采用插件化架构设计，支持多种远程连接协议。

### 核心架构
- **微服务架构**：基于 go-zero 框架构建的高性能微服务
- **插件化设计**：支持动态加载和卸载协议插件
- **WebSocket 桥接**：提供实时双向通信能力
- **连接池管理**：高效的连接复用和资源管理
- **监控和指标**：完整的性能监控和健康检查

### 技术栈
- **框架**：go-zero v1.7.3
- **协议支持**：SSH、Telnet、RDP、VNC、IPMI、SNMP
- **WebSocket**：gorilla/websocket
- **数据库**：支持 MySQL、PostgreSQL、SQLite
- **日志**：结构化日志记录
- **配置**：YAML 配置文件

## 核心功能（含版本号）

### 1. 协议插件系统 (v1.0.0)
- **SSH 插件**：支持密码和密钥认证，终端会话管理
- **RDP 插件**：基于 Guacamole 的 RDP 连接代理
- **Telnet 插件**：传统 Telnet 协议支持
- **DB 插件**：数据库连接和查询执行
- **VNC 插件**：VNC 远程桌面支持（通过 Guacamole）

### 2. WebSocket 隧道服务 (v1.0.0)
- 统一的 WebSocket 接口
- 多协议支持的单一入口
- 实时数据传输和会话管理
- 自动重连和错误恢复

### 3. 连接管理系统 (v1.0.0)
- 连接池管理和复用
- 会话生命周期管理
- 超时和资源清理
- 并发连接限制

### 4. 监控和健康检查 (v1.0.0)
- 实时状态监控
- 性能指标收集
- 健康检查接口
- 自动故障恢复

### 5. 配置管理 (v1.0.0)
- 热重载配置
- 环境变量支持
- 安全配置管理
- 插件配置隔离

## 已知问题（按优先级排序）

### 高优先级
1. **并发安全性**：部分插件在高并发场景下可能存在数据竞争
2. **内存泄漏**：长时间运行可能存在连接未正确释放的问题
3. **错误处理**：某些异常情况下的错误处理不够完善

### 中优先级
1. **性能优化**：连接池大小和超时配置需要根据实际场景调优
2. **日志优化**：日志级别和输出格式需要进一步标准化
3. **配置验证**：启动时的配置验证不够严格

### 低优先级
1. **文档完善**：API 文档和部署文档需要补充
2. **测试覆盖**：单元测试和集成测试覆盖率需要提升
3. **监控增强**：需要更多的业务指标和告警机制

## 文档索引

### 设计文档
- [架构设计](doc/design/architecture.md) - 系统整体架构设计
- [插件设计](doc/design/plugin_architecture.md) - 插件系统设计
- [安全设计](doc/design/security.md) - 安全机制设计

### API 文档
- [HTTP API](doc/api/http_api.md) - REST API 接口文档
- [WebSocket API](doc/api/websocket_api.md) - WebSocket 接口文档
- [插件 API](doc/api/plugin_api.md) - 插件开发接口

### 运维文档
- [部署指南](doc/ops/deployment.md) - 部署和配置指南
- [监控指南](doc/ops/monitoring.md) - 监控和告警配置
- [故障排查](doc/ops/troubleshooting.md) - 常见问题和解决方案

### 开发文档
- [开发指南](doc/dev/development.md) - 开发环境搭建和规范
- [插件开发](doc/dev/plugin_development.md) - 插件开发指南
- [测试指南](doc/dev/testing.md) - 测试规范和工具

### 变更日志
- [更新日志](doc/CHANGELOG.md) - 版本更新记录
- [迁移指南](doc/MIGRATION.md) - 版本迁移指南

## 快速开始

### 环境要求
- Go 1.23.0+
- 支持的操作系统：Linux、Windows、macOS
- 内存：最少 512MB，推荐 2GB+
- CPU：最少 1 核，推荐 2 核+

### 安装和运行
```bash
# 克隆项目
git clone <repository-url>
cd agent

# 安装依赖
go mod tidy

# 配置文件
cp etc/proxy.yaml.example etc/proxy.yaml
# 编辑配置文件...

# 启动服务
go run ./cmd/proxy -f etc/proxy.yaml
```

### 验证安装
```bash
# 健康检查
curl http://localhost:8889/health

# 查看状态
curl http://localhost:8889/status

# 查看插件
curl http://localhost:8889/plugins
```

## 联系方式

- **项目维护**：NewBee Team
- **技术支持**：通过 Issue 提交问题
- **文档贡献**：欢迎提交 PR 完善文档

---

**注意**：本项目遵循企业级开发规范，所有代码变更必须经过代码审查，所有功能必须有对应的测试用例。 
