package guac

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

    "github.com/coder-lulu/newbee-proxy/plugins/common"

	"github.com/zeromicro/go-zero/core/logx"
)

// GuacPlugin Guacamole协议插件实现
type GuacPlugin struct {
	name        string
	version     string
	status      string
	config      map[string]interface{}
	connections map[string]*GuacConnection
	tunnelMap   *TunnelMap
	server      *Server
	mutex       sync.RWMutex
	logger      logx.Logger
	startedAt   *time.Time
	stoppedAt   *time.Time
	loadedAt    time.Time
}

// GuacConnection Guacamole连接实现
type GuacConnection struct {
	id           string
	target       string
	tunnel       Tunnel
	status       common.ConnectionStatus
	createdAt    time.Time
	lastActiveAt time.Time
	metadata     map[string]string
	mutex        sync.RWMutex
}

// NewGuacPlugin 创建新的Guacamole插件实例
func NewGuacPlugin() *GuacPlugin {
	return &GuacPlugin{
		name:        "guacamole",
		version:     "1.0.0",
		status:      "stopped",
		config:      make(map[string]interface{}),
		connections: make(map[string]*GuacConnection),
		logger:      logx.WithContext(context.Background()),
		loadedAt:    time.Now(),
	}
}

// Name 插件名称
func (p *GuacPlugin) Name() string {
	return p.name
}

// Version 插件版本
func (p *GuacPlugin) Version() string {
	return p.version
}

// SupportedProtocols 支持的协议
func (p *GuacPlugin) SupportedProtocols() []string {
	return []string{"rdp", "vnc", "ssh", "telnet"}
}

// Description 插件描述
func (p *GuacPlugin) Description() string {
	return "Guacamole protocol plugin for remote desktop connections"
}

// Initialize 初始化插件
func (p *GuacPlugin) Initialize(config map[string]interface{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.config = config
	p.tunnelMap = NewTunnelMap()

	// 创建服务器
	p.server = NewServer(p.createTunnelForServer)

	p.logger.Info("Guacamole plugin initialized")
	return nil
}

// Start 启动插件
func (p *GuacPlugin) Start() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.status == "running" {
		return fmt.Errorf("plugin already running")
	}

	now := time.Now()
	p.startedAt = &now
	p.status = "running"

	p.logger.Info("Guacamole plugin started")
	return nil
}

// Stop 停止插件
func (p *GuacPlugin) Stop() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.status != "running" {
		return fmt.Errorf("plugin not running")
	}

	// 关闭所有连接
	for id, conn := range p.connections {
		conn.Close()
		delete(p.connections, id)
	}

	// 关闭隧道映射
	if p.tunnelMap != nil {
		p.tunnelMap.Shutdown()
	}

	now := time.Now()
	p.stoppedAt = &now
	p.status = "stopped"

	p.logger.Info("Guacamole plugin stopped")
	return nil
}

// IsRunning 检查插件是否正在运行
func (p *GuacPlugin) IsRunning() bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.status == "running"
}

// CreateConnection 创建新连接
func (p *GuacPlugin) CreateConnection(ctx context.Context, target string, credentials *common.Credentials) (common.Connection, error) {
	if !p.IsRunning() {
		return nil, fmt.Errorf("plugin not running")
	}

	// 解析参数和查询
	query := make(map[string][]string)
	query["width"] = []string{"800"}
	query["height"] = []string{"600"}

	parameters := map[string]string{
		"scheme":   "rdp",
		"hostname": target,
		"port":     "3389",
		"username": credentials.Username,
		"password": credentials.Password,
	}

	// 创建隧道
	tunnel, err := DoConnect(query, parameters, credentials.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to create tunnel: %v", err)
	}

	// 创建连接对象
	connID := fmt.Sprintf("guac_%d", time.Now().UnixNano())
	conn := &GuacConnection{
		id:           connID,
		target:       target,
		tunnel:       tunnel,
		status:       common.StatusConnected,
		createdAt:    time.Now(),
		lastActiveAt: time.Now(),
		metadata:     make(map[string]string),
	}

	p.mutex.Lock()
	p.connections[connID] = conn
	p.mutex.Unlock()

	p.logger.Infof("Created Guacamole connection: %s -> %s", connID, target)
	return conn, nil
}

// CloseConnection 关闭连接
func (p *GuacPlugin) CloseConnection(connectionId string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	conn, exists := p.connections[connectionId]
	if !exists {
		return fmt.Errorf("connection not found: %s", connectionId)
	}

	err := conn.Close()
	delete(p.connections, connectionId)

	p.logger.Infof("Closed Guacamole connection: %s", connectionId)
	return err
}

// GetConnection 获取连接
func (p *GuacPlugin) GetConnection(connectionId string) (common.Connection, bool) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	conn, exists := p.connections[connectionId]
	return conn, exists
}

// ListConnections 列出所有连接
func (p *GuacPlugin) ListConnections() []string {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	var ids []string
	for id := range p.connections {
		ids = append(ids, id)
	}
	return ids
}

// GetStatus 获取插件状态
func (p *GuacPlugin) GetStatus() *common.PluginStatus {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return &common.PluginStatus{
		Name:        p.name,
		Version:     p.version,
		Status:      p.status,
		LoadedAt:    p.loadedAt,
		StartedAt:   p.startedAt,
		StoppedAt:   p.stoppedAt,
		Config:      p.config,
		Connections: len(p.connections),
		Sessions:    len(p.connections), // 简化：假设每个连接一个会话
	}
}

// GetMetrics 获取插件指标
func (p *GuacPlugin) GetMetrics() *common.PluginMetrics {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return &common.PluginMetrics{
		ActiveConnections: len(p.connections),
		ActiveSessions:    len(p.connections),
		UpdatedAt:         time.Now(),
	}
}

// UpdateConfig 更新配置
func (p *GuacPlugin) UpdateConfig(config map[string]interface{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.config = config
	p.logger.Info("Guacamole plugin configuration updated")
	return nil
}

// createTunnelForServer 内部方法：为Server接口创建隧道
func (p *GuacPlugin) createTunnelForServer(r *http.Request) (Tunnel, error) {
	query := r.URL.Query()

	parameters := map[string]string{
		"scheme":   "rdp",
		"hostname": query.Get("hostname"),
		"port":     query.Get("port"),
		"username": query.Get("username"),
		"password": query.Get("password"),
	}

	if parameters["port"] == "" {
		parameters["port"] = "3389"
	}

	username := parameters["username"]
	return DoConnect(query, parameters, username)
}

// GuacConnection 方法实现

// ID 连接ID
func (c *GuacConnection) ID() string {
	return c.id
}

// Target 目标地址
func (c *GuacConnection) Target() string {
	return c.target
}

// Protocol 协议
func (c *GuacConnection) Protocol() string {
	return "rdp"
}

// Status 连接状态
func (c *GuacConnection) Status() common.ConnectionStatus {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.status
}

// CreatedAt 创建时间
func (c *GuacConnection) CreatedAt() time.Time {
	return c.createdAt
}

// LastActiveAt 最后活跃时间
func (c *GuacConnection) LastActiveAt() time.Time {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.lastActiveAt
}

// Write 写入数据
func (c *GuacConnection) Write(data []byte) (int, error) {
	c.mutex.Lock()
	c.lastActiveAt = time.Now()
	c.mutex.Unlock()

	if c.tunnel == nil {
		return 0, fmt.Errorf("tunnel not available")
	}

	writer := c.tunnel.AcquireWriter()
	defer c.tunnel.ReleaseWriter()

	return writer.Write(data)
}

// Read 读取数据
func (c *GuacConnection) Read(data []byte) (int, error) {
	c.mutex.Lock()
	c.lastActiveAt = time.Now()
	c.mutex.Unlock()

	if c.tunnel == nil {
		return 0, fmt.Errorf("tunnel not available")
	}

	reader := c.tunnel.AcquireReader()
	defer c.tunnel.ReleaseReader()

	// InstructionReader使用ReadSome方法而不是Read
	instruction, err := reader.ReadSome()
	if err != nil {
		return 0, err
	}

	n := copy(data, instruction)
	return n, nil
}

// SetWebSocketWriter 设置WebSocket写入器
func (c *GuacConnection) SetWebSocketWriter(writer io.Writer) error {
	// Guacamole协议通过tunnel处理WebSocket
	return nil
}

// SetWebSocketReader 设置WebSocket读取器
func (c *GuacConnection) SetWebSocketReader(reader io.Reader) error {
	// Guacamole协议通过tunnel处理WebSocket
	return nil
}

// Close 关闭连接
func (c *GuacConnection) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.status = common.StatusDisconnected

	if c.tunnel != nil {
		return c.tunnel.Close()
	}
	return nil
}

// IsConnected 检查是否已连接
func (c *GuacConnection) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.status == common.StatusConnected
}

// GetSession 获取会话
func (c *GuacConnection) GetSession() common.Session {
	// Guacamole协议的会话概念由tunnel管理
	return nil
}

// GetMetadata 获取元数据
func (c *GuacConnection) GetMetadata() map[string]string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	result := make(map[string]string)
	for k, v := range c.metadata {
		result[k] = v
	}
	return result
}

// SetMetadata 设置元数据
func (c *GuacConnection) SetMetadata(key, value string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.metadata[key] = value
}
