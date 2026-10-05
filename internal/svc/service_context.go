package svc

import (
    "context"
    "encoding/json"
    "sync"
    "time"

    "github.com/coder-lulu/newbee-proxy/internal/config"
    metrics "github.com/coder-lulu/newbee-proxy/internal/metrics"
    "github.com/coder-lulu/newbee-proxy/internal/types"
    "github.com/coder-lulu/newbee-proxy/internal/storage/sqlite"

    "github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
    Config config.Config

	// 插件管理器
	PluginManager *PluginManager

	// 心跳管理
	HeartbeatManager *HeartbeatManager

	// 任务执行器 - 使用增强版
	TaskExecutor *EnhancedTaskExecutor

    // 监控指标收集器
    MetricsCollector *MetricsCollector

    // 审计
    AuditManager *AuditManager

	// 会话管理器
	SessionManager *SessionManager

	// Telnet会话管理器
	TelnetSessionManager *TelnetSessionManager

    // 连接池管理器
    ConnectionPoolManager *ConnectionPoolManager

    // Ops Center注册
    ProxyReg *ProxyRegistrationManager

    // 日志记录器
    Logger logx.Logger

    // 上下文控制
    Ctx    context.Context
    Cancel context.CancelFunc

    // 运行状态：online, draining, offline
    state   string
    stateMu sync.RWMutex

    // Agent状态
    Status *types.AgentStatus
    mutex  sync.RWMutex

    // 本地存储（可选）
    Store interface{
        Close() error
        RunGC(retentionDays int, maxSizeMB int) (map[string]int, error)
    }
}

func NewServiceContext(c config.Config) *ServiceContext {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logx.WithContext(ctx)

	now := time.Now()
	svcCtx := &ServiceContext{
		Config: c,
		Logger: logger,
		Ctx:    ctx,
        Cancel: cancel,
        state:  "online",
        Status: &types.AgentStatus{
            Status:         "initializing",
            ActiveSessions: 0,
            StartTime:      now,
            Uptime:         0,
        },
	}

    // 初始化各个组件（弃用 gRPC OpsRpc，仅保留 OpsCenter HTTP/PSK）
	svcCtx.PluginManager = NewPluginManager(c.Plugins, logger)
	svcCtx.HeartbeatManager = NewHeartbeatManager(svcCtx)
	svcCtx.TaskExecutor = NewEnhancedTaskExecutor(svcCtx)
    svcCtx.MetricsCollector = NewMetricsCollector(svcCtx)
    svcCtx.AuditManager = NewAuditManager(logger)
	svcCtx.SessionManager = NewSessionManager(c.Limits, logger)
	svcCtx.TelnetSessionManager = NewTelnetSessionManager(logger)
    svcCtx.ConnectionPoolManager = NewConnectionPoolManager(logger)

    // 初始化Ops Center注册
    svcCtx.ProxyReg = NewProxyRegistrationManager(c.OpsCenter, logger, svcCtx)

    // 初始化本地存储（SQLite）
    if c.Storage.EnableSQLite {
        dbPath := c.Storage.DBPath
        if dbPath == "" { dbPath = "data/proxy.db" }
        store, err := sqlite.Open(dbPath)
        if err != nil {
            logger.Errorf("打开本地存储失败，将跳过：%v", err)
        } else {
            logger.Infof("本地存储已启用：%s", dbPath)
            svcCtx.Store = store
        }
    }

    return svcCtx
}

// Start 启动所有服务组件
func (svc *ServiceContext) Start() error {
	svc.Logger.Info("Starting Agent service context...")

	// 更新状态
	svc.updateStatus("starting")

    // 启动插件管理器
    if err := svc.PluginManager.Start(); err != nil {
        return err
    }

    // 启动其他组件
    svc.HeartbeatManager.Start()
    svc.TaskExecutor.Start()
    svc.MetricsCollector.Start()

    // 启动Ops Center注册与心跳
    if svc.ProxyReg != nil {
        svc.ProxyReg.Start()
    }

    // 启动存储 GC（若启用）
    if svc.Store != nil && (svc.Config.Storage.RetentionDays > 0 || svc.Config.Storage.MaxSizeMB > 0) {
        go func() {
            ticker := time.NewTicker(15 * time.Minute)
            defer ticker.Stop()
            // 启动即跑一次 GC
            _, _ = svc.Store.RunGC(svc.Config.Storage.RetentionDays, svc.Config.Storage.MaxSizeMB)
            for {
                select {
                case <-svc.Ctx.Done():
                    return
                case <-ticker.C:
                    if stats, err := svc.Store.RunGC(svc.Config.Storage.RetentionDays, svc.Config.Storage.MaxSizeMB); err != nil {
                        svc.Logger.Errorf("Storage GC error: %v", err)
                    } else if stats["tasks_deleted"]+stats["sessions_deleted"]+stats["files_deleted"] > 0 {
                        svc.Logger.Infof("Storage GC: %+v", stats)
                    }
                }
            }
        }()
    }

    // 启动 Outbox 重放循环（若启用本地存储）
    if svc.Store != nil {
        go svc.replayOutboxLoop()
    }

    // 定时采集 DB 指标（若启用本地存储）
    if s, ok := svc.Store.(*sqlite.Store); ok {
        go func() {
            ticker := time.NewTicker(30 * time.Second)
            defer ticker.Stop()
            for {
                select {
                case <-svc.Ctx.Done():
                    return
                case <-ticker.C:
                    if sz, err := s.DBSizeBytes(); err == nil {
                        metrics.SetDBSizeBytes(sz)
                    }
                    // 统计 outbox pending
                    var n int64
                    if err := s.DB.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE status='pending'`).Scan(&n); err == nil {
                        metrics.SetOutboxPending(n)
                    }
                    // 采样系统/任务/会话指标并写入 metrics_history
                    // 这些值可以来自 SystemMetrics/GetActiveCounts
                    cpu := 0.0
                    mem := 0.0
                    gor := 0
                    if svc.MetricsCollector != nil {
                        // 这里简单留空（若需可引入 system_metrics.GetMetrics）
                    }
                    tasks, sessions := svc.GetActiveCounts()
                    _ = s.InsertMetricsHistory(time.Now(), cpu, mem, gor, tasks, sessions, n, n)
                }
            }
        }()
    }

	// 更新状态为在线
	svc.updateStatus("online")

	svc.Logger.Info("Agent service context started successfully")
	return nil
}

// Stop 停止所有服务组件
func (svc *ServiceContext) Stop() error {
	svc.Logger.Info("Stopping Agent service context...")

	// 更新状态
	svc.updateStatus("stopping")

	// 停止各个组件

	if svc.PluginManager != nil {
		svc.PluginManager.Stop()
	}

	if svc.HeartbeatManager != nil {
		svc.HeartbeatManager.Stop()
	}

	if svc.TaskExecutor != nil {
		svc.TaskExecutor.Stop()
	}

	if svc.MetricsCollector != nil {
		svc.MetricsCollector.Stop()
	}

	if svc.SessionManager != nil {
		svc.SessionManager.Stop()
	}

	if svc.TelnetSessionManager != nil {
		svc.TelnetSessionManager.Close()
	}

    if svc.ConnectionPoolManager != nil {
        svc.ConnectionPoolManager.Stop()
    }

    if svc.ProxyReg != nil {
        svc.ProxyReg.Stop()
    }

    if svc.Store != nil {
        _ = svc.Store.Close()
    }

    // 取消上下文
    svc.Cancel()

	// 更新状态
	svc.updateStatus("stopped")

	svc.Logger.Info("Agent service context stopped")
	return nil
}

// ---------- 高可用状态管理 ----------

// SetState 设置当前运行状态（online/draining/offline）
func (svc *ServiceContext) SetState(state string) {
    svc.stateMu.Lock()
    defer svc.stateMu.Unlock()
    switch state {
    case "online", "draining", "offline":
        svc.state = state
    default:
        svc.Logger.Infof("invalid state %s, keep %s", state, svc.state)
    }
}

// GetState 获取当前状态
func (svc *ServiceContext) GetState() string {
    svc.stateMu.RLock()
    defer svc.stateMu.RUnlock()
    return svc.state
}

// IsAcceptingNew 是否接受新会话/新任务（仅 online 接受）
func (svc *ServiceContext) IsAcceptingNew() bool {
    return svc.GetState() == "online"
}

// GetActiveCounts 返回活跃任务/会话的估计数量
func (svc *ServiceContext) GetActiveCounts() (tasks int, sessions int) {
    if svc.TaskExecutor != nil {
        tasks = svc.TaskExecutor.GetRunningCount()
    }
    if svc.SessionManager != nil {
        sessions += svc.SessionManager.GetActiveSessionCount()
    }
    if svc.TelnetSessionManager != nil {
        sessions += svc.TelnetSessionManager.GetActiveSessionCount()
    }
    return
}

// DrainAndWait 切换 draining 并等待活跃任务/会话清零或超时
func (svc *ServiceContext) DrainAndWait(timeout time.Duration) {
    svc.SetState("draining")
    deadline := time.Now().Add(timeout)
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()
    for {
        tasks, sessions := svc.GetActiveCounts()
        if tasks == 0 && sessions == 0 {
            return
        }
        if time.Now().After(deadline) {
            svc.Logger.Infof("drain timeout reached: tasks=%d sessions=%d", tasks, sessions)
            return
        }
        <-ticker.C
    }
}

// GetStatus 获取Agent状态
func (svc *ServiceContext) GetStatus() *types.AgentStatus {
	svc.mutex.RLock()
	defer svc.mutex.RUnlock()

	// 创建状态副本并计算实时数据
	status := *svc.Status
	status.Uptime = time.Since(svc.Status.StartTime)

    // 更新活跃会话数
    if svc.SessionManager != nil {
        status.ActiveSessions = svc.SessionManager.GetActiveSessionCount()
    }

    // 不再维护连接状态/版本/能力等 agent 元信息

	return &status
}

// updateStatus 更新Agent状态
func (svc *ServiceContext) updateStatus(status string) {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	svc.Status.Status = status
}

// UpdateOPSConnection 更新OPS连接状态
func (svc *ServiceContext) UpdateOPSConnection(connected bool) { /* no-op: removed */ }

// UpdateActiveSessions 更新活跃会话数
func (svc *ServiceContext) UpdateActivesSessions(count int) {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	svc.Status.ActiveSessions = count
}

// UpdateLoadedPlugins 更新已加载插件列表
func (svc *ServiceContext) UpdateLoadedPlugins(plugins []string) { /* no-op: removed */ }

// replayOutboxLoop 周期性重放 outbox 事件（例如任务结果上报），具备指数退避
func (svc *ServiceContext) replayOutboxLoop() {
    type outboxStore interface {
        FetchOutboxBatch(limit int) ([]sqlite.OutboxEvent, error)
        MarkOutboxSent(ids []int64) error
        MarkOutboxFail(id int64, lastErr string, nextRetryAt time.Time) error
    }

    store, ok := svc.Store.(outboxStore)
    if !ok {
        return
    }

    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-svc.Ctx.Done():
            return
        case <-ticker.C:
            batchStart := time.Now()
            // 仅在注册管理器可用时尝试发送
            if svc.ProxyReg == nil || svc.ProxyReg.client == nil {
                continue
            }

            events, err := store.FetchOutboxBatch(100)
            if err != nil {
                svc.Logger.Errorf("Outbox fetch error: %v", err)
                continue
            }
            if len(events) == 0 {
                continue
            }

            var sent []int64
            var failed, retried int
            for _, e := range events {
                switch e.EventType {
                case "task.result":
                    var payload map[string]any
                    if err := json.Unmarshal(e.Payload, &payload); err != nil {
                        _ = store.MarkOutboxFail(e.ID, "unmarshal: "+err.Error(), time.Now().Add(1*time.Minute))
                        failed++
                        continue
                    }
                    if err := svc.ProxyReg.client.ReportTaskResult(payload); err != nil {
                        next := computeBackoff(e.RetryCount + 1)
                        _ = store.MarkOutboxFail(e.ID, err.Error(), time.Now().Add(next))
                        retried++
                        continue
                    }
                    sent = append(sent, e.ID)
                default:
                    // 未知事件类型，直接标记为已处理以防止阻塞
                    sent = append(sent, e.ID)
                }
            }
            if len(sent) > 0 {
                if err := store.MarkOutboxSent(sent); err != nil {
                    svc.Logger.Errorf("Outbox mark sent error: %v", err)
                }
                metrics.AddOutboxReplayed(len(sent))
            }
            if failed > 0 { metrics.AddOutboxFailed(failed) }
            if retried > 0 { metrics.AddOutboxRetry(retried) }
            metrics.ObserveOutboxReplayBatchSeconds(time.Since(batchStart).Seconds())
        }
    }
}

// computeBackoff 指数退避（5s 起步，封顶 5m）
func computeBackoff(retry int) time.Duration {
    if retry < 1 {
        retry = 1
    }
    base := 5 * time.Second
    max := 5 * time.Minute
    // 2^(retry-1) * base，封顶 max
    d := base * time.Duration(1<<uint(retry-1))
    if d > max {
        return max
    }
    return d
}
