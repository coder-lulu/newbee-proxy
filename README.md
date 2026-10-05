# 新蜂资产管理平台 — 远程执行代理

位于受管网络侧的协议代理与执行节点，向运维中心注册和上报心跳，为远程连接、插件执行及统一 I/O Provider 提供网络侧执行能力。与部署在单台主机上的 nb-agent 分工不同。

仓库：[coder-lulu/newbee-proxy](https://github.com/coder-lulu/newbee-proxy) · [平台工作区](https://github.com/coder-lulu/newbee)

## 获取代码

推荐通过完整工作区开发，保留兄弟模块目录及本地 `replace` 依赖。以下命令使用 Bash；Go 工作区要求 Go 1.25.1 或更高版本。

```bash
git clone --recurse-submodules https://github.com/coder-lulu/newbee.git
cd newbee/newbee-proxy
```

已有工作区执行 `git submodule update --init --recursive`。单独克隆模块时，需要自行补齐 `go.mod` 中的本地依赖路径。

## 目录导航

| 目录 | 用途 |
| --- | --- |
| `cmd/proxy.go` | 当前主入口 |
| `internal/` | 服务装配、执行器、中心连接和 HTTP 接口 |
| `plugins/` | SSH、RDP 等协议插件 |
| `provider/` | Provider 适配 |
| `etc/proxy.yaml.example` | 配置模板 |
| `test/`、`tests/` | 测试与联调资料 |

## 配置与运行

首次复制 `etc/proxy.yaml.example` 为 `etc/proxy.yaml`，设置 `OpsCenter`、`Security`、`Plugins`、`Network`、`Limits` 和 `Storage`。配置中的接入凭据需与运维中心匹配。示例 HTTP 端口为 `9001`，以配置为准。

启用 SQLite 本地存储时，`Storage.DBPath` 指定的数据库（默认 `data/proxy.db`）由启动逻辑自动创建和迁移，不随源码分发。它仅保存代理任务、会话和 outbox，不替代平台的 MySQL 主数据库。

当前入口使用 `conf.Load`，未启用环境变量展开；启动前必须将 `${...}` 占位符替换成实际本地值，不能仅通过 export 配置。密钥及真实运行配置不要提交。

```bash
go run ./cmd/proxy.go -f etc/proxy.yaml
```

构建与检查：

```bash
mkdir -p bin
go build -o bin/proxy ./cmd/proxy.go
go test ./...
go vet ./...
```

配置就绪后可运行 `./bin/proxy -f etc/proxy.yaml`。HTTP 入口包含 `/health`、`/status`；按安全配置设置访问凭据后检查，并从运维中心确认 Worker 注册和心跳。入口当前使用 HTTP/WebSocket 与 Ops Center 通信；不要将历史 gRPC 控制面说明作为当前启动方式。

## 相关资料

- [SSH/RDP 公共组件](plugins/common/README.md)
- [RDP 插件](plugins/rdp/README.md)
- [运维中心仓库](https://github.com/coder-lulu/newbee-ops)
- [统一 I/O 公共接口](https://github.com/coder-lulu/newbee-io)

历史快速启动和集成文档包含旧目录、端口及示例运行状态，当前入口与配置方式以本页和代码为准。

## 许可证与来源

本仓库采用 [MIT](LICENSE)。第三方依赖遵循各自许可证，保留原有版权与许可声明。
