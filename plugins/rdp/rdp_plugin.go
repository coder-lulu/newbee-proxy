package rdp

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"newbee-agent/plugins/common"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	PluginName        = "rdp"
	PluginVersion     = "v1.0.0"
	PluginDescription = "RDP协议插件，基于Guacamole协议实现Windows远程桌面连接管理"
)

// RDPPlugin RDP协议插件实现 - 参考mayfly-go架构
type RDPPlugin struct {
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

	// 核心组件 - 参考mayfly-go
	guacdManager     *GuacdManager
	tunnelManager    *TunnelManager
	sessionStore     *SessionStore
	connectionPool   *common.ConnectionPool
	metricsCollector *common.MetricsCollector
	validator        *common.ConfigValidator
	idGenerator      *common.IDGenerator

	// 同步控制
	mutex  sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	// 日志
	logger logx.Logger

	// RDP特定配置
	rdpConfig *RDPPluginConfig
}

// RDPConfig RDP插件配置
type RDPConfig struct {
	MaxConnections     int           `json:"max_connections"`      // 最大连接数
	KeepAliveInterval  time.Duration `json:"keep_alive_interval"`  // 心跳间隔
	ConnectionTimeout  time.Duration `json:"connection_timeout"`   // 连接超时
	BufferSize         int           `json:"buffer_size"`          // 缓冲区大小
	DefaultWidth       int           `json:"default_width"`        // 默认屏幕宽度
	DefaultHeight      int           `json:"default_height"`       // 默认屏幕高度
	EnableClipboard    bool          `json:"enable_clipboard"`     // 启用剪贴板共享
	EnableFileTransfer bool          `json:"enable_file_transfer"` // 启用文件传输
	CompressionLevel   int           `json:"compression_level"`    // 压缩级别 0-9

	// WebSocket桥接配置
	WebSocketBridge *common.WebSocketBridge `json:"websocket_bridge"`
}

// NewRDPPlugin 创建RDP插件实例 - 参考mayfly-go初始化方式
func NewRDPPlugin() *RDPPlugin {
	ctx, cancel := context.WithCancel(context.Background())

	// 初始化默认配置
	defaultConfig := DefaultRDPConfig()

	plugin := &RDPPlugin{
		name:        PluginName,
		version:     PluginVersion,
		description: PluginDescription,
		loadedAt:    time.Now(),
		config:      make(map[string]interface{}),
		ctx:         ctx,
		cancel:      cancel,
		logger:      logx.WithContext(ctx),
		validator:   &common.ConfigValidator{},
		idGenerator: common.NewIDGenerator("rdp"),
		rdpConfig:   defaultConfig,
	}

	// 初始化核心组件 - 参考mayfly-go组件架构
	plugin.guacdManager = NewGuacdManager(&defaultConfig.Guacd)
	plugin.tunnelManager = NewTunnelManager()
	plugin.sessionStore = NewSessionStore()

	return plugin
}

// Name 返回插件名称
func (p *RDPPlugin) Name() string {
	return p.name
}

// Version 返回插件版本
func (p *RDPPlugin) Version() string {
	return p.version
}

// SupportedProtocols 返回支持的协议列表
func (p *RDPPlugin) SupportedProtocols() []string {
	return []string{"rdp"}
}

// Description 返回插件描述
func (p *RDPPlugin) Description() string {
	return p.description
}

// Initialize 初始化插件 - 参考mayfly-go初始化流程
func (p *RDPPlugin) Initialize(config map[string]interface{}) error {
	// 安全检查，防止空指针
	if p == nil {
		return fmt.Errorf("RDP plugin is nil")
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.logger.Info("正在初始化基于Guacamole协议的RDP插件...")

	// 参数验证
	if config == nil {
		return fmt.Errorf("配置不能为nil")
	}

	// 使用defer确保在失败时清理资源
	defer func() {
		if r := recover(); r != nil {
			p.logger.Errorf("RDP插件初始化过程中发生panic: %v", r)
		}
	}()

	// 保存配置
	p.config = config

	// 解析RDP特定配置
	if err := p.parseConfig(config); err != nil {
		p.logger.Errorf("解析RDP配置失败: %v", err)
		return fmt.Errorf("解析RDP配置失败: %w", err)
	}

	// 验证关键配置
	if p.rdpConfig == nil {
		return fmt.Errorf("RDP配置解析后为nil")
	}

	// 重新创建 GuacdManager 以使用更新的配置
	if p.guacdManager != nil {
		if err := p.guacdManager.Close(); err != nil {
			p.logger.Errorf("关闭旧的GuacdManager失败: %v", err)
		}
	}

	// 创建新的GuacdManager
	p.guacdManager = NewGuacdManager(&p.rdpConfig.Guacd)
	if p.guacdManager == nil {
		return fmt.Errorf("创建GuacdManager失败")
	}

	// 初始化连接池 - 参考mayfly-go连接管理
	if p.rdpConfig.MaxConnections <= 0 {
		p.rdpConfig.MaxConnections = 20 // 默认值
		p.logger.Errorf("最大连接数配置无效，使用默认值: %d", p.rdpConfig.MaxConnections)
	}

	p.connectionPool = common.NewConnectionPool(p.rdpConfig.MaxConnections)
	if p.connectionPool == nil {
		return fmt.Errorf("创建连接池失败")
	}

	p.metricsCollector = common.NewMetricsCollector()
	if p.metricsCollector == nil {
		return fmt.Errorf("创建指标收集器失败")
	}

	p.logger.Infof("RDP插件初始化成功，最大连接数: %d", p.rdpConfig.MaxConnections)
	p.logger.Infof("Guacamole守护进程地址: %s", p.rdpConfig.Guacd.Address)

	// 简单验证Guacd地址格式
	if p.rdpConfig.Guacd.Address == "" {
		p.logger.Errorf("Guacd地址为空")
		p.logger.Errorf("RDP插件将在Guacd连接问题下启动，某些功能可能不可用")
	}

	return nil
}

// Start 启动插件 - 参考mayfly-go启动流程
func (p *RDPPlugin) Start() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.isRunning {
		return fmt.Errorf("RDP plugin is already running")
	}

	p.logger.Info("Starting RDP plugin with Guacamole protocol...")

	// 验证初始化状态
	if p.connectionPool == nil || p.guacdManager == nil {
		return fmt.Errorf("RDP plugin not properly initialized")
	}

	// 启动后台任务 - 参考mayfly-go后台服务
	go p.startBackgroundTasks()

	// 更新状态
	now := time.Now()
	p.startedAt = &now
	p.isRunning = true

	p.logger.Info("RDP plugin started successfully")
	return nil
}

// Stop 停止插件 - 参考mayfly-go停止流程
func (p *RDPPlugin) Stop() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.isRunning {
		return fmt.Errorf("RDP plugin is not running")
	}

	p.logger.Info("Stopping RDP plugin...")

	// 停止后台任务
	p.cancel()

	// 关闭所有连接和会话 - 参考mayfly-go资源清理
	if p.sessionStore != nil {
		p.sessionStore.CloseAll()
	}

	if p.tunnelManager != nil {
		p.tunnelManager.CloseAll()
	}

	if p.connectionPool != nil {
		p.connectionPool.Clear()
	}

	if p.guacdManager != nil {
		p.guacdManager.Close()
	}

	// 更新状态
	now := time.Now()
	p.stoppedAt = &now
	p.isRunning = false

	p.logger.Info("RDP plugin stopped successfully")
	return nil
}

// IsRunning 检查插件是否正在运行
func (p *RDPPlugin) IsRunning() bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.isRunning
}

// CreateConnection 创建RDP连接 - 参考mayfly-go的DoConnect实现
func (p *RDPPlugin) CreateConnection(ctx context.Context, target string, credentials *common.Credentials) (common.Connection, error) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if !p.isRunning {
		return nil, fmt.Errorf("RDP plugin is not running")
	}

	connectionId := p.idGenerator.Generate()
	p.logger.Infof("Creating RDP connection %s to %s", connectionId, target)

	// 创建RDP连接 - 使用Guacamole协议
	conn, err := p.createGuacamoleConnection(ctx, connectionId, target, credentials)
	if err != nil {
		p.logger.Errorf("Failed to create RDP connection %s: %v", connectionId, err)
		return nil, err
	}

	// 添加到连接池
	if err := p.connectionPool.Add(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to add connection to pool: %w", err)
	}

	// 更新指标
	p.metricsCollector.IncrementConnections()

	p.logger.Infof("RDP connection %s created successfully", connectionId)
	return conn, nil
}

// CloseConnection 关闭RDP连接 - 参考mayfly-go连接管理
func (p *RDPPlugin) CloseConnection(connectionId string) error {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	p.logger.Infof("Closing RDP connection %s", connectionId)

	// 从连接池移除
	conn, exists := p.connectionPool.Get(connectionId)
	if !exists {
		return fmt.Errorf("connection %s not found", connectionId)
	}

	// 关闭连接
	if err := conn.Close(); err != nil {
		p.logger.Errorf("Error closing RDP connection %s: %v", connectionId, err)
	}

	p.connectionPool.Remove(connectionId)

	// 更新指标
	p.metricsCollector.DecrementConnections()

	p.logger.Infof("RDP connection %s closed", connectionId)
	return nil
}

// GetConnection 获取RDP连接
func (p *RDPPlugin) GetConnection(connectionId string) (common.Connection, bool) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.connectionPool.Get(connectionId)
}

// ListConnections 列出所有连接ID
func (p *RDPPlugin) ListConnections() []string {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.connectionPool.List()
}

// GetStatus 获取插件状态 - 参考mayfly-go状态管理
func (p *RDPPlugin) GetStatus() *common.PluginStatus {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	status := "stopped"
	if p.isRunning {
		status = "running"
	}

	return &common.PluginStatus{
		Name:        p.name,
		Version:     p.version,
		Status:      status,
		StartedAt:   p.startedAt,
		StoppedAt:   p.stoppedAt,
		LoadedAt:    p.loadedAt,
		Connections: len(p.connectionPool.List()),
		Config:      p.config,
	}
}

// GetMetrics 获取插件指标
func (p *RDPPlugin) GetMetrics() *common.PluginMetrics {
	return p.metricsCollector.GetMetrics()
}

// UpdateConfig 更新配置 - 参考mayfly-go配置管理
func (p *RDPPlugin) UpdateConfig(config map[string]interface{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.logger.Info("Updating RDP plugin configuration...")

	// 验证新配置（简化实现）
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// 解析新配置
	newConfig := &RDPPluginConfig{}
	if err := p.parseConfigToStruct(config, newConfig); err != nil {
		return fmt.Errorf("failed to parse new config: %w", err)
	}

	// 应用新配置
	p.config = config
	p.rdpConfig = newConfig

	// 更新组件配置（简化实现）
	// 重新解析配置
	if err := p.parseConfig(config); err != nil {
		return fmt.Errorf("failed to parse updated config: %w", err)
	}

	// 重新创建GuacdManager以使用新配置
	if p.guacdManager != nil {
		p.guacdManager.Close()
	}
	p.guacdManager = NewGuacdManager(&p.rdpConfig.Guacd)

	p.logger.Info("RDP plugin configuration updated successfully")

	p.logger.Info("RDP plugin configuration updated successfully")
	return nil
}

// parseConfig 解析配置 - 参考mayfly-go配置解析
func (p *RDPPlugin) parseConfig(config map[string]interface{}) error {
	if len(config) == 0 {
		p.logger.Info("Using default RDP configuration")
		return nil
	}

	return p.parseConfigToStruct(config, p.rdpConfig)
}

// parseConfigToStruct 解析配置到结构体
func (p *RDPPlugin) parseConfigToStruct(config map[string]interface{}, target *RDPPluginConfig) error {
	p.logger.Infof("Parsing RDP configuration: %+v", config)

	// 解析 MaxConnections
	if maxConn, exists := config["MaxConnections"]; exists {
		if v, ok := maxConn.(int); ok {
			target.MaxConnections = v
		} else if v, ok := maxConn.(float64); ok {
			target.MaxConnections = int(v)
		}
	}

	// 解析 ConnectionTimeout
	if connTimeout, exists := config["ConnectionTimeout"]; exists {
		if v, ok := connTimeout.(int); ok {
			target.ConnectionTimeout = time.Duration(v) * time.Second
		} else if v, ok := connTimeout.(float64); ok {
			target.ConnectionTimeout = time.Duration(v) * time.Second
		}
	}

	// 解析 Guacd 配置
	if guacdConfig, exists := config["Guacd"]; exists {
		if guacdMap, ok := guacdConfig.(map[string]interface{}); ok {
			// 解析 Address
			if address, exists := guacdMap["Address"]; exists {
				if v, ok := address.(string); ok {
					target.Guacd.Address = v
					p.logger.Infof("设置 Guacd 地址: %s", v)
				}
			}

			// 解析 FallbackAddresses
			if fallbacks, exists := guacdMap["FallbackAddresses"]; exists {
				if fallbackList, ok := fallbacks.([]interface{}); ok {
					target.Guacd.FallbackAddresses = make([]string, 0, len(fallbackList))
					for _, fb := range fallbackList {
						if v, ok := fb.(string); ok {
							target.Guacd.FallbackAddresses = append(target.Guacd.FallbackAddresses, v)
						}
					}
					p.logger.Infof("设置 Guacd 备用地址: %v", target.Guacd.FallbackAddresses)
				}
			}

			// 解析 ConnectTimeout
			if connectTimeout, exists := guacdMap["ConnectTimeout"]; exists {
				if v, ok := connectTimeout.(string); ok {
					if duration, err := time.ParseDuration(v); err == nil {
						target.Guacd.ConnectTimeout = duration
					}
				} else if v, ok := connectTimeout.(int); ok {
					target.Guacd.ConnectTimeout = time.Duration(v) * time.Second
				} else if v, ok := connectTimeout.(float64); ok {
					target.Guacd.ConnectTimeout = time.Duration(v) * time.Second
				}
			}

			// 解析 HealthCheckInterval
			if healthInterval, exists := guacdMap["HealthCheckInterval"]; exists {
				if v, ok := healthInterval.(string); ok {
					if duration, err := time.ParseDuration(v); err == nil {
						target.Guacd.HealthCheckInterval = duration
					}
				} else if v, ok := healthInterval.(int); ok {
					target.Guacd.HealthCheckInterval = time.Duration(v) * time.Second
				} else if v, ok := healthInterval.(float64); ok {
					target.Guacd.HealthCheckInterval = time.Duration(v) * time.Second
				}
			}
		}
	}

	// 解析 Connection 配置
	if connectionConfig, exists := config["Connection"]; exists {
		if connMap, ok := connectionConfig.(map[string]interface{}); ok {
			// 解析 DefaultWidth
			if width, exists := connMap["DefaultWidth"]; exists {
				if v, ok := width.(int); ok {
					target.Connection.DefaultWidth = v
				} else if v, ok := width.(float64); ok {
					target.Connection.DefaultWidth = int(v)
				}
			}

			// 解析 DefaultHeight
			if height, exists := connMap["DefaultHeight"]; exists {
				if v, ok := height.(int); ok {
					target.Connection.DefaultHeight = v
				} else if v, ok := height.(float64); ok {
					target.Connection.DefaultHeight = int(v)
				}
			}

			// 解析 ColorDepth
			if colorDepth, exists := connMap["ColorDepth"]; exists {
				if v, ok := colorDepth.(int); ok {
					target.Connection.ColorDepth = v
				} else if v, ok := colorDepth.(float64); ok {
					target.Connection.ColorDepth = int(v)
				}
			}

			// 解析 DPI
			if dpi, exists := connMap["DPI"]; exists {
				if v, ok := dpi.(int); ok {
					target.Connection.DPI = v
				} else if v, ok := dpi.(float64); ok {
					target.Connection.DPI = int(v)
				}
			}
		}
	}

	p.logger.Infof("RDP配置解析完成: MaxConn=%d, Guacd=%s, Fallbacks=%v, ConnectTimeout=%v, HealthCheckInterval=%v",
		target.MaxConnections, target.Guacd.Address, target.Guacd.FallbackAddresses,
		target.Guacd.ConnectTimeout, target.Guacd.HealthCheckInterval)

	return nil
}

// startBackgroundTasks 启动后台任务 - 参考mayfly-go后台监控
func (p *RDPPlugin) startBackgroundTasks() {
	// 健康检查
	healthTicker := time.NewTicker(30 * time.Second)
	defer healthTicker.Stop()

	// 连接清理
	cleanupTicker := time.NewTicker(5 * time.Minute)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-healthTicker.C:
			p.performHealthCheck()
		case <-cleanupTicker.C:
			p.performConnectionCleanup()
		}
	}
}

// performHealthCheck 执行健康检查 - 参考mayfly-go健康检查
func (p *RDPPlugin) performHealthCheck() {
	if p.guacdManager != nil {
		p.guacdManager.performHealthCheck()
	}
}

// performConnectionCleanup 执行连接清理
func (p *RDPPlugin) performConnectionCleanup() {
	// 简化实现：检查连接池中的连接状态
	if p.connectionPool != nil {
		connections := p.connectionPool.List()
		for _, connId := range connections {
			if conn, exists := p.connectionPool.Get(connId); exists {
				if !conn.IsConnected() {
					p.connectionPool.Remove(connId)
					p.logger.Infof("Cleaned up disconnected RDP connection: %s", connId)
				}
			}
		}
	}
}

// createGuacamoleConnection 创建Guacamole RDP连接 - 基于mayfly-go的简化实现
func (p *RDPPlugin) createGuacamoleConnection(ctx context.Context, connectionId, target string, credentials *common.Credentials) (common.Connection, error) {
	p.logger.Infof("Creating RDP connection %s to %s", connectionId, target)

	// 直接使用新的GuacamoleConnection
	conn, err := NewGuacamoleConnection(connectionId, target, credentials, p.guacdManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create guacamole connection: %w", err)
	}

	p.logger.Infof("RDP connection %s created successfully", connectionId)
	return conn, nil
}

// CreateGuacamoleStream 创建Guacamole流连接 - 为WebSocket隧道使用
func (p *RDPPlugin) CreateGuacamoleStream(ctx context.Context, target string, credentials *common.Credentials, config *GuacamoleConfiguration) (*GuacamoleStream, error) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if !p.isRunning {
		return nil, fmt.Errorf("RDP plugin is not running")
	}

	host, port, err := parseTarget(target)
	if err != nil {
		return nil, fmt.Errorf("invalid target format: %w", err)
	}

	p.logger.Infof("Creating Guacamole stream for %s:%d", host, port)

	// 连接到Guacamole daemon
	guacdConn, err := p.guacdManager.GetConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to guacd: %w", err)
	}

	// 创建Guacamole流
	stream := NewGuacamoleStream(guacdConn, p.rdpConfig.ConnectionTimeout)
	stream.ConnectionID = config.ConnectionID

	// 执行Guacamole握手
	if err := stream.Handshake(config); err != nil {
		stream.Close()
		return nil, fmt.Errorf("Guacamole handshake failed: %w", err)
	}

	p.logger.Infof("Guacamole stream created successfully for %s", target)
	return stream, nil
}

// GetCurrentGuacdAddress 获取当前Guacamole守护进程地址
func (p *RDPPlugin) GetCurrentGuacdAddress() string {
	if p.guacdManager != nil {
		return p.guacdManager.GetConfig().Address
	}
	return "unknown"
}

// UpdateGuacdAddress 更新Guacamole守护进程地址（简化实现）
func (p *RDPPlugin) UpdateGuacdAddress(newAddress string) error {
	if p.rdpConfig != nil {
		p.rdpConfig.Guacd.Address = newAddress
		p.logger.Infof("Guacd地址已更新为: %s", newAddress)
		return nil
	}
	return fmt.Errorf("rdp config not initialized")
}

// UpdateGuacdFallbacks 更新Guacamole守护进程备用地址（简化实现）
func (p *RDPPlugin) UpdateGuacdFallbacks(fallbacks []string) error {
	if p.rdpConfig != nil {
		p.rdpConfig.Guacd.FallbackAddresses = fallbacks
		p.logger.Infof("Guacd备用地址已更新，数量: %d", len(fallbacks))
		return nil
	}
	return fmt.Errorf("rdp config not initialized")
}

// GetCurrentConfig 获取当前配置
func (p *RDPPlugin) GetCurrentConfig() interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.rdpConfig
}

// parseTarget 解析目标地址
func parseTarget(target string) (string, int, error) {
	if target == "" {
		return "", 0, fmt.Errorf("target cannot be empty")
	}

	// 检查是否包含端口
	if strings.Contains(target, ":") {
		host, portStr, err := net.SplitHostPort(target)
		if err != nil {
			return "", 0, fmt.Errorf("invalid target format: %w", err)
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			return "", 0, fmt.Errorf("invalid port: %w", err)
		}

		return host, port, nil
	}

	// 默认RDP端口
	return target, 3389, nil
}
