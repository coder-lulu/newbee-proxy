package svc

import (
	"context"
	"sync"
	"time"

	"newbee-agent/internal/client"
	"newbee-agent/internal/config"
	"newbee-agent/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config config.Config

	// OPS客户端
	OpsClient *client.OpsClient

	// 插件管理器
	PluginManager *PluginManager

	// 心跳管理
	HeartbeatManager *HeartbeatManager

	// 任务执行器 - 使用增强版
	TaskExecutor *EnhancedTaskExecutor

	// 监控指标收集器
	MetricsCollector *MetricsCollector

	// 会话管理器
	SessionManager *SessionManager

	// Telnet会话管理器
	TelnetSessionManager *TelnetSessionManager

	// 连接池管理器
	ConnectionPoolManager *ConnectionPoolManager

	// 日志记录器
	Logger logx.Logger

	// 上下文控制
	Ctx    context.Context
	Cancel context.CancelFunc

	// Agent状态
	Status *types.AgentStatus
	mutex  sync.RWMutex
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
		Status: &types.AgentStatus{
			ID:             c.Agent.ID,
			Status:         "initializing",
			Version:        c.Agent.Version,
			Region:         c.Agent.Region,
			Capabilities:   c.Agent.Capabilities,
			ConnectedToOPS: false,
			ActiveSessions: 0,
			LoadedPlugins:  []string{},
			Metadata:       make(map[string]string),
			StartTime:      now,
			Uptime:         0,
			LastHeartbeat:  now,
		},
	}

	// 初始化各个组件
	svcCtx.OpsClient = client.NewOpsClient(&c)
	svcCtx.PluginManager = NewPluginManager(c.Plugins, logger)
	svcCtx.HeartbeatManager = NewHeartbeatManager(svcCtx)
	svcCtx.TaskExecutor = NewEnhancedTaskExecutor(svcCtx)
	svcCtx.MetricsCollector = NewMetricsCollector(svcCtx)
	svcCtx.SessionManager = NewSessionManager(c.Limits, logger)
	svcCtx.TelnetSessionManager = NewTelnetSessionManager(logger)
	svcCtx.ConnectionPoolManager = NewConnectionPoolManager(logger)

	return svcCtx
}

// Start 启动所有服务组件
func (svc *ServiceContext) Start() error {
	svc.Logger.Info("Starting Agent service context...")

	// 更新状态
	svc.updateStatus("starting")

	// 启动OPS客户端
	if err := svc.OpsClient.Start(); err != nil {
		return err
	}

	// 启动插件管理器
	if err := svc.PluginManager.Start(); err != nil {
		return err
	}

	// 启动其他组件
	svc.HeartbeatManager.Start()
	svc.TaskExecutor.Start()
	svc.MetricsCollector.Start()

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
	if svc.OpsClient != nil {
		svc.OpsClient.Stop()
	}

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

	// 取消上下文
	svc.Cancel()

	// 更新状态
	svc.updateStatus("stopped")

	svc.Logger.Info("Agent service context stopped")
	return nil
}

// GetStatus 获取Agent状态
func (svc *ServiceContext) GetStatus() *types.AgentStatus {
	svc.mutex.RLock()
	defer svc.mutex.RUnlock()

	// 创建状态副本并计算实时数据
	status := *svc.Status
	status.Uptime = time.Since(svc.Status.StartTime)

	// 更新插件列表
	if svc.PluginManager != nil {
		status.LoadedPlugins = svc.PluginManager.GetLoadedPlugins()
	}

	// 更新活跃会话数
	if svc.SessionManager != nil {
		status.ActiveSessions = svc.SessionManager.GetActiveSessionCount()
	}

	// 更新OPS连接状态
	if svc.OpsClient != nil {
		status.ConnectedToOPS = svc.OpsClient.IsConnected()
	}

	return &status
}

// updateStatus 更新Agent状态
func (svc *ServiceContext) updateStatus(status string) {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	svc.Status.Status = status
}

// UpdateOPSConnection 更新OPS连接状态
func (svc *ServiceContext) UpdateOPSConnection(connected bool) {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	svc.Status.ConnectedToOPS = connected
}

// UpdateActiveSessions 更新活跃会话数
func (svc *ServiceContext) UpdateActivesSessions(count int) {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	svc.Status.ActiveSessions = count
}

// UpdateLoadedPlugins 更新已加载插件列表
func (svc *ServiceContext) UpdateLoadedPlugins(plugins []string) {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	svc.Status.LoadedPlugins = plugins
}
