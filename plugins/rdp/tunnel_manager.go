package rdp

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
)

// Tunnel 隧道接口 - 参考mayfly-go的Tunnel设计
type Tunnel interface {
	io.ReadWriteCloser
	GetUUID() string
	ConnectionID() string
	IsOpen() bool
	GetLastActivity() time.Time
	UpdateActivity()
}

// SimpleTunnel 简单隧道实现 - 参考mayfly-go SimpleTunnel
type SimpleTunnel struct {
	uuid         string
	connectionID string
	stream       *GuacamoleStream
	isOpen       bool
	lastActivity time.Time
	mutex        sync.RWMutex
	readerLock   CountedLock
	writerLock   CountedLock
}

// CountedLock 计数锁 - 参考mayfly-go的锁机制
type CountedLock struct {
	mutex  sync.Mutex
	queued int
}

func (cl *CountedLock) Lock() {
	cl.mutex.Lock()
}

func (cl *CountedLock) Unlock() {
	cl.mutex.Unlock()
}

func (cl *CountedLock) HasQueued() bool {
	return cl.queued > 0
}

// NewSimpleTunnel 创建简单隧道 - 参考mayfly-go的构造方式
func NewSimpleTunnel(uuid, connectionID string, stream *GuacamoleStream) *SimpleTunnel {
	return &SimpleTunnel{
		uuid:         uuid,
		connectionID: connectionID,
		stream:       stream,
		isOpen:       true,
		lastActivity: time.Now(),
	}
}

func (t *SimpleTunnel) GetUUID() string {
	return t.uuid
}

func (t *SimpleTunnel) ConnectionID() string {
	return t.connectionID
}

func (t *SimpleTunnel) IsOpen() bool {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.isOpen
}

func (t *SimpleTunnel) GetLastActivity() time.Time {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.lastActivity
}

func (t *SimpleTunnel) UpdateActivity() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.lastActivity = time.Now()
}

func (t *SimpleTunnel) Read(p []byte) (n int, err error) {
	t.readerLock.Lock()
	defer t.readerLock.Unlock()

	if !t.isOpen {
		return 0, io.EOF
	}

	// 使用ReadSome方法
	data, err := t.stream.ReadSome()
	if err != nil {
		return 0, err
	}

	n = copy(p, data)
	if n > 0 {
		t.UpdateActivity()
	}
	return n, nil
}

func (t *SimpleTunnel) Write(p []byte) (n int, err error) {
	t.writerLock.Lock()
	defer t.writerLock.Unlock()

	if !t.isOpen {
		return 0, io.ErrClosedPipe
	}

	n, err = t.stream.Write(p)
	if n > 0 {
		t.UpdateActivity()
	}
	return
}

func (t *SimpleTunnel) Close() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if !t.isOpen {
		return nil
	}

	t.isOpen = false
	if t.stream != nil {
		return t.stream.Close()
	}
	return nil
}

// TunnelManager 隧道管理器 - 参考mayfly-go的TunnelMap
type TunnelManager struct {
	tunnels     map[string]Tunnel
	mutex       sync.RWMutex
	timeout     time.Duration
	cleanupStop chan struct{}
	logger      logx.Logger
}

// NewTunnelManager 创建隧道管理器
func NewTunnelManager() *TunnelManager {
	tm := &TunnelManager{
		tunnels:     make(map[string]Tunnel),
		timeout:     15 * time.Minute, // 15分钟超时
		cleanupStop: make(chan struct{}),
		logger:      logx.WithContext(nil),
	}

	// 启动清理协程
	go tm.cleanupRoutine()

	return tm
}

// Add 添加隧道
func (tm *TunnelManager) Add(uuid string, tunnel Tunnel) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	tm.tunnels[uuid] = tunnel
	tm.logger.Infof("Added tunnel: %s", uuid)
}

// Get 获取隧道
func (tm *TunnelManager) Get(uuid string) (Tunnel, bool) {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	tunnel, exists := tm.tunnels[uuid]
	if exists && tunnel.IsOpen() {
		tunnel.UpdateActivity()
	}
	return tunnel, exists
}

// Remove 移除隧道
func (tm *TunnelManager) Remove(uuid string) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	if tunnel, exists := tm.tunnels[uuid]; exists {
		tunnel.Close()
		delete(tm.tunnels, uuid)
		tm.logger.Infof("Removed tunnel: %s", uuid)
	}
}

// List 列出所有隧道
func (tm *TunnelManager) List() []string {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	uuids := make([]string, 0, len(tm.tunnels))
	for uuid := range tm.tunnels {
		uuids = append(uuids, uuid)
	}
	return uuids
}

// CloseAll 关闭所有隧道
func (tm *TunnelManager) CloseAll() {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	close(tm.cleanupStop)

	for uuid, tunnel := range tm.tunnels {
		tunnel.Close()
		tm.logger.Infof("Closed tunnel: %s", uuid)
	}

	tm.tunnels = make(map[string]Tunnel)
	tm.logger.Info("Closed all tunnels")
}

// cleanupRoutine 清理过期隧道 - 参考mayfly-go的清理机制
func (tm *TunnelManager) cleanupRoutine() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-tm.cleanupStop:
			return
		case <-ticker.C:
			tm.cleanupExpiredTunnels()
		}
	}
}

// cleanupExpiredTunnels 清理过期隧道
func (tm *TunnelManager) cleanupExpiredTunnels() {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	now := time.Now()
	expired := make([]string, 0)

	for uuid, tunnel := range tm.tunnels {
		if !tunnel.IsOpen() || now.Sub(tunnel.GetLastActivity()) > tm.timeout {
			expired = append(expired, uuid)
		}
	}

	for _, uuid := range expired {
		if tunnel := tm.tunnels[uuid]; tunnel != nil {
			tunnel.Close()
			delete(tm.tunnels, uuid)
			tm.logger.Infof("Cleaned up expired tunnel: %s", uuid)
		}
	}
}

// GetStats 获取统计信息
func (tm *TunnelManager) GetStats() map[string]interface{} {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	return map[string]interface{}{
		"total_tunnels": len(tm.tunnels),
		"timeout":       tm.timeout.String(),
	}
}

// WebSocketTunnel WebSocket隧道实现 - 参考mayfly-go的WebSocket处理
type WebSocketTunnel struct {
	*SimpleTunnel
	wsConn *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	logger logx.Logger
}

// NewWebSocketTunnel 创建WebSocket隧道
func NewWebSocketTunnel(uuid, connectionID string, wsConn *websocket.Conn, stream *GuacamoleStream) *WebSocketTunnel {
	ctx, cancel := context.WithCancel(context.Background())

	return &WebSocketTunnel{
		SimpleTunnel: NewSimpleTunnel(uuid, connectionID, stream),
		wsConn:       wsConn,
		ctx:          ctx,
		cancel:       cancel,
		logger:       logx.WithContext(ctx),
	}
}

// StartForwarding 开始数据转发 - 参考mayfly-go的WsToGuacd和GuacdToWs
func (wt *WebSocketTunnel) StartForwarding() {
	go wt.wsToGuacd()
	go wt.guacdToWs()
}

// wsToGuacd WebSocket到Guacamole的数据转发
func (wt *WebSocketTunnel) wsToGuacd() {
	defer wt.cancel()

	for {
		select {
		case <-wt.ctx.Done():
			return
		default:
		}

		_, data, err := wt.wsConn.ReadMessage()
		if err != nil {
			wt.logger.Errorf("Error reading from WebSocket: %v", err)
			return
		}

		if _, err := wt.stream.Write(data); err != nil {
			wt.logger.Errorf("Error writing to Guacamole: %v", err)
			return
		}

		wt.UpdateActivity()
	}
}

// guacdToWs Guacamole到WebSocket的数据转发
func (wt *WebSocketTunnel) guacdToWs() {
	defer wt.cancel()

	for {
		select {
		case <-wt.ctx.Done():
			return
		default:
		}

		data, err := wt.stream.ReadSome()
		if err != nil {
			if err != io.EOF {
				wt.logger.Errorf("Error reading from Guacamole: %v", err)
			}
			return
		}

		if len(data) > 0 {
			if err := wt.wsConn.WriteMessage(websocket.TextMessage, data); err != nil {
				wt.logger.Errorf("Error writing to WebSocket: %v", err)
				return
			}
			wt.UpdateActivity()
		}
	}
}

func (wt *WebSocketTunnel) Close() error {
	wt.cancel()
	if wt.wsConn != nil {
		wt.wsConn.Close()
	}
	return wt.SimpleTunnel.Close()
}
