package handlers

import (
    "encoding/json"
    "fmt"
    "net/http"
    "strconv"
    "sync"
    "time"

    mctx "github.com/coder-lulu/newbee-proxy/internal/middleware"
    "github.com/coder-lulu/newbee-proxy/internal/metrics"
    "github.com/coder-lulu/newbee-proxy/internal/svc"
    "github.com/coder-lulu/newbee-proxy/plugins/common"

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
    once   sync.Once
    // 背压发送队列
    sendCh chan []byte
}

func NewSafeWebSocketWriter(conn *websocket.Conn, logger logx.Logger) *SafeWebSocketWriter {
    w := &SafeWebSocketWriter{
        conn:   conn,
        logger: logger,
        sendCh: make(chan []byte, 256),
    }
    w.once.Do(func() { go w.writeLoop() })
    return w
}

func (w *SafeWebSocketWriter) Write(p []byte) (n int, err error) {
    w.mutex.Lock()
    closed := w.closed
    w.mutex.Unlock()
    if closed {
        return 0, fmt.Errorf("websocket connection closed")
    }
    // 入队，5s 超时
    select {
    case w.sendCh <- append([]byte(nil), p...):
        metrics.AddWSSentBytes("ssh/telnet", len(p))
        return len(p), nil
    case <-time.After(5 * time.Second):
        metrics.IncWSSendQueueDrop("ssh/telnet")
        return 0, fmt.Errorf("websocket send queue full")
    }
}

func (w *SafeWebSocketWriter) WriteJSON(v interface{}) error {
    data, err := json.Marshal(v)
    if err != nil {
        return err
    }
    _, err = w.Write(data)
    return err
}

func (w *SafeWebSocketWriter) Close() {
    w.mutex.Lock()
    defer w.mutex.Unlock()
    w.closed = true
    select {
    case <-time.After(0):
    default:
    }
    // 尽量关闭发送队列
    close(w.sendCh)
}

// writeLoop 后台写循环 + 心跳
func (w *SafeWebSocketWriter) writeLoop() {
    // 心跳与 RTT 统计
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case data, ok := <-w.sendCh:
            if !ok {
                return
            }
            // 写入 TextMessage 保持兼容终端
            w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
            if err := w.conn.WriteMessage(websocket.TextMessage, data); err != nil {
                w.logger.Errorf("WebSocket write error: %v", err)
                metrics.IncWSError("ssh/telnet", "write")
                return
            }
        case <-ticker.C:
            // 发送 ping 并观测 pong RTT
            start := time.Now()
            w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
            if err := w.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                metrics.IncWSError("ssh/telnet", "ping")
                return
            }
            _ = w.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
            w.conn.SetPongHandler(func(string) error {
                metrics.ObserveWSPingRTT("ssh/telnet", time.Since(start).Seconds())
                // 恢复读超时由桥接方控制
                _ = w.conn.SetReadDeadline(time.Time{})
                return nil
            })
        }
    }
}

// WebSocketTunnelHandler WebSocket隧道处理器
func WebSocketTunnelHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        logger := logx.WithContext(r.Context())

        // 维护窗口：拒绝新会话
        if !svcCtx.IsAcceptingNew() {
            w.Header().Set("Retry-After", "30")
            http.Error(w, "Service Unavailable: draining", http.StatusServiceUnavailable)
            return
        }

        // 握手前认证（协议为 ssh）
        if ok, reason := mctx.VerifyWSRequest(r, "ssh", svcCtx.Config.Security, svcCtx.Config.OpsCenter.PSK); !ok {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        } else if reason != "ok" {
            logger.Infof("WS auth in observe mode: %s", reason)
        }

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

        // 校验首帧 SessionID 与 JWT claims 一致（若提供）
        if claims, ok := mctx.GetJWTClaims(r.Context()); ok {
            if sid, ok2 := claims["sessionId"].(string); ok2 && sid != "" && tunnelReq.SessionID != "" && sid != tunnelReq.SessionID {
                // 观察模式：仅记录；严格模式由握手中间件处理
                logger.Infof("sessionId mismatch: claim=%s req=%s", sid, tunnelReq.SessionID)
            }
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
            if svcCtx.AuditManager != nil {
                if claims, ok := mctx.GetJWTClaims(r.Context()); ok {
                    svcCtx.AuditManager.LogWSError(r, "ssh", target, tunnelReq.SessionID, "", claims, err)
                } else {
                    svcCtx.AuditManager.LogWSError(r, "ssh", target, tunnelReq.SessionID, "", nil, err)
                }
            }
            return
        }
        defer plugin.CloseConnection(sshConn.ID())

        // 审计：连接建立
        if svcCtx.AuditManager != nil {
            if claims, ok := mctx.GetJWTClaims(r.Context()); ok {
                svcCtx.AuditManager.LogWSConnect(r, "ssh", target, tunnelReq.SessionID, sshConn.ID(), claims)
            } else {
                svcCtx.AuditManager.LogWSConnect(r, "ssh", target, tunnelReq.SessionID, sshConn.ID(), nil)
            }
        }

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
        metrics.IncWSConnections("ssh")
        defer metrics.DecWSConnections("ssh")

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
        if svcCtx.AuditManager != nil {
            if claims, ok := mctx.GetJWTClaims(r.Context()); ok {
                svcCtx.AuditManager.LogWSDisconnect(r, "ssh", target, tunnelReq.SessionID, sshConn.ID(), claims)
            } else {
                svcCtx.AuditManager.LogWSDisconnect(r, "ssh", target, tunnelReq.SessionID, sshConn.ID(), nil)
            }
        }
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
        metrics.IncWSError("ssh", "read")
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

        // 维护窗口：拒绝新会话
        if !svcCtx.IsAcceptingNew() {
            w.Header().Set("Retry-After", "30")
            http.Error(w, "Service Unavailable: draining", http.StatusServiceUnavailable)
            return
        }

        // 握手前认证（协议为 telnet）
        if ok, reason := mctx.VerifyWSRequest(r, "telnet", svcCtx.Config.Security, svcCtx.Config.OpsCenter.PSK); !ok {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        } else if reason != "ok" {
            logger.Infof("WS auth in observe mode: %s", reason)
        }

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

        // 校验首帧 SessionID 与 JWT claims 一致（若提供）
        if claims, ok := mctx.GetJWTClaims(r.Context()); ok {
            if sid, ok2 := claims["sessionId"].(string); ok2 && sid != "" && tunnelReq.SessionID != "" && sid != tunnelReq.SessionID {
                logger.Infof("sessionId mismatch: claim=%s req=%s", sid, tunnelReq.SessionID)
            }
        }

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
            if svcCtx.AuditManager != nil {
                if claims, ok := mctx.GetJWTClaims(r.Context()); ok {
                    svcCtx.AuditManager.LogWSError(r, "telnet", fmt.Sprintf("%s:%d", tunnelReq.Target, tunnelReq.Port), tunnelReq.SessionID, "", claims, err)
                } else {
                    svcCtx.AuditManager.LogWSError(r, "telnet", fmt.Sprintf("%s:%d", tunnelReq.Target, tunnelReq.Port), tunnelReq.SessionID, "", nil, err)
                }
            }
            return
        }
        defer svcCtx.TelnetSessionManager.CloseSession(telnetSession.ID)

        if svcCtx.AuditManager != nil {
            if claims, ok := mctx.GetJWTClaims(r.Context()); ok {
                svcCtx.AuditManager.LogWSConnect(r, "telnet", fmt.Sprintf("%s:%d", tunnelReq.Target, tunnelReq.Port), tunnelReq.SessionID, telnetSession.ID, claims)
            } else {
                svcCtx.AuditManager.LogWSConnect(r, "telnet", fmt.Sprintf("%s:%d", tunnelReq.Target, tunnelReq.Port), tunnelReq.SessionID, telnetSession.ID, nil)
            }
        }

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
        metrics.IncWSConnections("telnet")
        defer metrics.DecWSConnections("telnet")

		// 启动双向数据转发
		go telnetSession.StartForwarding(safeWriter)

        // 处理来自WebSocket的消息并转发到Telnet（带背压的读队列）
        // 采用有界队列，避免 Telnet 写入阻塞导致的内存膨胀
        const readQueueSize = 256
        readQ := make(chan []byte, readQueueSize)

        // WebSocket 读协程：将消息入队，队列满时丢弃并记录
        wsReadDone := make(chan struct{})
        go func() {
            defer close(wsReadDone)
            for {
                _, message, err := conn.ReadMessage()
                if err != nil {
                    logger.Infof("WebSocket read error (connection closed): %v", err)
                    return
                }
                select {
                case readQ <- append([]byte(nil), message...):
                    metrics.AddWSRecvBytes("telnet", len(message))
                case <-time.After(5 * time.Second):
                    // 入队超时，视为背压丢弃
                    metrics.IncWSSendQueueDrop("telnet_read")
                }
            }
        }()

        // Telnet 写协程：从队列取数据并发送
        telnetWriteDone := make(chan struct{})
        go func() {
            defer close(telnetWriteDone)
            for msg := range readQ {
                if err := telnetSession.SendToTelnet(msg); err != nil {
                    logger.Errorf("Failed to send to telnet: %v", err)
                    return
                }
            }
        }()

        // 等待上下文结束或读写终止
        select {
        case <-r.Context().Done():
        case <-wsReadDone:
        case <-telnetWriteDone:
        }

        // 清理
        close(readQ)
        
        logger.Info("WebSocket Telnet tunnel session ended")
        if svcCtx.AuditManager != nil {
            if claims, ok := mctx.GetJWTClaims(r.Context()); ok {
                svcCtx.AuditManager.LogWSDisconnect(r, "telnet", fmt.Sprintf("%s:%d", tunnelReq.Target, tunnelReq.Port), tunnelReq.SessionID, telnetSession.ID, claims)
            } else {
                svcCtx.AuditManager.LogWSDisconnect(r, "telnet", fmt.Sprintf("%s:%d", tunnelReq.Target, tunnelReq.Port), tunnelReq.SessionID, telnetSession.ID, nil)
            }
        }
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
