package db

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ConnectionManager 数据库连接管理器
type ConnectionManager struct {
	connections map[string]*ManagedConnection
	mutex       sync.RWMutex
	config      *ConnectionManagerConfig
	ticker      *time.Ticker
	stopChan    chan struct{}
	logger      interface {
		Info(msg string)
		Warn(msg string)
	}
}

// ConnectionManagerConfig 连接管理器配置
type ConnectionManagerConfig struct {
	// 连接超时设置
	ConnectionTimeout time.Duration `json:"connection_timeout"` // 建立连接超时 (默认30秒)
	IdleTimeout       time.Duration `json:"idle_timeout"`       // 空闲连接超时 (默认30分钟)
	MaxLifetime       time.Duration `json:"max_lifetime"`       // 连接最大生存时间 (默认2小时)

	// 清理设置
	CleanupInterval time.Duration `json:"cleanup_interval"` // 清理检查间隔 (默认5分钟)
	MaxConnections  int           `json:"max_connections"`  // 最大连接数 (默认100)

	// SQL执行超时
	DefaultQueryTimeout time.Duration `json:"default_query_timeout"` // 默认查询超时 (默认60秒)
	DefaultExecTimeout  time.Duration `json:"default_exec_timeout"`  // 默认执行超时 (默认300秒)
	MaxQueryTimeout     time.Duration `json:"max_query_timeout"`     // 最大查询超时 (默认600秒)

	// 内存控制
	EnableMemoryControl bool  `json:"enable_memory_control"` // 启用内存控制
	MaxMemoryPerConn    int64 `json:"max_memory_per_conn"`   // 每个连接最大内存 (默认100MB)

	// 日志设置
	EnableAuditLog     bool          `json:"enable_audit_log"`     // 启用审计日志
	LogSlowQueries     bool          `json:"log_slow_queries"`     // 记录慢查询
	SlowQueryThreshold time.Duration `json:"slow_query_threshold"` // 慢查询阈值 (默认10秒)
}

// ManagedConnection 被管理的连接
type ManagedConnection struct {
	conn         DbConnection
	id           string
	createdAt    time.Time
	lastActiveAt time.Time
	queryCount   int64
	errorCount   int64
	totalTime    time.Duration
	metadata     map[string]interface{}
	mutex        sync.RWMutex
}

// NewConnectionManager 创建连接管理器
func NewConnectionManager(config *ConnectionManagerConfig, logger interface {
	Info(msg string)
	Warn(msg string)
}) *ConnectionManager {
	if config == nil {
		config = DefaultConnectionManagerConfig()
	}

	manager := &ConnectionManager{
		connections: make(map[string]*ManagedConnection),
		config:      config,
		stopChan:    make(chan struct{}),
		logger:      logger,
	}

	// 启动清理任务
	manager.startCleanupTask()

	return manager
}

// DefaultConnectionManagerConfig 默认连接管理器配置
func DefaultConnectionManagerConfig() *ConnectionManagerConfig {
	return &ConnectionManagerConfig{
		ConnectionTimeout:   30 * time.Second,
		IdleTimeout:         30 * time.Minute,
		MaxLifetime:         2 * time.Hour,
		CleanupInterval:     5 * time.Minute,
		MaxConnections:      100,
		DefaultQueryTimeout: 60 * time.Second,
		DefaultExecTimeout:  300 * time.Second,
		MaxQueryTimeout:     600 * time.Second,
		EnableMemoryControl: true,
		MaxMemoryPerConn:    100 * 1024 * 1024, // 100MB
		EnableAuditLog:      true,
		LogSlowQueries:      true,
		SlowQueryThreshold:  10 * time.Second,
	}
}

// AddConnection 添加连接到管理器
func (cm *ConnectionManager) AddConnection(conn DbConnection) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// 检查连接数限制
	if len(cm.connections) >= cm.config.MaxConnections {
		return fmt.Errorf("maximum connections limit reached: %d", cm.config.MaxConnections)
	}

	id := conn.ID()
	managed := &ManagedConnection{
		conn:         conn,
		id:           id,
		createdAt:    time.Now(),
		lastActiveAt: time.Now(),
		metadata:     make(map[string]interface{}),
	}

	cm.connections[id] = managed

	if cm.logger != nil {
		cm.logger.Info(fmt.Sprintf("Connection added to manager: %s", id))
	}

	return nil
}

// GetConnection 获取连接
func (cm *ConnectionManager) GetConnection(id string) (DbConnection, error) {
	cm.mutex.RLock()
	managed, exists := cm.connections[id]
	cm.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("connection not found: %s", id)
	}

	// 更新最后活动时间
	managed.mutex.Lock()
	managed.lastActiveAt = time.Now()
	managed.mutex.Unlock()

	// 检查连接是否仍然有效
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := managed.conn.Ping(ctx); err != nil {
		// 连接已失效，从管理器中移除
		cm.RemoveConnection(id)
		return nil, fmt.Errorf("connection is invalid: %w", err)
	}

	return managed.conn, nil
}

// RemoveConnection 移除连接
func (cm *ConnectionManager) RemoveConnection(id string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	managed, exists := cm.connections[id]
	if !exists {
		return fmt.Errorf("connection not found: %s", id)
	}

	// 关闭连接
	if err := managed.conn.Close(); err != nil && cm.logger != nil {
		cm.logger.Warn(fmt.Sprintf("Failed to close connection %s: %v", id, err))
	}

	delete(cm.connections, id)

	if cm.logger != nil {
		cm.logger.Info(fmt.Sprintf("Connection removed from manager: %s", id))
	}

	return nil
}

// ExecuteQueryWithTimeout 执行查询（带超时控制）
func (cm *ConnectionManager) ExecuteQueryWithTimeout(ctx context.Context, connectionId, sql string, timeout time.Duration, args ...any) (*QueryResult, error) {
	conn, err := cm.GetConnection(connectionId)
	if err != nil {
		return nil, err
	}

	// 应用超时控制
	if timeout == 0 {
		timeout = cm.config.DefaultQueryTimeout
	}
	if timeout > cm.config.MaxQueryTimeout {
		timeout = cm.config.MaxQueryTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startTime := time.Now()

	// 执行查询
	result, err := conn.ExecuteQuery(ctx, sql, args...)

	duration := time.Since(startTime)

	// 更新统计信息
	cm.updateConnectionStats(connectionId, duration, err)

	// 记录慢查询
	if cm.config.LogSlowQueries && duration > cm.config.SlowQueryThreshold && cm.logger != nil {
		cm.logger.Warn(fmt.Sprintf("Slow query detected: %s (duration: %v, sql: %s)", connectionId, duration, sql))
	}

	// 记录审计日志
	if cm.config.EnableAuditLog && cm.logger != nil {
		status := "success"
		if err != nil {
			status = "error"
		}
		cm.logger.Info(fmt.Sprintf("Query executed: conn=%s, status=%s, duration=%v, sql=%s", connectionId, status, duration, sql))
	}

	return result, err
}

// ExecuteUpdateWithTimeout 执行更新（带超时控制）
func (cm *ConnectionManager) ExecuteUpdateWithTimeout(ctx context.Context, connectionId, sql string, timeout time.Duration, args ...any) (*ExecResult, error) {
	conn, err := cm.GetConnection(connectionId)
	if err != nil {
		return nil, err
	}

	// 应用超时控制
	if timeout == 0 {
		timeout = cm.config.DefaultExecTimeout
	}
	if timeout > cm.config.MaxQueryTimeout {
		timeout = cm.config.MaxQueryTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startTime := time.Now()

	// 执行更新
	result, err := conn.ExecuteUpdate(ctx, sql, args...)

	duration := time.Since(startTime)

	// 更新统计信息
	cm.updateConnectionStats(connectionId, duration, err)

	// 记录慢查询
	if cm.config.LogSlowQueries && duration > cm.config.SlowQueryThreshold && cm.logger != nil {
		cm.logger.Warn(fmt.Sprintf("Slow update detected: %s (duration: %v, sql: %s)", connectionId, duration, sql))
	}

	// 记录审计日志
	if cm.config.EnableAuditLog && cm.logger != nil {
		status := "success"
		if err != nil {
			status = "error"
		}
		cm.logger.Info(fmt.Sprintf("Update executed: conn=%s, status=%s, duration=%v, sql=%s", connectionId, status, duration, sql))
	}

	return result, err
}

// ListConnections 列出所有连接信息
func (cm *ConnectionManager) ListConnections() []ConnectionInfo {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	connections := make([]ConnectionInfo, 0, len(cm.connections))
	now := time.Now()

	for _, managed := range cm.connections {
		managed.mutex.RLock()
		info := ConnectionInfo{
			ID:           managed.id,
			CreatedAt:    managed.createdAt,
			LastActiveAt: managed.lastActiveAt,
			IdleDuration: now.Sub(managed.lastActiveAt),
			Age:          now.Sub(managed.createdAt),
			QueryCount:   managed.queryCount,
			ErrorCount:   managed.errorCount,
			AvgDuration:  managed.getAvgDuration(),
			Status:       cm.getConnectionStatus(managed),
		}
		managed.mutex.RUnlock()
		connections = append(connections, info)
	}

	return connections
}

// GetStats 获取管理器统计信息
func (cm *ConnectionManager) GetStats() *ManagerStats {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	stats := &ManagerStats{
		TotalConnections: len(cm.connections),
		ActiveThreshold:  cm.config.IdleTimeout,
		Config:           cm.config,
	}

	now := time.Now()
	for _, managed := range cm.connections {
		managed.mutex.RLock()
		if now.Sub(managed.lastActiveAt) < cm.config.IdleTimeout {
			stats.ActiveConnections++
		} else {
			stats.IdleConnections++
		}
		stats.TotalQueries += managed.queryCount
		stats.TotalErrors += managed.errorCount
		managed.mutex.RUnlock()
	}

	return stats
}

// Shutdown 关闭连接管理器
func (cm *ConnectionManager) Shutdown() error {
	// 停止清理任务
	close(cm.stopChan)
	if cm.ticker != nil {
		cm.ticker.Stop()
	}

	// 关闭所有连接
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	for id, managed := range cm.connections {
		if err := managed.conn.Close(); err != nil && cm.logger != nil {
			cm.logger.Warn(fmt.Sprintf("Failed to close connection %s during shutdown: %v", id, err))
		}
	}

	cm.connections = make(map[string]*ManagedConnection)

	if cm.logger != nil {
		cm.logger.Info("Connection manager shutdown completed")
	}

	return nil
}

// startCleanupTask 启动清理任务
func (cm *ConnectionManager) startCleanupTask() {
	cm.ticker = time.NewTicker(cm.config.CleanupInterval)

	go func() {
		for {
			select {
			case <-cm.ticker.C:
				cm.cleanup()
			case <-cm.stopChan:
				return
			}
		}
	}()
}

// cleanup 清理过期连接
func (cm *ConnectionManager) cleanup() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	now := time.Now()
	var toRemove []string

	for id, managed := range cm.connections {
		managed.mutex.RLock()

		// 检查连接是否过期
		shouldRemove := false
		reason := ""

		// 检查空闲超时
		if now.Sub(managed.lastActiveAt) > cm.config.IdleTimeout {
			shouldRemove = true
			reason = "idle timeout"
		}

		// 检查最大生存时间
		if now.Sub(managed.createdAt) > cm.config.MaxLifetime {
			shouldRemove = true
			reason = "max lifetime exceeded"
		}

		// 检查连接健康状态 - 使用更短的超时时间并增加错误处理
		if !shouldRemove {
			// 创建独立的context，避免阻塞主清理流程
			pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

			// 启动goroutine进行健康检查，避免阻塞
			pingDone := make(chan error, 1)
			go func() {
				defer cancel()
				defer func() {
					if r := recover(); r != nil {
						pingDone <- fmt.Errorf("ping panic: %v", r)
					}
				}()
				pingDone <- managed.conn.Ping(pingCtx)
			}()

			// 等待ping结果或超时
			select {
			case err := <-pingDone:
				if err != nil {
					shouldRemove = true
					reason = fmt.Sprintf("ping failed: %v", err)
				}
			case <-time.After(3 * time.Second):
				shouldRemove = true
				reason = "ping timeout"
				cancel() // 确保取消context
			}
		}

		managed.mutex.RUnlock()

		if shouldRemove {
			toRemove = append(toRemove, id)
			if cm.logger != nil {
				cm.logger.Info(fmt.Sprintf("Marking connection for cleanup: %s (reason: %s)", id, reason))
			}
		}
	}

	// 移除过期连接 - 并发安全的清理
	for _, id := range toRemove {
		if managed, exists := cm.connections[id]; exists {
			// 使用goroutine异步关闭连接，避免阻塞清理流程
			go func(conn DbConnection, connId string) {
				defer func() {
					if r := recover(); r != nil {
						if cm.logger != nil {
							cm.logger.Warn(fmt.Sprintf("Panic while closing connection %s: %v", connId, r))
						}
					}
				}()

				// 设置关闭超时
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				done := make(chan error, 1)
				go func() {
					done <- func() error {
						if closer, ok := conn.(interface{ Close() error }); ok {
							return closer.Close()
						}
						return nil
					}()
				}()

				select {
				case err := <-done:
					if err != nil && cm.logger != nil {
						cm.logger.Warn(fmt.Sprintf("Failed to close connection %s during cleanup: %v", connId, err))
					}
				case <-ctx.Done():
					if cm.logger != nil {
						cm.logger.Warn(fmt.Sprintf("Timeout while closing connection %s", connId))
					}
				}
			}(managed.conn, id)

			// 立即从映射中删除，防止重复使用
			delete(cm.connections, id)
		}
	}

	if len(toRemove) > 0 && cm.logger != nil {
		cm.logger.Info(fmt.Sprintf("Cleanup completed: marked %d connections for removal", len(toRemove)))
	}

	// 检查连接数量是否超过限制
	if len(cm.connections) > cm.config.MaxConnections {
		excess := len(cm.connections) - cm.config.MaxConnections
		if cm.logger != nil {
			cm.logger.Warn(fmt.Sprintf("Connection count (%d) exceeds limit (%d), need to remove %d connections",
				len(cm.connections), cm.config.MaxConnections, excess))
		}

		// 按最后活动时间排序，移除最旧的连接
		type connWithTime struct {
			id           string
			lastActiveAt time.Time
		}

		var connsWithTime []connWithTime
		for id, managed := range cm.connections {
			managed.mutex.RLock()
			connsWithTime = append(connsWithTime, connWithTime{
				id:           id,
				lastActiveAt: managed.lastActiveAt,
			})
			managed.mutex.RUnlock()
		}

		// 简单排序，找出最旧的连接
		for i := 0; i < excess && i < len(connsWithTime); i++ {
			oldestIdx := i
			for j := i + 1; j < len(connsWithTime); j++ {
				if connsWithTime[j].lastActiveAt.Before(connsWithTime[oldestIdx].lastActiveAt) {
					oldestIdx = j
				}
			}
			if oldestIdx != i {
				connsWithTime[i], connsWithTime[oldestIdx] = connsWithTime[oldestIdx], connsWithTime[i]
			}

			// 移除最旧的连接
			id := connsWithTime[i].id
			if managed, exists := cm.connections[id]; exists {
				go func(conn DbConnection, connId string) {
					defer func() {
						if r := recover(); r != nil {
							if cm.logger != nil {
								cm.logger.Warn(fmt.Sprintf("Panic while force closing connection %s: %v", connId, r))
							}
						}
					}()
					if closer, ok := conn.(interface{ Close() error }); ok {
						closer.Close()
					}
				}(managed.conn, id)

				delete(cm.connections, id)
				if cm.logger != nil {
					cm.logger.Info(fmt.Sprintf("Force removed excess connection: %s", id))
				}
			}
		}
	}
}

// updateConnectionStats 更新连接统计信息
func (cm *ConnectionManager) updateConnectionStats(connectionId string, duration time.Duration, err error) {
	cm.mutex.RLock()
	managed, exists := cm.connections[connectionId]
	cm.mutex.RUnlock()

	if !exists {
		return
	}

	managed.mutex.Lock()
	defer managed.mutex.Unlock()

	managed.queryCount++
	managed.totalTime += duration
	managed.lastActiveAt = time.Now()

	if err != nil {
		managed.errorCount++
	}
}

// getConnectionStatus 获取连接状态
func (cm *ConnectionManager) getConnectionStatus(managed *ManagedConnection) string {
	now := time.Now()

	if now.Sub(managed.lastActiveAt) > cm.config.IdleTimeout {
		return "idle"
	}

	if now.Sub(managed.createdAt) > cm.config.MaxLifetime*9/10 {
		return "aging"
	}

	return "active"
}

// getAvgDuration 获取平均执行时间
func (mc *ManagedConnection) getAvgDuration() time.Duration {
	if mc.queryCount == 0 {
		return 0
	}
	return mc.totalTime / time.Duration(mc.queryCount)
}

// ConnectionInfo 连接信息
type ConnectionInfo struct {
	ID           string        `json:"id"`
	CreatedAt    time.Time     `json:"created_at"`
	LastActiveAt time.Time     `json:"last_active_at"`
	IdleDuration time.Duration `json:"idle_duration"`
	Age          time.Duration `json:"age"`
	QueryCount   int64         `json:"query_count"`
	ErrorCount   int64         `json:"error_count"`
	AvgDuration  time.Duration `json:"avg_duration"`
	Status       string        `json:"status"`
}

// ManagerStats 管理器统计信息
type ManagerStats struct {
	TotalConnections  int                      `json:"total_connections"`
	ActiveConnections int                      `json:"active_connections"`
	IdleConnections   int                      `json:"idle_connections"`
	TotalQueries      int64                    `json:"total_queries"`
	TotalErrors       int64                    `json:"total_errors"`
	ActiveThreshold   time.Duration            `json:"active_threshold"`
	Config            *ConnectionManagerConfig `json:"config"`
}
