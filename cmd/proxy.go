package main

// 重命名后的入口（由 cmd/agent/main.go 迁移而来），逻辑保持一致，仅品牌/文案/默认配置名更新

import (
    "context"
    "encoding/json"
    "flag"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/coder-lulu/newbee-proxy/internal/config"
    "github.com/coder-lulu/newbee-proxy/internal/handlers"
    mctx "github.com/coder-lulu/newbee-proxy/internal/middleware"
    metrics "github.com/coder-lulu/newbee-proxy/internal/metrics"
    "github.com/coder-lulu/newbee-proxy/internal/svc"
    "github.com/coder-lulu/newbee-proxy/internal/types"

    "github.com/zeromicro/go-zero/core/conf"
    "github.com/zeromicro/go-zero/core/logx"
    "github.com/zeromicro/go-zero/rest"
    promhttp "github.com/prometheus/client_golang/prometheus/promhttp"
    _ "net/http/pprof"
)

var configFile = flag.String("f", "etc/proxy.yaml", "the config file")

func main() {
    flag.Parse()

    var c config.Config
    if err := conf.Load(*configFile, &c); err != nil {
        // 不再保留 etc/agent.yaml 回退
        logx.Errorf("Failed to load config: %v", err)
        os.Exit(1)
    }

    if err := logx.SetUp(c.Log); err != nil {
        logx.Errorf("Failed to setup log: %v", err)
        os.Exit(1)
    }
    defer logx.Close()

    logx.Infof("Starting Newbee Proxy")

    svcCtx := svc.NewServiceContext(c)
    if err := svcCtx.Start(); err != nil {
        logx.Errorf("Failed to start service context: %v", err)
        os.Exit(1)
    }

    // 仅保留 HTTP/WS/OpsCenter 心跳（已弃用 gRPC 控制面）

    server := rest.MustNewServer(c.RestConf)
    // 安全中间件
    mctx.RegisterSecurity(server, svcCtx)

    // 健康/状态/指标/pprof/插件
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/health", Handler: healthCheckHandler(svcCtx)})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/status", Handler: statusHandler(svcCtx)})
    metrics.RegisterPrometheus()
    metrics.RegisterHTTPMetrics()
    metrics.RegisterPortFwdMetrics()
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/metrics", Handler: func(w http.ResponseWriter, r *http.Request) { promhttp.Handler().ServeHTTP(w, r) }})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/metrics.json", Handler: metricsHandler(svcCtx)})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/debug/pprof/", Handler: http.DefaultServeMux.ServeHTTP})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/plugins", Handler: pluginsHandler(svcCtx)})

    // 管理接口（PSK 保护）
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/admin/state", Handler: handlers.AdminGetStateHandler(svcCtx)})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/admin/state", Handler: handlers.AdminSetStateHandler(svcCtx)})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/admin/drain", Handler: handlers.AdminSetDrain(svcCtx)})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/admin/online", Handler: handlers.AdminSetOnline(svcCtx)})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/admin/shutdown", Handler: handlers.AdminShutdown(svcCtx)})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/admin/storage/stats", Handler: handlers.AdminGetStorageStatsHandler(svcCtx)})

    // 流式任务注入（辅助）
    // 已去除基于 gRPC 的注入辅助端点

    // WS/Guacamole/任务/DB/HTTP/PortFwd 路由
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/ws/ssh", Handler: handlers.WebSocketTunnelHandler(svcCtx)})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/ws/telnet", Handler: handlers.WebSocketTelnetTunnelHandler(svcCtx)})
    gh := handlers.NewGuacamoleWebSocketHandler(svcCtx)
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/rdp/websocket", Handler: gh.HandleGuacamoleWebSocket})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/ssh/websocket", Handler: gh.HandleGuacamoleWebSocket})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/vnc/websocket", Handler: gh.HandleGuacamoleWebSocket})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/telnet/websocket", Handler: gh.HandleGuacamoleWebSocket})

    th := handlers.NewTaskHandler(svcCtx)
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/task/command", Handler: th.ExecuteCommand})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/task/script", Handler: th.ExecuteScript})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/task/file", Handler: th.ExecuteFileTransfer})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/task/http", Handler: th.ExecuteHTTP})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/task/cancel", Handler: th.CancelTask})
    server.AddRoute(rest.Route{Method: http.MethodGet,  Path: "/api/task/status/:taskId", Handler: th.GetTaskStatus})
    server.AddRoute(rest.Route{Method: http.MethodGet,  Path: "/api/task/result/:taskId", Handler: th.GetTaskResult})
    server.AddRoute(rest.Route{Method: http.MethodGet,  Path: "/api/task/active", Handler: th.GetActiveTasks})
    server.AddRoute(rest.Route{Method: http.MethodGet,  Path: "/api/task/stats", Handler: th.GetTaskStats})

    db := handlers.NewDbHandler(svcCtx)
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/db/test", Handler: db.TestConnection})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/db/connect", Handler: db.CreateConnection})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/db/execute", Handler: db.ExecuteSQL})
    server.AddRoute(rest.Route{Method: http.MethodGet,  Path: "/api/db/databases", Handler: db.GetDatabases})
    server.AddRoute(rest.Route{Method: http.MethodGet,  Path: "/api/db/tables", Handler: db.GetTables})
    server.AddRoute(rest.Route{Method: http.MethodGet,  Path: "/api/db/table/info", Handler: db.GetTableInfo})
    server.AddRoute(rest.Route{Method: http.MethodDelete,Path: "/api/db/disconnect", Handler: db.CloseConnection})
    server.AddRoute(rest.Route{Method: http.MethodGet,  Path: "/api/db/stats", Handler: db.GetConnectionStats})
    server.AddRoute(rest.Route{Method: http.MethodGet,  Path: "/api/db/websocket", Handler: db.HandleWebSocket})

    // HTTP 同步请求
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/http/request", Handler: handlers.HTTPRequestHandler(svcCtx)})

    // Port Forward 管理接口
    pfh := handlers.NewPortFwdHandler(svcCtx)
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/portfwd/rules", Handler: pfh.CreateRule})
    server.AddRoute(rest.Route{Method: http.MethodDelete, Path: "/api/portfwd/rules/:id", Handler: pfh.DeleteRule})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/portfwd/rules/:id/start", Handler: pfh.StartRule})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/portfwd/rules/:id/stop", Handler: pfh.StopRule})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/portfwd/rules", Handler: pfh.ListRules})

    // SNMP 接口
    snmph := handlers.NewSNMPHandler(svcCtx)
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/snmp/get", Handler: snmph.Get})
    server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/snmp/walk", Handler: snmph.Walk})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/snmp/ping", Handler: snmph.Ping})

    // 诊断接口（网络连通性/DNS/TLS/ICMP/SNMP 快测）
    dh := handlers.NewDiagHandler(svcCtx)
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/diag/tcp", Handler: dh.TCP})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/diag/dns", Handler: dh.DNS})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/diag/tls", Handler: dh.TLS})
    server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/diag/icmp", Handler: dh.ICMP})
    logx.Infof("Newbee Proxy listening on port %d", c.Port)

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    go server.Start()

    select {
    case <-quit:
        logx.Info("Received shutdown signal")
    }

    logx.Info("Shutting down Proxy...")
    svcCtx.SetState("draining")
    svcCtx.DrainAndWait(15 * time.Second)

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    done := make(chan struct{})
    go func() {
        defer close(done)
        server.Stop()
        if err := svcCtx.Stop(); err != nil { logx.Errorf("Failed to stop service context: %v", err) }
    }()
    select {
    case <-done:
        logx.Info("Graceful shutdown completed")
    case <-shutdownCtx.Done():
        logx.Info("Shutdown timeout reached, forcing exit")
        os.Exit(1)
    }
}

// 以下处理器与原 agent/main.go 相同（简化复制）
func healthCheckHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var status *types.AgentStatus
        defer func() { if r := recover(); r != nil { logx.Errorf("Health check panic: %v", r); http.Error(w, "Internal server error", 500) } }()
        if svcCtx != nil { status = svcCtx.GetStatus() }
        if status == nil { _ = json.NewEncoder(w).Encode(map[string]any{"status":"ok"}); return }
        _ = json.NewEncoder(w).Encode(status)
    }
}

func statusHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(svcCtx.GetStatus()) }
}

func metricsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // 任务快照
        tasksRunning, sessions := svcCtx.GetActiveCounts()
        taskSnap := map[string]any{
            "running": tasksRunning,
            "queue_len": 0,
        }
        // 尝试读取内部队列长度（不可见直接保留0）

        // 系统快照（使用 ProxyRegistrationManager 的 SystemMetrics）
        // 系统快照（独立采样，避免访问未导出的字段）
        sys := map[string]any{}
        sm := metrics.NewSystemMetrics()
        if err := sm.Collect(); err == nil {
            m := sm.GetMetrics()
            sys = map[string]any{
                "cpu":          m.CPUUsage,
                "mem":          m.MemoryUsage,
                "disk":         m.DiskUsage,
                "net_in_bps":   m.NetworkInDelta,
                "net_out_bps":  m.NetworkOutDelta,
                "goroutines":   m.GoRoutines,
            }
        }

        // 插件状态/指标快照（轻量，不做深处理）
        var plugStatus map[string]any
        var plugMetrics map[string]any
        if svcCtx != nil && svcCtx.PluginManager != nil {
            plugStatus = map[string]any{}
            plugMetrics = map[string]any{}
            for _, name := range svcCtx.PluginManager.GetLoadedPlugins() {
                if st, err := svcCtx.PluginManager.GetPluginStatus(name); err == nil { plugStatus[name] = st }
                if m, err := svcCtx.PluginManager.GetPluginMetrics(name); err == nil { plugMetrics[name] = m }
            }
        }

        snap := map[string]any{
            "ws":       metrics.GetWSMetrics(),
            "tasks":    taskSnap,
            "sessions": sessions,
            "system":   sys,
            "plugins":  plugStatus,
            "plugin_metrics": plugMetrics,
            // 预留：SNMP/PortFwd/DB 快照可在此聚合（当前 Prom 已覆盖）
            "ts": time.Now().Unix(),
        }
        _ = json.NewEncoder(w).Encode(snap)
    }
}

func pluginsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if svcCtx == nil || svcCtx.PluginManager == nil {
            _ = json.NewEncoder(w).Encode([]string{})
            return
        }
        _ = json.NewEncoder(w).Encode(svcCtx.PluginManager.GetLoadedPlugins())
    }
}
