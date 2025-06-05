package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"newbee-agent/internal/svc"
	"newbee-agent/plugins/common"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// 允许所有来源（在生产环境中应该更严格）
		return true
	},
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// SSHTunnelRequest SSH隧道请求参数
type SSHTunnelRequest struct {
	Target     string `json:"target"`      // SSH服务器地址
	Port       int    `json:"port"`        // SSH端口
	Username   string `json:"username"`    // 用户名
	Password   string `json:"password"`    // 密码（可选）
	PrivateKey string `json:"private_key"` // 私钥（可选）
	AuthType   string `json:"auth_type"`   // 认证类型
	Cols       int    `json:"cols"`        // 终端列数
	Rows       int    `json:"rows"`        // 终端行数
	SessionID  string `json:"session_id"`  // 会话ID
}

// SSHTunnelResponse SSH隧道响应
type SSHTunnelResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	ConnectionID string `json:"connection_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
}

// TelnetTunnelRequest Telnet隧道请求参数
type TelnetTunnelRequest struct {
	Target    string `json:"target"`     // Telnet服务器地址
	Port      int    `json:"port"`       // Telnet端口
	Username  string `json:"username"`   // 用户名（可选，有些设备只需密码）
	Password  string `json:"password"`   // 密码
	Cols      int    `json:"cols"`       // 终端列数
	Rows      int    `json:"rows"`       // 终端行数
	SessionID string `json:"session_id"` // 会话ID
}

// TelnetTunnelResponse Telnet隧道响应
type TelnetTunnelResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	ConnectionID string `json:"connection_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
}

// SafeWebSocketWriter 线程安全的WebSocket写入器
type SafeWebSocketWriter struct {
	conn   *websocket.Conn
	logger logx.Logger
	mutex  sync.Mutex
	closed bool
}

func NewSafeWebSocketWriter(conn *websocket.Conn, logger logx.Logger) *SafeWebSocketWriter {
	return &SafeWebSocketWriter{
		conn:   conn,
		logger: logger,
	}
}

func (w *SafeWebSocketWriter) Write(p []byte) (n int, err error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.closed {
		return 0, fmt.Errorf("websocket connection closed")
	}

	err = w.conn.WriteMessage(websocket.TextMessage, p)
	if err != nil {
		w.logger.Errorf("WebSocket write error: %v", err)
		w.closed = true
		return 0, err
	}
	return len(p), nil
}

func (w *SafeWebSocketWriter) WriteJSON(v interface{}) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.closed {
		return fmt.Errorf("websocket connection closed")
	}

	err := w.conn.WriteJSON(v)
	if err != nil {
		w.logger.Errorf("WebSocket WriteJSON error: %v", err)
		w.closed = true
	}
	return err
}

func (w *SafeWebSocketWriter) Close() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.closed = true
}

// WebSocketTunnelHandler WebSocket隧道处理器
func WebSocketTunnelHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logx.WithContext(r.Context())

		// 升级到WebSocket连接
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.Errorf("WebSocket upgrade failed: %v", err)
			http.Error(w, "WebSocket upgrade failed", http.StatusBadRequest)
			return
		}
		defer conn.Close()

		// 创建线程安全的写入器
		safeWriter := NewSafeWebSocketWriter(conn, logger)
		defer safeWriter.Close()

		// 等待客户端发送连接请求
		var tunnelReq SSHTunnelRequest
		if err := conn.ReadJSON(&tunnelReq); err != nil {
			logger.Errorf("Failed to read tunnel request: %v", err)
			sendErrorResponse(safeWriter, "Invalid tunnel request")
			return
		}

		// 获取SSH插件
		plugin, exists := svcCtx.PluginManager.GetPlugin("ssh")
		if !exists {
			logger.Error("SSH plugin not found")
			sendErrorResponse(safeWriter, "SSH plugin not available")
			return
		}

		// 创建SSH连接
		credentials := &common.Credentials{
			Username:   tunnelReq.Username,
			Password:   tunnelReq.Password,
			PrivateKey: tunnelReq.PrivateKey,
			AuthType:   tunnelReq.AuthType,
			Timeout:    30, // 30秒超时
		}

		target := fmt.Sprintf("%s:%d", tunnelReq.Target, tunnelReq.Port)
		sshConn, err := plugin.CreateConnection(r.Context(), target, credentials)
		if err != nil {
			logger.Errorf("Failed to create SSH connection: %v", err)
			sendErrorResponse(safeWriter, fmt.Sprintf("SSH connection failed: %v", err))
			return
		}
		defer plugin.CloseConnection(sshConn.ID())

		// 发送成功响应
		response := SSHTunnelResponse{
			Success:      true,
			Message:      "SSH tunnel established",
			ConnectionID: sshConn.ID(),
			SessionID:    tunnelReq.SessionID,
		}
		if err := safeWriter.WriteJSON(response); err != nil {
			logger.Errorf("Failed to send success response: %v", err)
			return
		}

		logger.Infof("SSH WebSocket tunnel established: %s -> %s", conn.RemoteAddr(), target)

		// 设置SSH连接的WebSocket读写器，使用SSH内部的桥接机制
		wsReader := &WebSocketReader{conn: conn, logger: logger}
		if err := sshConn.SetWebSocketReader(wsReader); err != nil {
			logger.Errorf("Failed to set WebSocket reader: %v", err)
			return
		}

		if err := sshConn.SetWebSocketWriter(safeWriter); err != nil {
			logger.Errorf("Failed to set WebSocket writer: %v", err)
			return
		}

		logger.Info("WebSocket bridge configured successfully, waiting for connection to close...")

		// 等待请求上下文完成（WebSocket连接关闭时会触发）
		// 不进行任何WebSocket写入操作，避免与SSH桥接的并发写入冲突
		<-r.Context().Done()

		logger.Info("WebSocket SSH tunnel session ended")
	}
}

// WebSocketReader WebSocket读取器
type WebSocketReader struct {
	conn   *websocket.Conn
	logger logx.Logger
}

func (r *WebSocketReader) Read(p []byte) (n int, err error) {
	_, data, err := r.conn.ReadMessage()
	if err != nil {
		return 0, err
	}

	copy(p, data)
	return len(data), nil
}

// sendErrorResponse 发送错误响应
func sendErrorResponse(writer *SafeWebSocketWriter, message string) {
	response := SSHTunnelResponse{
		Success: false,
		Message: message,
	}
	writer.WriteJSON(response)
}

// ResizeTerminalHandler 调整终端大小处理器
func ResizeTerminalHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logx.WithContext(r.Context())

		// 获取参数
		connectionID := r.URL.Query().Get("connection_id")
		colsStr := r.URL.Query().Get("cols")
		rowsStr := r.URL.Query().Get("rows")

		if connectionID == "" || colsStr == "" || rowsStr == "" {
			http.Error(w, "Missing required parameters", http.StatusBadRequest)
			return
		}

		cols, err := strconv.Atoi(colsStr)
		if err != nil {
			http.Error(w, "Invalid cols parameter", http.StatusBadRequest)
			return
		}

		rows, err := strconv.Atoi(rowsStr)
		if err != nil {
			http.Error(w, "Invalid rows parameter", http.StatusBadRequest)
			return
		}

		// 获取SSH插件
		plugin, exists := svcCtx.PluginManager.GetPlugin("ssh")
		if !exists {
			http.Error(w, "SSH plugin not available", http.StatusServiceUnavailable)
			return
		}

		// 获取连接并验证其存在
		_, exists = plugin.GetConnection(connectionID)
		if !exists {
			http.Error(w, "Connection not found", http.StatusNotFound)
			return
		}

		// 这里应该实现终端大小调整功能
		// 由于SSH连接接口中没有定义这个方法，我们先记录日志
		logger.Infof("Terminal resize request: connection=%s, cols=%d, rows=%d", connectionID, cols, rows)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Terminal resize requested",
		})
	}
}

// WebSocketTelnetTunnelHandler WebSocket Telnet隧道处理器
func WebSocketTelnetTunnelHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logx.WithContext(r.Context())

		// 升级到WebSocket连接
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.Errorf("WebSocket upgrade failed: %v", err)
			http.Error(w, "WebSocket upgrade failed", http.StatusBadRequest)
			return
		}
		defer conn.Close()

		// 创建线程安全的写入器
		safeWriter := NewSafeWebSocketWriter(conn, logger)
		defer safeWriter.Close()

		logger.Info("WebSocket Telnet tunnel connection established")

		// 等待客户端发送连接请求
		var tunnelReq TelnetTunnelRequest
		if err := conn.ReadJSON(&tunnelReq); err != nil {
			logger.Errorf("Failed to read telnet tunnel request: %v", err)
			sendTelnetErrorResponse(safeWriter, "Invalid telnet tunnel request")
			return
		}

		logger.Infof("Received telnet tunnel request: %s:%d", tunnelReq.Target, tunnelReq.Port)

		// 转换为svc包中的TelnetTunnelRequest
		svcReq := &svc.TelnetTunnelRequest{
			Target:    tunnelReq.Target,
			Port:      tunnelReq.Port,
			Username:  tunnelReq.Username,
			Password:  tunnelReq.Password,
			Cols:      tunnelReq.Cols,
			Rows:      tunnelReq.Rows,
			SessionID: tunnelReq.SessionID,
		}

		// 创建Telnet连接
		telnetSession, err := svcCtx.TelnetSessionManager.CreateSession(r.Context(), svcReq)
		if err != nil {
			logger.Errorf("Failed to create telnet session: %v", err)
			sendTelnetErrorResponse(safeWriter, fmt.Sprintf("Telnet connection failed: %v", err))
			return
		}
		defer svcCtx.TelnetSessionManager.CloseSession(telnetSession.ID)

		// 发送成功响应
		response := TelnetTunnelResponse{
			Success:      true,
			Message:      "Telnet tunnel established",
			ConnectionID: telnetSession.ID,
			SessionID:    tunnelReq.SessionID,
		}
		if err := safeWriter.WriteJSON(response); err != nil {
			logger.Errorf("Failed to send success response: %v", err)
			return
		}

		logger.Infof("Telnet WebSocket tunnel established: %s -> %s:%d", conn.RemoteAddr(), tunnelReq.Target, tunnelReq.Port)

		// 启动双向数据转发
		go telnetSession.StartForwarding(safeWriter)

		// 处理来自WebSocket的消息并转发到Telnet
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				logger.Infof("WebSocket read error (connection closed): %v", err)
				break
			}

			// 转发消息到Telnet连接
			if err := telnetSession.SendToTelnet(message); err != nil {
				logger.Errorf("Failed to send to telnet: %v", err)
				break
			}
		}

		logger.Info("WebSocket Telnet tunnel session ended")
	}
}

// sendTelnetErrorResponse 发送Telnet错误响应
func sendTelnetErrorResponse(writer *SafeWebSocketWriter, message string) {
	response := TelnetTunnelResponse{
		Success: false,
		Message: message,
	}
	writer.WriteJSON(response)
}
