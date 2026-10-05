# Newbee Proxy 快速启动指南

## 🚀 最快启动方式

```bash
cd /opt/code/newbee/newbee-proxy
./start-proxy.sh
```

## 📊 当前 Proxy 状态

**Proxy 已在运行中！** ✅

- **Worker ID**: agent-001
- **状态**: online
- **端口**: 8889
- **最后心跳**: 2025-12-18 17:24:56
- **活跃会话**: 0
- **CPU 使用率**: 16.7%
- **内存使用率**: 65.8%

### 验证命令

```bash
# 检查 Proxy 状态
curl http://localhost:8889/status | jq

# 检查健康状态
curl http://localhost:8889/health

# 查看已加载插件
curl http://localhost:8889/plugins

# 查看数据库中的 Proxy 记录（若已接入中心）
mysql -h192.168.26.130 -uroot -p123456 newbee -e \
  "SELECT worker_id, name, worker_status, last_heartbeat FROM ops_workers WHERE worker_id='agent-001';"
```

## 📝 启动方法汇总

### 方法 1: 使用启动脚本（推荐）
```bash
cd /opt/code/newbee/newbee-proxy
./start-proxy.sh etc/proxy.yaml
```

### 方法 2: 直接运行
```bash
cd /opt/code/newbee/newbee-proxy
go build -o bin/proxy ./cmd/proxy && ./bin/proxy -f etc/proxy.yaml
```

### 方法 3: 从源码运行
```bash
cd /opt/code/newbee/newbee-proxy
go run ./cmd/proxy -f etc/proxy.yaml
```

### 方法 4: 后台运行
```bash
cd /opt/code/newbee/newbee-proxy
nohup ./bin/proxy -f etc/proxy.yaml > proxy.log 2>&1 &

# 查看日志
tail -f proxy.log

# 停止
pkill -f "bin/proxy"
```

## 🔍 故障排查

### Proxy 无法启动

```bash
# 检查端口占用
lsof -i :8889

# 杀掉占用进程
kill $(lsof -ti :8889)
```

### Proxy 无法注册到 Ops Center

```bash
# 1. 检查 Ops Center 是否运行
netstat -tuln | grep 9601

# 2. 检查 PSK 是否匹配
# Proxy 配置
grep "PSK:" etc/proxy.yaml

# Ops Center 配置
grep "PSK:" /opt/code/newbee/ops-center/api/etc/ops.yaml

# 3. 检查网络连通性
curl http://127.0.0.1:9601/worker/register
```

### 查看 Worker 日志

如果使用 nohup 后台运行：
```bash
tail -f worker.log
```

如果直接运行，日志会输出到终端。

## 📚 详细文档

完整文档请参考：`docs/PROXY_STARTUP_GUIDE.md`（由原 Worker 指南重命名而来）

## 🔗 相关服务

### 启动 Ops Center（如果未运行）

```bash
cd /opt/code/newbee/ops-center/api
go run ops.go -f etc/ops.yaml
```

### 访问前端 Proxy 管理页面

1. 启动前端：
```bash
cd /opt/code/newbee/ui/apps/web-antd
pnpm dev
```

2. 访问：http://localhost:3100
3. 登录后进入："运维中心" -> "Worker管理"

## ⚙️ 配置文件说明

**位置**: `/opt/code/newbee/newbee-proxy/etc/proxy.yaml`

**关键配置**:
```yaml
# Proxy 身份（必须唯一）
Agent:
  ID: agent-001                    # ⚠️ 多个 Worker 不能相同

# Ops Center 连接（必须配置）
OpsCenter:
  Enabled: true                    # ⚠️ 必须为 true
  Endpoints:
    - "http://127.0.0.1:9601"     # Ops Center API 地址
  PSK: "dev-psk"                   # ⚠️ 必须与 Ops Center 一致

# 监听端口
Port: 8889                         # Proxy HTTP 服务端口
```

## 🎯 快速验证清单

启动 Proxy 后，依次检查：

- [ ] Proxy 进程正在运行
- [ ] 端口 8889 可访问
- [ ] `/status` 返回正常
- [ ] 数据库中有 Worker 记录
- [ ] Worker 状态为 "online"
- [ ] 心跳时间持续更新
- [ ] 前端页面能看到 Worker

### 一键验证脚本

```bash
#!/bin/bash
echo "=== Proxy 验证 ==="
echo ""

echo "1. 检查进程:"
ps aux | grep "bin/proxy" | grep -v grep

echo ""
echo "2. 检查端口:"
netstat -tuln | grep 8889

echo ""
echo "3. 检查状态:"
curl -s http://localhost:8889/status | jq -r '.status, .id'

echo ""
echo "4. 检查数据库:"
mysql -h192.168.26.130 -uroot -p123456 newbee -e \
  "SELECT worker_id, worker_status, last_heartbeat FROM ops_workers WHERE worker_id='agent-001';" 2>/dev/null

echo ""
echo "=== 验证完成 ==="
```

保存为 `verify-worker.sh` 并运行：
```bash
chmod +x verify-worker.sh
./verify-worker.sh
```

---

**提示**: 如需部署多个 Worker，请修改 `Agent.ID`、`Port` 和 `Network.LocalIP` 等配置，确保不冲突。
