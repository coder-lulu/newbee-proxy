package ws

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"newbee-agent/plugins/common"

	"github.com/zeromicro/go-zero/core/logx"
)

// WSPlugin WebSocket插件实现
type WSPlugin struct {
	name        string
	version     string
	status      string
	config      map[string]interface{}
	manager     *ClientManager
	connections map[string]*WSConnection
	mutex       sync.RWMutex
	logger      logx.Logger
	startedAt   *time.Time
	stoppedAt   *time.Time
	loadedAt    time.Time
}

// WSConnection WebSocket连接包装器
type WSConnection struct {
	id           string
	target       string
	client       *Client
	status       common.ConnectionStatus
	createdAt    time.Time
	lastActiveAt time.Time
	metadata     map[string]string
	mutex        sync.RWMutex
}

// NewWSPlugin 创建新的WebSocket插件实例
func NewWSPlugin() *WSPlugin {
	return &WSPlugin{
		name:        "websocket",
		version:     "1.0.0",
		status:      "stopped",
		config:      make(map[string]interface{}),
		connections: make(map[string]*WSConnection),
		logger:      logx.WithContext(context.Background()),
		loadedAt:    time.Now(),
	}
}

// Name 插件名称
func (p *WSPlugin) Name() string {
	return p.name
}

// Version 插件版本
func (p *WSPlugin) Version() string {
	return p.version
}

// SupportedProtocols 支持的协议
func (p *WSPlugin) SupportedProtocols() []string {
	return []string{"websocket"}
}

// Description 插件描述
func (p *WSPlugin) Description() string {
	return "WebSocket management plugin for real-time communication"
}

// Initialize 初始化插件
func (p *WSPlugin) Initialize(config map[string]interface{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.config = config
	p.manager = NewClientManager()

	p.logger.Info("WebSocket plugin initialized")
	return nil
}

// Start 启动插件
func (p *WSPlugin) Start() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.status == "running" {
		return fmt.Errorf("plugin already running")
	}

	// 启动ClientManager
	go p.manager.Start()

	now := time.Now()
	p.startedAt = &now
	p.status = "running"

	p.logger.Info("WebSocket plugin started")
	return nil
}

// Stop 停止插件
func (p *WSPlugin) Stop() error {
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

	now := time.Now()
	p.stoppedAt = &now
	p.status = "stopped"

	p.logger.Info("WebSocket plugin stopped")
	return nil
}

// IsRunning 检查插件是否正在运行
func (p *WSPlugin) IsRunning() bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.status == "running"
}

// CreateConnection 创建新连接
func (p *WSPlugin) CreateConnection(ctx context.Context, target string, credentials *common.Credentials) (common.Connection, error) {
	if !p.IsRunning() {
		return nil, fmt.Errorf("plugin not running")
	}

	// 为WebSocket连接创建连接对象
	connID := fmt.Sprintf("ws_%d_%d", time.Now().UnixNano(), len(p.connections))
	conn := &WSConnection{
		id:           connID,
		target:       target,
		status:       common.StatusConnected,
		createdAt:    time.Now(),
		lastActiveAt: time.Now(),
		metadata:     make(map[string]string),
	}

	p.mutex.Lock()
	p.connections[connID] = conn
	p.mutex.Unlock()

	p.logger.Infof("Created WebSocket connection: %s -> %s", connID, target)
	return conn, nil
}

// CloseConnection 关闭连接
func (p *WSPlugin) CloseConnection(connectionId string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	conn, exists := p.connections[connectionId]
	if !exists {
		return fmt.Errorf("connection not found: %s", connectionId)
	}

	err := conn.Close()
	delete(p.connections, connectionId)

	p.logger.Infof("Closed WebSocket connection: %s", connectionId)
	return err
}

// GetConnection 获取连接
func (p *WSPlugin) GetConnection(connectionId string) (common.Connection, bool) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	conn, exists := p.connections[connectionId]
	return conn, exists
}

// ListConnections 列出所有连接
func (p *WSPlugin) ListConnections() []string {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	var ids []string
	for id := range p.connections {
		ids = append(ids, id)
	}
	return ids
}

// GetStatus 获取插件状态
func (p *WSPlugin) GetStatus() *common.PluginStatus {
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
		Sessions:    len(p.connections),
	}
}

// GetMetrics 获取插件指标
func (p *WSPlugin) GetMetrics() *common.PluginMetrics {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return &common.PluginMetrics{
		ActiveConnections: len(p.connections),
		ActiveSessions:    len(p.connections),
		UpdatedAt:         time.Now(),
	}
}

// UpdateConfig 更新配置
func (p *WSPlugin) UpdateConfig(config map[string]interface{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.config = config
	p.logger.Info("WebSocket plugin configuration updated")
	return nil
}

// GetManager 获取ClientManager实例
func (p *WSPlugin) GetManager() *ClientManager {
	return p.manager
}

// WSConnection 方法实现

// ID 连接ID
func (c *WSConnection) ID() string {
	return c.id
}

// Target 目标地址
func (c *WSConnection) Target() string {
	return c.target
}

// Protocol 协议
func (c *WSConnection) Protocol() string {
	return "websocket"
}

// Status 连接状态
func (c *WSConnection) Status() common.ConnectionStatus {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.status
}

// CreatedAt 创建时间
func (c *WSConnection) CreatedAt() time.Time {
	return c.createdAt
}

// LastActiveAt 最后活跃时间
func (c *WSConnection) LastActiveAt() time.Time {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.lastActiveAt
}

// Write 写入数据
func (c *WSConnection) Write(data []byte) (int, error) {
	c.mutex.Lock()
	c.lastActiveAt = time.Now()
	c.mutex.Unlock()

	if c.client == nil {
		return 0, fmt.Errorf("client not available")
	}

	// 通过WebSocket客户端发送数据
	msg := &Msg{
		Data: data,
		Type: BinaryMsg,
	}

	err := c.client.WriteMsg(msg)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

// Read 读取数据
func (c *WSConnection) Read(data []byte) (int, error) {
	c.mutex.Lock()
	c.lastActiveAt = time.Now()
	c.mutex.Unlock()

	// WebSocket是异步的，这里返回空数据
	// 实际数据通过消息处理器处理
	return 0, nil
}

// SetWebSocketWriter 设置WebSocket写入器
func (c *WSConnection) SetWebSocketWriter(writer io.Writer) error {
	// WebSocket连接本身就是写入器
	return nil
}

// SetWebSocketReader 设置WebSocket读取器
func (c *WSConnection) SetWebSocketReader(reader io.Reader) error {
	// WebSocket连接本身就是读取器
	return nil
}

// Close 关闭连接
func (c *WSConnection) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.status = common.StatusDisconnected

	if c.client != nil {
		// 通过Manager关闭客户端
		Manager.CloseClient(c.client)
	}
	return nil
}

// IsConnected 检查是否已连接
func (c *WSConnection) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.status == common.StatusConnected
}

// GetSession 获取会话
func (c *WSConnection) GetSession() common.Session {
	// WebSocket的会话概念由连接本身管理
	return nil
}

// GetMetadata 获取元数据
func (c *WSConnection) GetMetadata() map[string]string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	result := make(map[string]string)
	for k, v := range c.metadata {
		result[k] = v
	}
	return result
}

// SetMetadata 设置元数据
func (c *WSConnection) SetMetadata(key, value string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.metadata[key] = value
}

// SetClient 设置WebSocket客户端
func (c *WSConnection) SetClient(client *Client) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.client = client
}
