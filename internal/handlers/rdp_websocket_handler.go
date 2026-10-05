package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

    "github.com/coder-lulu/newbee-proxy/internal/svc"
    "github.com/coder-lulu/newbee-proxy/plugins/common"
    "github.com/coder-lulu/newbee-proxy/plugins/rdp"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// RDPWebSocketHandler RDP WebSocket处理器
type RDPWebSocketHandler struct {
	svcCtx   *svc.ServiceContext
	upgrader websocket.Upgrader
	logger   logx.Logger

	// 连接管理
	connections map[string]*RDPWebSocketConnection
	mutex       sync.RWMutex
}

// RDPWebSocketConnection RDP WebSocket连接
type RDPWebSocketConnection struct {
	id           string
	ws           *websocket.Conn
	rdpPlugin    *rdp.RDPPlugin
	rdpConn      common.Connection
	ctx          context.Context
	cancel       context.CancelFunc
	logger       logx.Logger
	lastActivity time.Time
	mutex        sync.RWMutex

	// 消息队列
	sendCh chan []byte
	recvCh chan []byte

	// 状态
	connected bool
	closed    bool
}

// RDPConnectionRequest RDP连接请求
type RDPConnectionRequest struct {
	Target   string `json:"target"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// RDPMessage RDP WebSocket消息
type RDPMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// NewRDPWebSocketHandler 创建RDP WebSocket处理器
func NewRDPWebSocketHandler(svcCtx *svc.ServiceContext) *RDPWebSocketHandler {
	return &RDPWebSocketHandler{
		svcCtx: svcCtx,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				return true // 允许跨域连接
			},
		},
		logger:      svcCtx.Logger,
		connections: make(map[string]*RDPWebSocketConnection),
	}
}

// HandleRDPWebSocket 处理RDP WebSocket连接
func (h *RDPWebSocketHandler) HandleRDPWebSocket(w http.ResponseWriter, r *http.Request) {
	// 升级到WebSocket连接
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Errorf("WebSocket升级失败: %v", err)
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	// 生成连接ID
	connectionId := fmt.Sprintf("rdp_%d", time.Now().UnixNano())
	h.logger.Infof("新的RDP WebSocket连接: %s", connectionId)

	// 创建WebSocket连接对象
	ctx, cancel := context.WithCancel(r.Context())
	wsConn := &RDPWebSocketConnection{
		id:           connectionId,
		ws:           ws,
		ctx:          ctx,
		cancel:       cancel,
		logger:       h.logger,
		lastActivity: time.Now(),
		sendCh:       make(chan []byte, 256),
		recvCh:       make(chan []byte, 256),
		connected:    false,
		closed:       false,
	}

	// 注册连接
	h.mutex.Lock()
	h.connections[connectionId] = wsConn
	h.mutex.Unlock()

	// 发送连接成功消息
	response := map[string]interface{}{
		"type":          "connection_established",
		"connection_id": connectionId,
		"status":        "waiting_for_rdp_request",
	}
	wsConn.sendMessage(response)

	// 启动处理goroutines
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		wsConn.readLoop(h)
	}()

	go func() {
		defer wg.Done()
		wsConn.writeLoop()
	}()

	go func() {
		defer wg.Done()
		wsConn.processLoop(h)
	}()

	// 等待连接结束
	wg.Wait()

	// 清理连接
	h.cleanup(connectionId)
	h.logger.Infof("RDP WebSocket连接已关闭: %s", connectionId)
}

// readLoop 读取循环
func (conn *RDPWebSocketConnection) readLoop(h *RDPWebSocketHandler) {
	defer func() {
		conn.logger.Info("RDP WebSocket读取循环结束")
		conn.close()
	}()

	// 设置读取限制和超时
	conn.ws.SetReadLimit(1024 * 1024) // 1MB
	conn.ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.ws.SetPongHandler(func(string) error {
		conn.ws.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		select {
		case <-conn.ctx.Done():
			return
		default:
		}

		messageType, data, err := conn.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				conn.logger.Errorf("WebSocket读取错误: %v", err)
			}
			return
		}

		conn.updateActivity()

		switch messageType {
		case websocket.TextMessage:
			// 判断是JSON控制消息还是Guacamole指令
			dataStr := string(data)
			if strings.HasPrefix(dataStr, "{") && strings.HasSuffix(dataStr, "}") {
				// JSON控制消息
				var msg RDPMessage
				if err := json.Unmarshal(data, &msg); err != nil {
					conn.logger.Errorf("解析WebSocket JSON消息失败: %v", err)
					continue
				}
				conn.handleControlMessage(&msg, h)
			} else {
				// Guacamole指令 - 转发到RDP连接
				if conn.connected && conn.rdpConn != nil {
					select {
					case conn.recvCh <- data:
					case <-time.After(5 * time.Second):
						conn.logger.Error("RDP接收队列已满")
					}
				}
			}

		case websocket.BinaryMessage:
			// 处理RDP数据
			if conn.connected && conn.rdpConn != nil {
				select {
				case conn.recvCh <- data:
				case <-time.After(5 * time.Second):
					conn.logger.Error("RDP接收队列已满")
				}
			}

		case websocket.PingMessage:
			// 回复Pong
			if err := conn.ws.WriteMessage(websocket.PongMessage, nil); err != nil {
				return
			}
		}
	}
}

// writeLoop 写入循环
func (conn *RDPWebSocketConnection) writeLoop() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		conn.logger.Info("RDP WebSocket写入循环结束")
	}()

	for {
		select {
		case <-conn.ctx.Done():
			return

		case data := <-conn.sendCh:
			conn.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
				conn.logger.Errorf("WebSocket写入错误: %v", err)
				return
			}
			conn.updateActivity()

		case <-ticker.C:
			// 发送ping保持连接
			conn.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// processLoop 处理循环
func (conn *RDPWebSocketConnection) processLoop(h *RDPWebSocketHandler) {
	defer conn.logger.Info("RDP WebSocket处理循环结束")

	for {
		select {
		case <-conn.ctx.Done():
			return

		case data := <-conn.recvCh:
			// 将WebSocket数据转发到RDP连接
			if conn.connected && conn.rdpConn != nil {
				if _, err := conn.rdpConn.Write(data); err != nil {
					conn.logger.Errorf("写入RDP连接失败: %v", err)
					conn.sendErrorMessage("rdp_write_error", err.Error())
				}
			}
		}
	}
}

// handleControlMessage 处理控制消息
func (conn *RDPWebSocketConnection) handleControlMessage(msg *RDPMessage, h *RDPWebSocketHandler) {
	switch msg.Type {
	case "rdp_connect":
		conn.handleRDPConnect(msg.Data, h)

	case "rdp_disconnect":
		conn.handleRDPDisconnect()

	case "mouse_event":
		conn.handleMouseEvent(msg.Data)

	case "keyboard_event":
		conn.handleKeyboardEvent(msg.Data)

	case "resize":
		conn.handleResize(msg.Data)

	default:
		conn.logger.Errorf("未知的控制消息类型: %s", msg.Type)
	}
}

// handleRDPConnect 处理RDP连接请求
func (conn *RDPWebSocketConnection) handleRDPConnect(data interface{}, h *RDPWebSocketHandler) {
	var req RDPConnectionRequest
	dataBytes, _ := json.Marshal(data)
	if err := json.Unmarshal(dataBytes, &req); err != nil {
		conn.sendErrorMessage("invalid_request", "无效的连接请求")
		return
	}

	conn.logger.Infof("处理RDP连接请求: %s:%d", req.Target, req.Port)

	// 获取RDP插件实例
	rdpPluginInterface, exists := h.svcCtx.PluginManager.GetPluginForProtocol("rdp")
	if !exists {
		conn.sendErrorMessage("plugin_not_found", "RDP插件未找到")
		return
	}

	rdpPlugin, ok := rdpPluginInterface.(*rdp.RDPPlugin)
	if !ok {
		conn.sendErrorMessage("plugin_type_error", "RDP插件类型错误")
		return
	}

	// 设置连接参数
	if req.Port <= 0 {
		req.Port = 3389 // 默认RDP端口
	}
	if req.Width <= 0 {
		req.Width = 1920
	}
	if req.Height <= 0 {
		req.Height = 1080
	}

	// 建立RDP连接
	credentials := &common.Credentials{
		Username: req.Username,
		Password: req.Password,
	}

	target := fmt.Sprintf("%s:%d", req.Target, req.Port)
	rdpConn, err := rdpPlugin.CreateConnection(conn.ctx, target, credentials)
	if err != nil {
		conn.sendErrorMessage("rdp_connection_failed", fmt.Sprintf("RDP连接失败: %v", err))
		return
	}

	// 设置WebSocket桥接
	rdpConnWriter := &rdpConnWriter{wsConn: conn}
	rdpConnReader := &rdpConnReader{wsConn: conn}

	if err := rdpConn.SetWebSocketWriter(rdpConnWriter); err != nil {
		rdpConn.Close()
		conn.sendErrorMessage("bridge_setup_error", fmt.Sprintf("设置WebSocket写入器失败: %v", err))
		return
	}

	if err := rdpConn.SetWebSocketReader(rdpConnReader); err != nil {
		rdpConn.Close()
		conn.sendErrorMessage("bridge_setup_error", fmt.Sprintf("设置WebSocket读取器失败: %v", err))
		return
	}

	// 保存连接引用
	conn.mutex.Lock()
	conn.rdpPlugin = rdpPlugin
	conn.rdpConn = rdpConn
	conn.connected = true
	conn.mutex.Unlock()

	// 启动RDP到WebSocket的数据转发 - 关键修复！
	go conn.rdpToWebSocketForwarding()

	// 发送连接成功消息
	conn.sendMessage(map[string]interface{}{
		"type":   "rdp_connected",
		"width":  req.Width,
		"height": req.Height,
		"target": target,
	})

	conn.logger.Infof("RDP连接建立成功: %s", target)
}

// handleRDPDisconnect 处理RDP断开连接
func (conn *RDPWebSocketConnection) handleRDPDisconnect() {
	conn.mutex.Lock()
	defer conn.mutex.Unlock()

	if conn.rdpConn != nil {
		conn.rdpConn.Close()
		conn.rdpConn = nil
	}

	// 注意：不要停止RDP插件，因为它是共享的，其他连接可能还在使用
	// 只清除引用
	conn.rdpPlugin = nil

	conn.connected = false
	conn.sendMessage(map[string]interface{}{
		"type": "rdp_disconnected",
	})

	conn.logger.Info("RDP连接已断开")
}

// handleMouseEvent 处理鼠标事件
func (conn *RDPWebSocketConnection) handleMouseEvent(data interface{}) {
	if !conn.connected || conn.rdpConn == nil {
		return
	}

	// 构建鼠标事件数据并发送到RDP连接
	// 这里简化处理，实际应该构建完整的RDP鼠标事件PDU
	mouseData, _ := json.Marshal(data)
	conn.rdpConn.Write(mouseData)
}

// handleKeyboardEvent 处理键盘事件
func (conn *RDPWebSocketConnection) handleKeyboardEvent(data interface{}) {
	if !conn.connected || conn.rdpConn == nil {
		return
	}

	// 构建键盘事件数据并发送到RDP连接
	// 这里简化处理，实际应该构建完整的RDP键盘事件PDU
	keyboardData, _ := json.Marshal(data)
	conn.rdpConn.Write(keyboardData)
}

// handleResize 处理屏幕尺寸变化
func (conn *RDPWebSocketConnection) handleResize(data interface{}) {
	if !conn.connected || conn.rdpConn == nil {
		return
	}

	// 发送屏幕尺寸变化事件到RDP连接
	resizeData, _ := json.Marshal(data)
	conn.rdpConn.Write(resizeData)
}

// sendMessage 发送控制消息
func (conn *RDPWebSocketConnection) sendMessage(data interface{}) {
	message, err := json.Marshal(data)
	if err != nil {
		conn.logger.Errorf("序列化消息失败: %v", err)
		return
	}

	conn.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.ws.WriteMessage(websocket.TextMessage, message); err != nil {
		conn.logger.Errorf("发送WebSocket消息失败: %v", err)
	}
}

// sendErrorMessage 发送错误消息
func (conn *RDPWebSocketConnection) sendErrorMessage(errorType, message string) {
	errorMsg := map[string]interface{}{
		"type":    "error",
		"error":   errorType,
		"message": message,
	}
	conn.sendMessage(errorMsg)
}

// updateActivity 更新活动时间
func (conn *RDPWebSocketConnection) updateActivity() {
	conn.mutex.Lock()
	conn.lastActivity = time.Now()
	conn.mutex.Unlock()
}

// close 关闭连接
func (conn *RDPWebSocketConnection) close() {
	conn.mutex.Lock()
	defer conn.mutex.Unlock()

	if conn.closed {
		return
	}
	conn.closed = true

	// 关闭RDP连接
	if conn.rdpConn != nil {
		conn.rdpConn.Close()
		conn.rdpConn = nil
	}

	// 注意：不要停止RDP插件，因为它是共享的，其他连接可能还在使用
	// RDP插件由插件管理器管理，会在服务关闭时停止
	conn.rdpPlugin = nil

	// 关闭WebSocket
	conn.ws.Close()

	// 取消上下文
	conn.cancel()

	// 关闭通道
	close(conn.sendCh)
	close(conn.recvCh)
}

// cleanup 清理连接
func (h *RDPWebSocketHandler) cleanup(connectionId string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if conn, exists := h.connections[connectionId]; exists {
		conn.close()
		delete(h.connections, connectionId)
	}
}

// rdpConnWriter WebSocket写入器适配器
type rdpConnWriter struct {
	wsConn *RDPWebSocketConnection
}

func (w *rdpConnWriter) Write(data []byte) (int, error) {
	select {
	case w.wsConn.sendCh <- data:
		return len(data), nil
	case <-time.After(5 * time.Second):
		return 0, fmt.Errorf("WebSocket发送队列已满")
	}
}

// rdpConnReader WebSocket读取器适配器
type rdpConnReader struct {
	wsConn *RDPWebSocketConnection
}

func (r *rdpConnReader) Read(data []byte) (int, error) {
	select {
	case received := <-r.wsConn.recvCh:
		n := copy(data, received)
		return n, nil
	case <-r.wsConn.ctx.Done():
		return 0, r.wsConn.ctx.Err()
	}
}

// rdpToWebSocketForwarding RDP到WebSocket的数据转发 - 基于mayfly-go模式
func (conn *RDPWebSocketConnection) rdpToWebSocketForwarding() {
	defer func() {
		if r := recover(); r != nil {
			conn.logger.Errorf("RDP到WebSocket转发panic: %v", r)
		}
		conn.logger.Info("RDP到WebSocket数据转发结束")
	}()

	buffer := make([]byte, 4096)

	for {
		select {
		case <-conn.ctx.Done():
			return
		default:
		}

		conn.mutex.RLock()
		rdpConn := conn.rdpConn
		connected := conn.connected
		conn.mutex.RUnlock()

		if !connected || rdpConn == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// 从RDP连接读取数据
		n, err := rdpConn.Read(buffer)
		if err != nil {
			if err != io.EOF {
				conn.logger.Errorf("从RDP连接读取数据失败: %v", err)
			}
			return
		}

		if n > 0 {
			// 发送到WebSocket
			select {
			case conn.sendCh <- buffer[:n]:
				// 数据已发送到WebSocket
				conn.updateActivity()
			case <-time.After(5 * time.Second):
				conn.logger.Errorf("WebSocket发送队列已满，丢弃数据")
			case <-conn.ctx.Done():
				return
			}
		}
	}
}
