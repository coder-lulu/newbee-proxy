package common

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// IDGenerator ID生成器
type IDGenerator struct {
	prefix  string
	counter int64
}

// NewIDGenerator 创建ID生成器
func NewIDGenerator(prefix string) *IDGenerator {
	return &IDGenerator{
		prefix:  prefix,
		counter: 0,
	}
}

// Generate 生成唯一ID
func (g *IDGenerator) Generate() string {
	timestamp := time.Now().Unix()
	counter := atomic.AddInt64(&g.counter, 1)

	// 生成随机字节
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	randomStr := hex.EncodeToString(randomBytes)

	return fmt.Sprintf("%s-%d-%d-%s", g.prefix, timestamp, counter, randomStr)
}

// ConnectionPool 连接池管理器
type ConnectionPool struct {
	connections map[string]Connection
	mutex       sync.RWMutex
	maxSize     int
	idGenerator *IDGenerator
}

// NewConnectionPool 创建连接池
func NewConnectionPool(maxSize int) *ConnectionPool {
	return &ConnectionPool{
		connections: make(map[string]Connection),
		maxSize:     maxSize,
		idGenerator: NewIDGenerator("conn"),
	}
}

// Add 添加连接
func (p *ConnectionPool) Add(conn Connection) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if len(p.connections) >= p.maxSize {
		return fmt.Errorf("connection pool is full (max: %d)", p.maxSize)
	}

	p.connections[conn.ID()] = conn
	return nil
}

// Get 获取连接
func (p *ConnectionPool) Get(id string) (Connection, bool) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	conn, exists := p.connections[id]
	return conn, exists
}

// Remove 移除连接
func (p *ConnectionPool) Remove(id string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	delete(p.connections, id)
}

// List 列出所有连接ID
func (p *ConnectionPool) List() []string {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	ids := make([]string, 0, len(p.connections))
	for id := range p.connections {
		ids = append(ids, id)
	}
	return ids
}

// Size 获取连接池大小
func (p *ConnectionPool) Size() int {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return len(p.connections)
}

// Clear 清空连接池
func (p *ConnectionPool) Clear() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// 关闭所有连接
	for _, conn := range p.connections {
		conn.Close()
	}

	p.connections = make(map[string]Connection)
}

// MetricsCollector 指标收集器
type MetricsCollector struct {
	metrics *PluginMetrics
	mutex   sync.RWMutex
}

// NewMetricsCollector 创建指标收集器
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		metrics: &PluginMetrics{
			UpdatedAt: time.Now(),
		},
	}
}

// IncrementConnections 增加连接计数
func (m *MetricsCollector) IncrementConnections() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	atomic.AddInt64(&m.metrics.TotalConnections, 1)
	m.metrics.ActiveConnections++
	m.metrics.UpdatedAt = time.Now()
}

// DecrementConnections 减少连接计数
func (m *MetricsCollector) DecrementConnections() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.metrics.ActiveConnections > 0 {
		m.metrics.ActiveConnections--
	}
	m.metrics.UpdatedAt = time.Now()
}

// IncrementFailedConnections 增加失败连接计数
func (m *MetricsCollector) IncrementFailedConnections() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	atomic.AddInt64(&m.metrics.FailedConnections, 1)
	m.metrics.UpdatedAt = time.Now()
}

// RecordConnectionTime 记录连接时间
func (m *MetricsCollector) RecordConnectionTime(duration time.Duration) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 简单的移动平均
	if m.metrics.AvgConnectionTime == 0 {
		m.metrics.AvgConnectionTime = duration
	} else {
		m.metrics.AvgConnectionTime = (m.metrics.AvgConnectionTime + duration) / 2
	}
	m.metrics.UpdatedAt = time.Now()
}

// IncrementSessions 增加会话计数
func (m *MetricsCollector) IncrementSessions() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	atomic.AddInt64(&m.metrics.TotalSessions, 1)
	m.metrics.ActiveSessions++
	m.metrics.UpdatedAt = time.Now()
}

// DecrementSessions 减少会话计数
func (m *MetricsCollector) DecrementSessions() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.metrics.ActiveSessions > 0 {
		m.metrics.ActiveSessions--
	}
	m.metrics.UpdatedAt = time.Now()
}

// AddBytesTransferred 添加传输字节数
func (m *MetricsCollector) AddBytesTransferred(sent, received int64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	atomic.AddInt64(&m.metrics.BytesSent, sent)
	atomic.AddInt64(&m.metrics.BytesReceived, received)
	m.metrics.UpdatedAt = time.Now()
}

// IncrementMessages 增加消息计数
func (m *MetricsCollector) IncrementMessages(sent, received int64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	atomic.AddInt64(&m.metrics.MessagesSent, sent)
	atomic.AddInt64(&m.metrics.MessagesReceived, received)
	m.metrics.UpdatedAt = time.Now()
}

// IncrementErrors 增加错误计数
func (m *MetricsCollector) IncrementErrors(errorType string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	atomic.AddInt64(&m.metrics.TotalErrors, 1)

	switch errorType {
	case ErrCodeTimeout:
		atomic.AddInt64(&m.metrics.TimeoutErrors, 1)
	case ErrCodeAuthFailed:
		atomic.AddInt64(&m.metrics.AuthErrors, 1)
	case ErrCodeNetworkError:
		atomic.AddInt64(&m.metrics.NetworkErrors, 1)
	}

	m.metrics.UpdatedAt = time.Now()
}

// UpdateSystemMetrics 更新系统指标
func (m *MetricsCollector) UpdateSystemMetrics(cpuUsage float64, memoryUsage int64, goroutineCount int) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.metrics.CPUUsage = cpuUsage
	m.metrics.MemoryUsage = memoryUsage
	m.metrics.GoroutineCount = goroutineCount
	m.metrics.UpdatedAt = time.Now()
}

// GetMetrics 获取指标副本
func (m *MetricsCollector) GetMetrics() *PluginMetrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// 创建副本以避免并发问题
	metrics := *m.metrics
	return &metrics
}

// Reset 重置指标
func (m *MetricsCollector) Reset() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.metrics = &PluginMetrics{
		UpdatedAt: time.Now(),
	}
}

// ConfigValidator 配置验证器
type ConfigValidator struct{}

// ValidateCredentials 验证认证信息
func (v *ConfigValidator) ValidateCredentials(creds *Credentials) error {
	if creds == nil {
		return fmt.Errorf("credentials cannot be nil")
	}

	if creds.Username == "" {
		return fmt.Errorf("username is required")
	}

	switch creds.AuthType {
	case "password":
		if creds.Password == "" {
			return fmt.Errorf("password is required for password authentication")
		}
	case "publickey":
		if creds.PrivateKey == "" && creds.KeyFile == "" {
			return fmt.Errorf("private key or key file is required for public key authentication")
		}
	case "keyboard-interactive":
		// 交互式认证，通常需要回调处理
	default:
		return fmt.Errorf("unsupported auth type: %s", creds.AuthType)
	}

	if creds.Timeout <= 0 {
		creds.Timeout = 30 // 默认30秒超时
	}

	return nil
}

// ValidateConnectionConfig 验证连接配置
func (v *ConfigValidator) ValidateConnectionConfig(config *ConnectionConfig) error {
	if config == nil {
		return fmt.Errorf("connection config cannot be nil")
	}

	if config.Target == "" {
		return fmt.Errorf("target is required")
	}

	if config.Port <= 0 || config.Port > 65535 {
		return fmt.Errorf("invalid port: %d", config.Port)
	}

	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}

	if config.MaxRetries < 0 {
		config.MaxRetries = 3
	}

	if config.RetryInterval <= 0 {
		config.RetryInterval = 5 * time.Second
	}

	if config.BufferSize <= 0 {
		config.BufferSize = 4096
	}

	return nil
}

// SessionManager 会话管理器
type SessionManager struct {
	sessions map[string]Session
	mutex    sync.RWMutex
	timeout  time.Duration
}

// NewSessionManager 创建会话管理器
func NewSessionManager(timeout time.Duration) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]Session),
		timeout:  timeout,
	}
}

// AddSession 添加会话
func (sm *SessionManager) AddSession(session Session) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.sessions[session.ID()] = session
}

// GetSession 获取会话
func (sm *SessionManager) GetSession(id string) (Session, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	session, exists := sm.sessions[id]
	return session, exists
}

// RemoveSession 移除会话
func (sm *SessionManager) RemoveSession(id string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if session, exists := sm.sessions[id]; exists {
		session.Stop()
		delete(sm.sessions, id)
	}
}

// ListSessions 列出所有会话
func (sm *SessionManager) ListSessions() []string {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	ids := make([]string, 0, len(sm.sessions))
	for id := range sm.sessions {
		ids = append(ids, id)
	}
	return ids
}

// CleanupInactiveSessions 清理非活跃会话
func (sm *SessionManager) CleanupInactiveSessions() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	for id, session := range sm.sessions {
		if !session.IsActive() {
			session.Stop()
			delete(sm.sessions, id)
		}
	}
}

// CloseAllSessions 关闭所有会话
func (sm *SessionManager) CloseAllSessions() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	for id, session := range sm.sessions {
		session.Stop()
		delete(sm.sessions, id)
	}
}
