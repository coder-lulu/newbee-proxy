package websocket

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"newbee-agent/plugins/common/timeout"

	"github.com/gorilla/websocket"
)

// WebSocketManager WebSocket连接管理器
type WebSocketManager struct {
	// 连接存储
	connections map[string]*WebSocketConnection
	mutex       sync.RWMutex

	// 配置
	config            *WebSocketConfig
	timeoutCtrl       *timeout.TimeoutController
	upgrader          websocket.Upgrader
	messageHandler    MessageHandler
	connectHandler    ConnectHandler
	disconnectHandler DisconnectHandler

	// 控制
	ctx           context.Context
	cancel        context.CancelFunc
	cleanupTicker *time.Ticker

	// 日志
	logger Logger
}

// WebSocketConfig WebSocket配置
type WebSocketConfig struct {
	// 升级器配置
	ReadBufferSize    int  `json:"read_buffer_size"`   // 读缓冲区大小
	WriteBufferSize   int  `json:"write_buffer_size"`  // 写缓冲区大小
	EnableCompression bool `json:"enable_compression"` // 启用压缩
	CheckOrigin       bool `json:"check_origin"`       // 检查来源

	// 连接配置
	MaxConnections  int           `json:"max_connections"`  // 最大连接数
	MaxMessageSize  int64         `json:"max_message_size"` // 最大消息大小
	CleanupInterval time.Duration `json:"cleanup_interval"` // 清理间隔

	// 心跳配置
	EnableHeartbeat   bool          `json:"enable_heartbeat"`   // 启用心跳
	HeartbeatInterval time.Duration `json:"heartbeat_interval"` // 心跳间隔

	// 缓冲配置
	MessageQueueSize int `json:"message_queue_size"` // 消息队列大小
}

// DefaultWebSocketConfig 返回默认WebSocket配置
func DefaultWebSocketConfig() *WebSocketConfig {
	return &WebSocketConfig{
		ReadBufferSize:    4096,
		WriteBufferSize:   4096,
		EnableCompression: false,
		CheckOrigin:       false,
		MaxConnections:    1000,
		MaxMessageSize:    1024 * 1024, // 1MB
		CleanupInterval:   5 * time.Minute,
		EnableHeartbeat:   true,
		HeartbeatInterval: 30 * time.Second,
		MessageQueueSize:  100,
	}
}

// MessageHandler 消息处理器接口
type MessageHandler interface {
	HandleMessage(conn *WebSocketConnection, messageType int, data []byte) error
}

// ConnectHandler 连接处理器接口
type ConnectHandler interface {
	OnConnect(conn *WebSocketConnection) error
}

// DisconnectHandler 断开连接处理器接口
type DisconnectHandler interface {
	OnDisconnect(conn *WebSocketConnection, reason string)
}

// MessageHandlerFunc 消息处理器函数类型
type MessageHandlerFunc func(conn *WebSocketConnection, messageType int, data []byte) error

// ConnectHandlerFunc 连接处理器函数类型
type ConnectHandlerFunc func(conn *WebSocketConnection) error

// DisconnectHandlerFunc 断开连接处理器函数类型
type DisconnectHandlerFunc func(conn *WebSocketConnection, reason string)

// 实现接口
func (f MessageHandlerFunc) HandleMessage(conn *WebSocketConnection, messageType int, data []byte) error {
	return f(conn, messageType, data)
}

func (f ConnectHandlerFunc) OnConnect(conn *WebSocketConnection) error {
	return f(conn)
}

func (f DisconnectHandlerFunc) OnDisconnect(conn *WebSocketConnection, reason string) {
	f(conn, reason)
}

// NewWebSocketManager 创建WebSocket管理器
func NewWebSocketManager(config *WebSocketConfig, timeoutConfig *timeout.TimeoutConfig) *WebSocketManager {
	if config == nil {
		config = DefaultWebSocketConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 创建升级器
	upgrader := websocket.Upgrader{
		ReadBufferSize:    config.ReadBufferSize,
		WriteBufferSize:   config.WriteBufferSize,
		EnableCompression: config.EnableCompression,
		CheckOrigin: func(r *http.Request) bool {
			return !config.CheckOrigin // 如果不检查来源，则总是返回true
		},
	}

	manager := &WebSocketManager{
		connections:   make(map[string]*WebSocketConnection),
		config:        config,
		timeoutCtrl:   timeout.NewTimeoutController(timeoutConfig),
		upgrader:      upgrader,
		ctx:           ctx,
		cancel:        cancel,
		cleanupTicker: time.NewTicker(config.CleanupInterval),
		logger:        DefaultLogger,
	}

	// 启动清理协程
	go manager.cleanupRoutine()

	return manager
}

// SetMessageHandler 设置消息处理器
func (wm *WebSocketManager) SetMessageHandler(handler MessageHandler) {
	wm.messageHandler = handler
}

// SetConnectHandler 设置连接处理器
func (wm *WebSocketManager) SetConnectHandler(handler ConnectHandler) {
	wm.connectHandler = handler
}

// SetDisconnectHandler 设置断开连接处理器
func (wm *WebSocketManager) SetDisconnectHandler(handler DisconnectHandler) {
	wm.disconnectHandler = handler
}

// UpgradeConnection 升级HTTP连接为WebSocket
func (wm *WebSocketManager) UpgradeConnection(w http.ResponseWriter, r *http.Request, sessionID string, metadata map[string]string) (*WebSocketConnection, error) {
	// 检查连接数限制
	wm.mutex.RLock()
	if len(wm.connections) >= wm.config.MaxConnections {
		wm.mutex.RUnlock()
		return nil, fmt.Errorf("maximum connections limit reached: %d", wm.config.MaxConnections)
	}
	wm.mutex.RUnlock()

	// 升级连接
	conn, err := wm.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade websocket connection: %w", err)
	}

	// 创建WebSocket连接对象
	wsConn := &WebSocketConnection{
		ID:           sessionID,
		wsConn:       conn,
		request:      r,
		manager:      wm,
		metadata:     metadata,
		messageChan:  make(chan *Message, wm.config.MessageQueueSize),
		sendChan:     make(chan *Message, wm.config.MessageQueueSize),
		stopChan:     make(chan struct{}),
		createdAt:    time.Now(),
		lastActiveAt: time.Now(),
		logger:       wm.logger,
	}

	// 设置连接参数
	conn.SetReadLimit(wm.config.MaxMessageSize)

	// 设置超时
	timeouts := wm.timeoutCtrl.GetWebSocketTimeouts()
	conn.SetReadDeadline(time.Now().Add(timeouts.PingTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(timeouts.PingTimeout))
		wsConn.updateLastActiveTime()
		return nil
	})

	// 添加到管理器
	wm.mutex.Lock()
	wm.connections[sessionID] = wsConn
	wm.mutex.Unlock()

	// 启动连接处理
	go wsConn.startReading()
	go wsConn.startWriting()
	if wm.config.EnableHeartbeat {
		go wsConn.startHeartbeat()
	}

	// 调用连接处理器
	if wm.connectHandler != nil {
		if err := wm.connectHandler.OnConnect(wsConn); err != nil {
			wm.RemoveConnection(sessionID)
			return nil, fmt.Errorf("connect handler failed: %w", err)
		}
	}

	wm.logger.Infof("WebSocket connection established: %s", sessionID)
	return wsConn, nil
}

// GetConnection 获取连接
func (wm *WebSocketManager) GetConnection(sessionID string) (*WebSocketConnection, bool) {
	wm.mutex.RLock()
	defer wm.mutex.RUnlock()

	conn, exists := wm.connections[sessionID]
	return conn, exists
}

// RemoveConnection 移除连接
func (wm *WebSocketManager) RemoveConnection(sessionID string) {
	wm.mutex.Lock()
	conn, exists := wm.connections[sessionID]
	if exists {
		delete(wm.connections, sessionID)
	}
	wm.mutex.Unlock()

	if exists {
		conn.close("connection removed")
		wm.logger.Infof("WebSocket connection removed: %s", sessionID)
	}
}

// ListConnections 列出所有连接ID
func (wm *WebSocketManager) ListConnections() []string {
	wm.mutex.RLock()
	defer wm.mutex.RUnlock()

	ids := make([]string, 0, len(wm.connections))
	for id := range wm.connections {
		ids = append(ids, id)
	}
	return ids
}

// GetConnectionCount 获取连接数
func (wm *WebSocketManager) GetConnectionCount() int {
	wm.mutex.RLock()
	defer wm.mutex.RUnlock()
	return len(wm.connections)
}

// BroadcastMessage 广播消息到所有连接
func (wm *WebSocketManager) BroadcastMessage(message *Message) {
	wm.mutex.RLock()
	connections := make([]*WebSocketConnection, 0, len(wm.connections))
	for _, conn := range wm.connections {
		connections = append(connections, conn)
	}
	wm.mutex.RUnlock()

	for _, conn := range connections {
		conn.SendMessage(message)
	}
}

// GetConnectionStats 获取连接统计
func (wm *WebSocketManager) GetConnectionStats() map[string]interface{} {
	wm.mutex.RLock()
	defer wm.mutex.RUnlock()

	stats := map[string]interface{}{
		"total_connections":  len(wm.connections),
		"max_connections":    wm.config.MaxConnections,
		"heartbeat_enabled":  wm.config.EnableHeartbeat,
		"heartbeat_interval": wm.config.HeartbeatInterval.String(),
		"cleanup_interval":   wm.config.CleanupInterval.String(),
	}

	// 连接详情
	connections := make([]map[string]interface{}, 0, len(wm.connections))
	for id, conn := range wm.connections {
		connections = append(connections, map[string]interface{}{
			"id":            id,
			"created_at":    conn.createdAt,
			"last_active":   conn.lastActiveAt,
			"idle_duration": time.Since(conn.lastActiveAt).String(),
			"remote_addr":   conn.getRemoteAddr(),
			"user_agent":    conn.getUserAgent(),
		})
	}
	stats["connections"] = connections

	return stats
}

// Shutdown 关闭管理器
func (wm *WebSocketManager) Shutdown() error {
	wm.logger.Info("Shutting down WebSocket manager...")

	// 停止清理协程
	wm.cancel()
	wm.cleanupTicker.Stop()

	// 关闭所有连接
	wm.mutex.Lock()
	connections := make([]*WebSocketConnection, 0, len(wm.connections))
	for _, conn := range wm.connections {
		connections = append(connections, conn)
	}
	wm.connections = make(map[string]*WebSocketConnection)
	wm.mutex.Unlock()

	for _, conn := range connections {
		conn.close("manager shutdown")
	}

	wm.logger.Infof("WebSocket manager shutdown completed, closed %d connections", len(connections))
	return nil
}

// cleanupRoutine 清理协程
func (wm *WebSocketManager) cleanupRoutine() {
	for {
		select {
		case <-wm.ctx.Done():
			return
		case <-wm.cleanupTicker.C:
			wm.cleanup()
		}
	}
}

// cleanup 清理过期连接
func (wm *WebSocketManager) cleanup() {
	wm.mutex.Lock()
	defer wm.mutex.Unlock()

	now := time.Now()
	idleTimeout := wm.timeoutCtrl.GetIdleTimeout()
	var expiredIDs []string

	for id, conn := range wm.connections {
		if now.Sub(conn.lastActiveAt) > idleTimeout {
			expiredIDs = append(expiredIDs, id)
		}
	}

	// 移除过期连接
	for _, id := range expiredIDs {
		if conn := wm.connections[id]; conn != nil {
			delete(wm.connections, id)
			go conn.close("idle timeout")
		}
	}

	if len(expiredIDs) > 0 {
		wm.logger.Infof("Cleaned up %d expired WebSocket connections", len(expiredIDs))
	}
}

// handleMessage 处理消息（内部方法）
func (wm *WebSocketManager) handleMessage(conn *WebSocketConnection, messageType int, data []byte) {
	if wm.messageHandler != nil {
		if err := wm.messageHandler.HandleMessage(conn, messageType, data); err != nil {
			wm.logger.Errorf("Message handler error for connection %s: %v", conn.ID, err)
		}
	}
}

// handleDisconnect 处理断开连接（内部方法）
func (wm *WebSocketManager) handleDisconnect(conn *WebSocketConnection, reason string) {
	if wm.disconnectHandler != nil {
		wm.disconnectHandler.OnDisconnect(conn, reason)
	}
}
