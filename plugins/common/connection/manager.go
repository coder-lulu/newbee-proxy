package connection

import (
	"context"
	"fmt"
	"sync"
	"time"

    "github.com/coder-lulu/newbee-proxy/plugins/common/timeout"
)

// ConnectionManager 通用连接管理器
type ConnectionManager struct {
	// 连接存储
	connections map[string]*ManagedConnection
	mutex       sync.RWMutex

	// 配置
	config      *ManagerConfig
	timeoutCtrl *timeout.TimeoutController

	// 控制
	ctx           context.Context
	cancel        context.CancelFunc
	cleanupTicker *time.Ticker

	// 事件处理器
	onConnect    ConnectionEventHandler
	onDisconnect ConnectionEventHandler
	onError      ErrorEventHandler

	// 统计
	stats *ManagerStats

	// 日志接口（简单接口，不依赖具体实现）
	logger Logger
}

// Logger 简单日志接口
type Logger interface {
	Info(msg string)
	Warn(msg string)
	Error(msg string)
}

// ManagerConfig 连接管理器配置
type ManagerConfig struct {
	// 连接限制
	MaxConnections int           `json:"max_connections"` // 最大连接数
	MaxIdleTime    time.Duration `json:"max_idle_time"`   // 最大空闲时间
	MaxLifetime    time.Duration `json:"max_lifetime"`    // 最大生存时间

	// 清理配置
	CleanupInterval     time.Duration `json:"cleanup_interval"`      // 清理间隔
	HealthCheckInterval time.Duration `json:"health_check_interval"` // 健康检查间隔

	// 重试配置
	EnableRetry       bool          `json:"enable_retry"`       // 启用重试
	MaxRetries        int           `json:"max_retries"`        // 最大重试次数
	RetryInterval     time.Duration `json:"retry_interval"`     // 重试间隔
	BackoffMultiplier float64       `json:"backoff_multiplier"` // 退避倍数

	// 监控配置
	EnableMetrics  bool `json:"enable_metrics"`   // 启用监控
	EnableAuditLog bool `json:"enable_audit_log"` // 启用审计日志
}

// DefaultManagerConfig 返回默认配置
func DefaultManagerConfig() *ManagerConfig {
	return &ManagerConfig{
		MaxConnections:      100,
		MaxIdleTime:         30 * time.Minute,
		MaxLifetime:         2 * time.Hour,
		CleanupInterval:     5 * time.Minute,
		HealthCheckInterval: 10 * time.Minute,
		EnableRetry:         true,
		MaxRetries:          3,
		RetryInterval:       5 * time.Second,
		BackoffMultiplier:   2.0,
		EnableMetrics:       true,
		EnableAuditLog:      true,
	}
}

// ManagedConnection 被管理的连接
type ManagedConnection struct {
	// 基础信息
	ID         string
	Type       string
	Target     string
	Connection interface{} // 实际的连接对象（如*sql.DB, *websocket.Conn等）
	Metadata   map[string]string

	// 时间信息
	CreatedAt       time.Time
	LastActiveAt    time.Time
	LastHealthCheck time.Time

	// 状态信息
	Status     ConnectionStatus
	Error      error
	RetryCount int

	// 统计信息
	UsageCount int64
	ErrorCount int64
	TotalTime  time.Duration

	// 同步
	mutex sync.RWMutex

	// 健康检查函数
	HealthChecker HealthChecker
}

// ConnectionStatus 连接状态
type ConnectionStatus string

const (
	StatusActive    ConnectionStatus = "active"
	StatusIdle      ConnectionStatus = "idle"
	StatusUnhealthy ConnectionStatus = "unhealthy"
	StatusError     ConnectionStatus = "error"
	StatusExpired   ConnectionStatus = "expired"
	StatusClosing   ConnectionStatus = "closing"
	StatusClosed    ConnectionStatus = "closed"
)

// HealthChecker 健康检查接口
type HealthChecker interface {
	CheckHealth(ctx context.Context, conn interface{}) error
}

// HealthCheckerFunc 健康检查函数类型
type HealthCheckerFunc func(ctx context.Context, conn interface{}) error

func (f HealthCheckerFunc) CheckHealth(ctx context.Context, conn interface{}) error {
	return f(ctx, conn)
}

// ConnectionEventHandler 连接事件处理器
type ConnectionEventHandler func(conn *ManagedConnection)

// ErrorEventHandler 错误事件处理器
type ErrorEventHandler func(conn *ManagedConnection, err error)

// ManagerStats 管理器统计
type ManagerStats struct {
	TotalConnections  int64         `json:"total_connections"`
	ActiveConnections int64         `json:"active_connections"`
	IdleConnections   int64         `json:"idle_connections"`
	ErrorConnections  int64         `json:"error_connections"`
	TotalUsages       int64         `json:"total_usages"`
	TotalErrors       int64         `json:"total_errors"`
	AverageLifetime   time.Duration `json:"average_lifetime"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

// NewConnectionManager 创建连接管理器
func NewConnectionManager(config *ManagerConfig, timeoutConfig *timeout.TimeoutConfig, logger Logger) *ConnectionManager {
	if config == nil {
		config = DefaultManagerConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	manager := &ConnectionManager{
		connections: make(map[string]*ManagedConnection),
		config:      config,
		timeoutCtrl: timeout.NewTimeoutController(timeoutConfig),
		ctx:         ctx,
		cancel:      cancel,
		stats:       &ManagerStats{},
		logger:      logger,
	}

	// 启动清理和健康检查协程
	manager.cleanupTicker = time.NewTicker(config.CleanupInterval)
	go manager.backgroundTasks()

	return manager
}

// AddConnection 添加连接
func (cm *ConnectionManager) AddConnection(id, connType, target string, conn interface{}, metadata map[string]string, healthChecker HealthChecker) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// 检查连接数限制
	if len(cm.connections) >= cm.config.MaxConnections {
		return fmt.Errorf("maximum connections limit reached: %d", cm.config.MaxConnections)
	}

	// 如果连接已存在，先移除旧连接
	if existing, exists := cm.connections[id]; exists {
		cm.removeConnectionUnsafe(id, existing)
	}

	// 创建管理连接对象
	managed := &ManagedConnection{
		ID:              id,
		Type:            connType,
		Target:          target,
		Connection:      conn,
		Metadata:        metadata,
		CreatedAt:       time.Now(),
		LastActiveAt:    time.Now(),
		LastHealthCheck: time.Now(),
		Status:          StatusActive,
		HealthChecker:   healthChecker,
	}

	cm.connections[id] = managed

	// 更新统计
	cm.stats.TotalConnections++
	cm.stats.ActiveConnections++
	cm.stats.UpdatedAt = time.Now()

	// 触发连接事件
	if cm.onConnect != nil {
		go cm.onConnect(managed)
	}

	if cm.logger != nil && cm.config.EnableAuditLog {
		cm.logger.Info(fmt.Sprintf("Connection added: %s (%s -> %s)", id, connType, target))
	}

	return nil
}

// GetConnection 获取连接
func (cm *ConnectionManager) GetConnection(id string) (*ManagedConnection, bool) {
	cm.mutex.RLock()
	managed, exists := cm.connections[id]
	cm.mutex.RUnlock()

	if !exists {
		return nil, false
	}

	// 更新使用统计
	managed.mutex.Lock()
	managed.LastActiveAt = time.Now()
	managed.UsageCount++
	if managed.Status == StatusIdle {
		managed.Status = StatusActive
	}
	managed.mutex.Unlock()

	return managed, true
}

// RemoveConnection 移除连接
func (cm *ConnectionManager) RemoveConnection(id string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	managed, exists := cm.connections[id]
	if !exists {
		return fmt.Errorf("connection not found: %s", id)
	}

	return cm.removeConnectionUnsafe(id, managed)
}

// removeConnectionUnsafe 移除连接（内部方法，不加锁）
func (cm *ConnectionManager) removeConnectionUnsafe(id string, managed *ManagedConnection) error {
	// 更新状态
	managed.mutex.Lock()
	managed.Status = StatusClosing
	managed.mutex.Unlock()

	// 如果有Closer接口，调用Close方法
	if closer, ok := managed.Connection.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil && cm.logger != nil {
			cm.logger.Warn(fmt.Sprintf("Failed to close connection %s: %v", id, err))
		}
	}

	// 从映射中删除
	delete(cm.connections, id)

	// 更新统计
	switch managed.Status {
	case StatusActive:
		cm.stats.ActiveConnections--
	case StatusIdle:
		cm.stats.IdleConnections--
	case StatusError, StatusUnhealthy:
		cm.stats.ErrorConnections--
	}

	managed.mutex.Lock()
	managed.Status = StatusClosed
	managed.mutex.Unlock()

	// 触发断开连接事件
	if cm.onDisconnect != nil {
		go cm.onDisconnect(managed)
	}

	if cm.logger != nil && cm.config.EnableAuditLog {
		cm.logger.Info(fmt.Sprintf("Connection removed: %s", id))
	}

	return nil
}

// ListConnections 列出所有连接
func (cm *ConnectionManager) ListConnections() []*ManagedConnection {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	connections := make([]*ManagedConnection, 0, len(cm.connections))
	for _, conn := range cm.connections {
		connections = append(connections, conn)
	}

	return connections
}

// GetConnectionsByType 根据类型获取连接
func (cm *ConnectionManager) GetConnectionsByType(connType string) []*ManagedConnection {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	var connections []*ManagedConnection
	for _, conn := range cm.connections {
		if conn.Type == connType {
			connections = append(connections, conn)
		}
	}

	return connections
}

// GetConnectionsByStatus 根据状态获取连接
func (cm *ConnectionManager) GetConnectionsByStatus(status ConnectionStatus) []*ManagedConnection {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	var connections []*ManagedConnection
	for _, conn := range cm.connections {
		conn.mutex.RLock()
		if conn.Status == status {
			connections = append(connections, conn)
		}
		conn.mutex.RUnlock()
	}

	return connections
}

// GetStats 获取统计信息
func (cm *ConnectionManager) GetStats() *ManagerStats {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	// 实时计算统计信息
	stats := &ManagerStats{
		TotalConnections: cm.stats.TotalConnections,
		TotalUsages:      0,
		TotalErrors:      0,
		UpdatedAt:        time.Now(),
	}

	var totalLifetime time.Duration
	activeCount := 0
	idleCount := 0
	errorCount := 0

	for _, conn := range cm.connections {
		conn.mutex.RLock()
		switch conn.Status {
		case StatusActive:
			activeCount++
		case StatusIdle:
			idleCount++
		case StatusError, StatusUnhealthy:
			errorCount++
		}
		stats.TotalUsages += conn.UsageCount
		stats.TotalErrors += conn.ErrorCount
		totalLifetime += time.Since(conn.CreatedAt)
		conn.mutex.RUnlock()
	}

	stats.ActiveConnections = int64(activeCount)
	stats.IdleConnections = int64(idleCount)
	stats.ErrorConnections = int64(errorCount)

	if len(cm.connections) > 0 {
		stats.AverageLifetime = totalLifetime / time.Duration(len(cm.connections))
	}

	return stats
}

// SetEventHandlers 设置事件处理器
func (cm *ConnectionManager) SetEventHandlers(onConnect, onDisconnect ConnectionEventHandler, onError ErrorEventHandler) {
	cm.onConnect = onConnect
	cm.onDisconnect = onDisconnect
	cm.onError = onError
}

// Shutdown 关闭管理器
func (cm *ConnectionManager) Shutdown() error {
	if cm.logger != nil {
		cm.logger.Info("Shutting down connection manager...")
	}

	// 停止后台任务
	cm.cancel()
	cm.cleanupTicker.Stop()

	// 关闭所有连接
	cm.mutex.Lock()
	connections := make([]*ManagedConnection, 0, len(cm.connections))
	for _, conn := range cm.connections {
		connections = append(connections, conn)
	}
	cm.connections = make(map[string]*ManagedConnection)
	cm.mutex.Unlock()

	for _, conn := range connections {
		cm.removeConnectionUnsafe(conn.ID, conn)
	}

	if cm.logger != nil {
		cm.logger.Info(fmt.Sprintf("Connection manager shutdown completed, closed %d connections", len(connections)))
	}

	return nil
}

// backgroundTasks 后台任务
func (cm *ConnectionManager) backgroundTasks() {
	healthCheckTicker := time.NewTicker(cm.config.HealthCheckInterval)
	defer healthCheckTicker.Stop()

	for {
		select {
		case <-cm.ctx.Done():
			return
		case <-cm.cleanupTicker.C:
			cm.cleanup()
		case <-healthCheckTicker.C:
			cm.healthCheck()
		}
	}
}

// cleanup 清理过期连接
func (cm *ConnectionManager) cleanup() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	now := time.Now()
	var expiredIDs []string

	for id, conn := range cm.connections {
		conn.mutex.RLock()

		shouldRemove := false
		reason := ""

		// 检查空闲超时
		if now.Sub(conn.LastActiveAt) > cm.config.MaxIdleTime {
			shouldRemove = true
			reason = "idle timeout"
		}

		// 检查最大生存时间
		if now.Sub(conn.CreatedAt) > cm.config.MaxLifetime {
			shouldRemove = true
			reason = "max lifetime exceeded"
		}

		// 检查错误状态
		if conn.Status == StatusError || conn.Status == StatusUnhealthy {
			shouldRemove = true
			reason = "unhealthy connection"
		}

		conn.mutex.RUnlock()

		if shouldRemove {
			expiredIDs = append(expiredIDs, id)
			if cm.logger != nil && cm.config.EnableAuditLog {
				cm.logger.Info(fmt.Sprintf("Marking connection for cleanup: %s (reason: %s)", id, reason))
			}
		}
	}

	// 移除过期连接
	for _, id := range expiredIDs {
		if conn := cm.connections[id]; conn != nil {
			cm.removeConnectionUnsafe(id, conn)
		}
	}

	if len(expiredIDs) > 0 && cm.logger != nil {
		cm.logger.Info(fmt.Sprintf("Cleanup completed: removed %d connections", len(expiredIDs)))
	}
}

// healthCheck 健康检查
func (cm *ConnectionManager) healthCheck() {
	cm.mutex.RLock()
	connections := make([]*ManagedConnection, 0, len(cm.connections))
	for _, conn := range cm.connections {
		connections = append(connections, conn)
	}
	cm.mutex.RUnlock()

	for _, conn := range connections {
		go cm.checkConnectionHealth(conn)
	}
}

// checkConnectionHealth 检查单个连接健康状态
func (cm *ConnectionManager) checkConnectionHealth(conn *ManagedConnection) {
	if conn.HealthChecker == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), cm.timeoutCtrl.GetConnectTimeout())
	defer cancel()

	conn.mutex.Lock()
	lastCheck := conn.LastHealthCheck
	conn.LastHealthCheck = time.Now()
	conn.mutex.Unlock()

	err := conn.HealthChecker.CheckHealth(ctx, conn.Connection)

	conn.mutex.Lock()
	if err != nil {
		conn.Error = err
		conn.ErrorCount++
		if conn.Status == StatusActive || conn.Status == StatusIdle {
			conn.Status = StatusUnhealthy
		}

		// 触发错误事件
		if cm.onError != nil {
			go cm.onError(conn, err)
		}

		if cm.logger != nil {
			cm.logger.Warn(fmt.Sprintf("Health check failed for connection %s: %v", conn.ID, err))
		}
	} else {
		// 健康检查成功，更新状态
		if conn.Status == StatusUnhealthy {
			conn.Status = StatusIdle
			conn.Error = nil
		}
	}
	conn.mutex.Unlock()

	// 记录健康检查间隔
	if cm.logger != nil && cm.config.EnableAuditLog && time.Since(lastCheck) > cm.config.HealthCheckInterval*2 {
		cm.logger.Warn(fmt.Sprintf("Health check interval exceeded for connection %s", conn.ID))
	}
}

// GetConnection 方法的扩展版本，支持自动重连
func (cm *ConnectionManager) GetConnectionWithRetry(id string) (*ManagedConnection, error) {
	conn, exists := cm.GetConnection(id)
	if !exists {
		return nil, fmt.Errorf("connection not found: %s", id)
	}

	// 检查连接状态
	conn.mutex.RLock()
	status := conn.Status
	conn.mutex.RUnlock()

	if status == StatusError || status == StatusUnhealthy {
		if cm.config.EnableRetry {
			return cm.retryConnection(conn)
		}
		return nil, fmt.Errorf("connection %s is in %s state", id, status)
	}

	return conn, nil
}

// retryConnection 重试连接
func (cm *ConnectionManager) retryConnection(conn *ManagedConnection) (*ManagedConnection, error) {
	conn.mutex.Lock()
	if conn.RetryCount >= cm.config.MaxRetries {
		conn.mutex.Unlock()
		return nil, fmt.Errorf("connection %s exceeded max retries", conn.ID)
	}
	conn.RetryCount++
	retryCount := conn.RetryCount
	conn.mutex.Unlock()

	// 计算退避时间
	backoffTime := time.Duration(float64(cm.config.RetryInterval) *
		(cm.config.BackoffMultiplier * float64(retryCount)))

	time.Sleep(backoffTime)

	// 执行健康检查
	cm.checkConnectionHealth(conn)

	conn.mutex.RLock()
	status := conn.Status
	conn.mutex.RUnlock()

	if status == StatusError || status == StatusUnhealthy {
		return cm.retryConnection(conn) // 递归重试
	}

	// 重置重试计数
	conn.mutex.Lock()
	conn.RetryCount = 0
	conn.mutex.Unlock()

	return conn, nil
}
