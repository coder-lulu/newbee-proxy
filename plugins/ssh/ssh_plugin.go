package ssh

import (
	"context"
	"fmt"
	"sync"
	"time"

	"newbee-agent/plugins/common"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	PluginName        = "ssh"
	PluginVersion     = "v1.0.0"
	PluginDescription = "SSH协议插件，支持SSH连接管理和WebSocket桥接"
)

// SSHPlugin SSH协议插件实现
type SSHPlugin struct {
	// 基础信息
	name        string
	version     string
	description string

	// 生命周期状态
	isRunning bool
	startedAt *time.Time
	stoppedAt *time.Time
	loadedAt  time.Time

	// 配置
	config map[string]interface{}

	// 核心组件
	connectionPool   *common.ConnectionPool
	metricsCollector *common.MetricsCollector
	sessionManager   *common.SessionManager
	validator        *common.ConfigValidator
	idGenerator      *common.IDGenerator

	// 同步控制
	mutex  sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	// 日志
	logger logx.Logger

	// SSH特定配置
	sshConfig *SSHConfig
}

// SSHConfig SSH插件配置
type SSHConfig struct {
	MaxConnections    int           `json:"max_connections"`     // 最大连接数
	KeepAliveInterval time.Duration `json:"keep_alive_interval"` // 心跳间隔
	ConnectionTimeout time.Duration `json:"connection_timeout"`  // 连接超时
	BufferSize        int           `json:"buffer_size"`         // 缓冲区大小
	EnableCompression bool          `json:"enable_compression"`  // 启用压缩
	SupportedCiphers  []string      `json:"supported_ciphers"`   // 支持的加密算法

	// WebSocket桥接配置
	WebSocketBridge *common.WebSocketBridge `json:"websocket_bridge"`
}

// NewSSHPlugin 创建SSH插件实例
func NewSSHPlugin() *SSHPlugin {
	ctx, cancel := context.WithCancel(context.Background())

	return &SSHPlugin{
		name:        PluginName,
		version:     PluginVersion,
		description: PluginDescription,
		loadedAt:    time.Now(),
		config:      make(map[string]interface{}),
		ctx:         ctx,
		cancel:      cancel,
		logger:      logx.WithContext(ctx),
		validator:   &common.ConfigValidator{},
		idGenerator: common.NewIDGenerator("ssh"),
		sshConfig: &SSHConfig{
			MaxConnections:    100,
			KeepAliveInterval: 30 * time.Second,
			ConnectionTimeout: 30 * time.Second,
			BufferSize:        4096,
			EnableCompression: false,
			SupportedCiphers:  []string{"aes128-ctr", "aes192-ctr", "aes256-ctr"},
			WebSocketBridge: &common.WebSocketBridge{
				Enabled:     true,
				BufferSize:  4096,
				Encoding:    "utf8",
				Compression: false,
			},
		},
	}
}

// Name 返回插件名称
func (p *SSHPlugin) Name() string {
	return p.name
}

// Version 返回插件版本
func (p *SSHPlugin) Version() string {
	return p.version
}

// SupportedProtocols 返回支持的协议列表
func (p *SSHPlugin) SupportedProtocols() []string {
	return []string{"ssh"}
}

// Description 返回插件描述
func (p *SSHPlugin) Description() string {
	return p.description
}

// Initialize 初始化插件
func (p *SSHPlugin) Initialize(config map[string]interface{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.logger.Info("Initializing SSH plugin...")

	// 保存配置
	p.config = config

	// 解析SSH特定配置
	if err := p.parseConfig(config); err != nil {
		return fmt.Errorf("failed to parse SSH config: %w", err)
	}

	// 初始化组件
	p.connectionPool = common.NewConnectionPool(p.sshConfig.MaxConnections)
	p.metricsCollector = common.NewMetricsCollector()
	p.sessionManager = common.NewSessionManager(1 * time.Hour) // 1小时会话超时

	p.logger.Infof("SSH plugin initialized with max connections: %d", p.sshConfig.MaxConnections)
	return nil
}

// Start 启动插件
func (p *SSHPlugin) Start() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.isRunning {
		return fmt.Errorf("SSH plugin is already running")
	}

	p.logger.Info("Starting SSH plugin...")

	// 验证初始化状态
	if p.connectionPool == nil {
		return fmt.Errorf("SSH plugin not initialized")
	}

	// 启动后台任务
	go p.startBackgroundTasks()

	// 更新状态
	now := time.Now()
	p.startedAt = &now
	p.isRunning = true

	p.logger.Info("SSH plugin started successfully")
	return nil
}

// Stop 停止插件
func (p *SSHPlugin) Stop() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.isRunning {
		return fmt.Errorf("SSH plugin is not running")
	}

	p.logger.Info("Stopping SSH plugin...")

	// 取消上下文，停止后台任务
	p.cancel()

	// 关闭所有连接
	if p.connectionPool != nil {
		p.connectionPool.Clear()
	}

	// 关闭所有会话
	if p.sessionManager != nil {
		p.sessionManager.CloseAllSessions()
	}

	// 更新状态
	now := time.Now()
	p.stoppedAt = &now
	p.isRunning = false

	p.logger.Info("SSH plugin stopped")
	return nil
}

// IsRunning 检查插件是否运行中
func (p *SSHPlugin) IsRunning() bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return p.isRunning
}

// CreateConnection 创建SSH连接
func (p *SSHPlugin) CreateConnection(ctx context.Context, target string, credentials *common.Credentials) (common.Connection, error) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if !p.isRunning {
		return nil, fmt.Errorf("SSH plugin is not running")
	}

	// 验证认证信息
	if err := p.validator.ValidateCredentials(credentials); err != nil {
		p.metricsCollector.IncrementErrors(common.ErrCodeInvalidConfig)
		return nil, fmt.Errorf("invalid credentials: %w", err)
	}

	// 检查连接池容量
	if p.connectionPool.Size() >= p.sshConfig.MaxConnections {
		p.metricsCollector.IncrementErrors(common.ErrCodeResourceExhausted)
		return nil, fmt.Errorf("connection pool is full (max: %d)", p.sshConfig.MaxConnections)
	}

	startTime := time.Now()

	// 创建SSH连接实例
	conn, err := p.createSSHConnection(ctx, target, credentials)
	if err != nil {
		p.metricsCollector.IncrementFailedConnections()
		p.metricsCollector.IncrementErrors(common.ErrCodeConnectionFailed)
		return nil, fmt.Errorf("failed to create SSH connection: %w", err)
	}

	// 添加到连接池
	if err := p.connectionPool.Add(conn); err != nil {
		conn.Close()
		p.metricsCollector.IncrementErrors(common.ErrCodeResourceExhausted)
		return nil, fmt.Errorf("failed to add connection to pool: %w", err)
	}

	// 更新指标
	connectionTime := time.Since(startTime)
	p.metricsCollector.RecordConnectionTime(connectionTime)
	p.metricsCollector.IncrementConnections()

	p.logger.Infof("SSH connection created successfully: %s -> %s (took %v)",
		conn.ID(), target, connectionTime)

	return conn, nil
}

// CloseConnection 关闭连接
func (p *SSHPlugin) CloseConnection(connectionId string) error {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	conn, exists := p.connectionPool.Get(connectionId)
	if !exists {
		return fmt.Errorf("connection not found: %s", connectionId)
	}

	// 关闭连接
	if err := conn.Close(); err != nil {
		p.logger.Errorf("Error closing connection %s: %v", connectionId, err)
	}

	// 从连接池移除
	p.connectionPool.Remove(connectionId)

	// 更新指标
	p.metricsCollector.DecrementConnections()

	p.logger.Infof("SSH connection closed: %s", connectionId)
	return nil
}

// GetConnection 获取连接
func (p *SSHPlugin) GetConnection(connectionId string) (common.Connection, bool) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return p.connectionPool.Get(connectionId)
}

// ListConnections 列出所有连接
func (p *SSHPlugin) ListConnections() []string {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return p.connectionPool.List()
}

// GetStatus 获取插件状态
func (p *SSHPlugin) GetStatus() *common.PluginStatus {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	status := &common.PluginStatus{
		Name:        p.name,
		Version:     p.version,
		LoadedAt:    p.loadedAt,
		StartedAt:   p.startedAt,
		StoppedAt:   p.stoppedAt,
		Config:      p.config,
		Connections: p.connectionPool.Size(),
		Sessions:    len(p.sessionManager.ListSessions()),
	}

	if p.isRunning {
		status.Status = "running"
	} else {
		status.Status = "stopped"
	}

	return status
}

// GetMetrics 获取插件指标
func (p *SSHPlugin) GetMetrics() *common.PluginMetrics {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return p.metricsCollector.GetMetrics()
}

// UpdateConfig 更新配置
func (p *SSHPlugin) UpdateConfig(config map[string]interface{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.logger.Info("Updating SSH plugin config...")

	// 解析新配置
	newSSHConfig := &SSHConfig{}
	if err := p.parseConfigToStruct(config, newSSHConfig); err != nil {
		return fmt.Errorf("failed to parse new config: %w", err)
	}

	// 更新配置
	p.config = config
	p.sshConfig = newSSHConfig

	p.logger.Info("SSH plugin config updated successfully")
	return nil
}

// parseConfig 解析配置
func (p *SSHPlugin) parseConfig(config map[string]interface{}) error {
	return p.parseConfigToStruct(config, p.sshConfig)
}

// parseConfigToStruct 解析配置到结构体
func (p *SSHPlugin) parseConfigToStruct(config map[string]interface{}, target *SSHConfig) error {
	// 这里可以使用反射或者手动解析
	// 为了简单，这里只处理几个关键配置项

	if maxConn, ok := config["max_connections"].(int); ok {
		target.MaxConnections = maxConn
	}

	if keepAlive, ok := config["keep_alive_interval"].(int); ok {
		target.KeepAliveInterval = time.Duration(keepAlive) * time.Second
	}

	if timeout, ok := config["connection_timeout"].(int); ok {
		target.ConnectionTimeout = time.Duration(timeout) * time.Second
	}

	if bufferSize, ok := config["buffer_size"].(int); ok {
		target.BufferSize = bufferSize
	}

	return nil
}

// startBackgroundTasks 启动后台任务
func (p *SSHPlugin) startBackgroundTasks() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			p.logger.Info("SSH plugin background tasks stopped")
			return
		case <-ticker.C:
			// 定期清理非活跃会话
			p.sessionManager.CleanupInactiveSessions()

			// 更新系统指标（这里可以添加实际的系统指标收集）
			p.metricsCollector.UpdateSystemMetrics(0.0, 0, 0)
		}
	}
}

// createSSHConnection 创建SSH连接实例 (将在ssh_connection.go中实现)
func (p *SSHPlugin) createSSHConnection(ctx context.Context, target string, credentials *common.Credentials) (common.Connection, error) {
	// 创建SSH连接的具体实现
	return NewSSHConnection(ctx, p.idGenerator.Generate(), target, credentials, p.sshConfig)
}
