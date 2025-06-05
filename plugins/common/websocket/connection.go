package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketConnection WebSocket连接封装
type WebSocketConnection struct {
	// 基础信息
	ID       string
	wsConn   *websocket.Conn
	request  *http.Request
	manager  *WebSocketManager
	metadata map[string]string

	// 消息通道
	messageChan chan *Message
	sendChan    chan *Message
	stopChan    chan struct{}

	// 状态管理
	closed       bool
	createdAt    time.Time
	lastActiveAt time.Time
	mutex        sync.RWMutex

	// 统计信息
	messagesSent     int64
	messagesReceived int64
	bytesSent        int64
	bytesReceived    int64

	// 控制
	ctx    context.Context
	cancel context.CancelFunc

	// 日志
	logger Logger
}

// Message WebSocket消息
type Message struct {
	Type      int                    `json:"type"`
	Data      interface{}            `json:"data"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	RequestID string                 `json:"request_id,omitempty"`
}

// SendMessage 发送消息
func (conn *WebSocketConnection) SendMessage(message *Message) error {
	if conn.isClosed() {
		return ErrConnectionClosed
	}

	select {
	case conn.sendChan <- message:
		return nil
	case <-time.After(5 * time.Second):
		return ErrSendTimeout
	}
}

// SendJSON 发送JSON消息
func (conn *WebSocketConnection) SendJSON(data interface{}) error {
	message := &Message{
		Type:      websocket.TextMessage,
		Data:      data,
		Timestamp: time.Now(),
	}
	return conn.SendMessage(message)
}

// SendText 发送文本消息
func (conn *WebSocketConnection) SendText(text string) error {
	message := &Message{
		Type:      websocket.TextMessage,
		Data:      text,
		Timestamp: time.Now(),
	}
	return conn.SendMessage(message)
}

// SendBinary 发送二进制消息
func (conn *WebSocketConnection) SendBinary(data []byte) error {
	message := &Message{
		Type:      websocket.BinaryMessage,
		Data:      data,
		Timestamp: time.Now(),
	}
	return conn.SendMessage(message)
}

// GetID 获取连接ID
func (conn *WebSocketConnection) GetID() string {
	return conn.ID
}

// GetMetadata 获取元数据
func (conn *WebSocketConnection) GetMetadata() map[string]string {
	conn.mutex.RLock()
	defer conn.mutex.RUnlock()

	metadata := make(map[string]string)
	for k, v := range conn.metadata {
		metadata[k] = v
	}
	return metadata
}

// SetMetadata 设置元数据
func (conn *WebSocketConnection) SetMetadata(key, value string) {
	conn.mutex.Lock()
	defer conn.mutex.Unlock()

	if conn.metadata == nil {
		conn.metadata = make(map[string]string)
	}
	conn.metadata[key] = value
}

// GetStats 获取连接统计
func (conn *WebSocketConnection) GetStats() ConnectionStats {
	conn.mutex.RLock()
	defer conn.mutex.RUnlock()

	return ConnectionStats{
		ID:               conn.ID,
		CreatedAt:        conn.createdAt,
		LastActiveAt:     conn.lastActiveAt,
		Duration:         time.Since(conn.createdAt),
		IdleDuration:     time.Since(conn.lastActiveAt),
		MessagesSent:     conn.messagesSent,
		MessagesReceived: conn.messagesReceived,
		BytesSent:        conn.bytesSent,
		BytesReceived:    conn.bytesReceived,
		RemoteAddr:       conn.getRemoteAddr(),
		UserAgent:        conn.getUserAgent(),
	}
}

// ConnectionStats 连接统计信息
type ConnectionStats struct {
	ID               string        `json:"id"`
	CreatedAt        time.Time     `json:"created_at"`
	LastActiveAt     time.Time     `json:"last_active_at"`
	Duration         time.Duration `json:"duration"`
	IdleDuration     time.Duration `json:"idle_duration"`
	MessagesSent     int64         `json:"messages_sent"`
	MessagesReceived int64         `json:"messages_received"`
	BytesSent        int64         `json:"bytes_sent"`
	BytesReceived    int64         `json:"bytes_received"`
	RemoteAddr       string        `json:"remote_addr"`
	UserAgent        string        `json:"user_agent"`
}

// startReading 启动读取协程
func (conn *WebSocketConnection) startReading() {
	defer func() {
		conn.logger.Infof("WebSocket reading stopped for connection: %s", conn.ID)
		conn.close("read loop ended")
	}()

	for {
		select {
		case <-conn.stopChan:
			return
		default:
		}

		messageType, data, err := conn.wsConn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				conn.logger.Errorf("WebSocket read error for %s: %v", conn.ID, err)
			}
			return
		}

		conn.updateLastActiveTime()
		conn.updateStats(0, int64(len(data)), 0, 1)

		// 转发给管理器处理
		conn.manager.handleMessage(conn, messageType, data)
	}
}

// startWriting 启动写入协程
func (conn *WebSocketConnection) startWriting() {
	defer func() {
		conn.logger.Infof("WebSocket writing stopped for connection: %s", conn.ID)
	}()

	for {
		select {
		case <-conn.stopChan:
			return
		case message := <-conn.sendChan:
			if err := conn.writeMessage(message); err != nil {
				conn.logger.Errorf("WebSocket write error for %s: %v", conn.ID, err)
				conn.close("write error")
				return
			}
		}
	}
}

// startHeartbeat 启动心跳协程
func (conn *WebSocketConnection) startHeartbeat() {
	ticker := time.NewTicker(conn.manager.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-conn.stopChan:
			return
		case <-ticker.C:
			if err := conn.ping(); err != nil {
				conn.logger.Errorf("WebSocket ping error for %s: %v", conn.ID, err)
				conn.close("ping failed")
				return
			}
		}
	}
}

// writeMessage 写入消息
func (conn *WebSocketConnection) writeMessage(message *Message) error {
	timeouts := conn.manager.timeoutCtrl.GetWebSocketTimeouts()
	conn.wsConn.SetWriteDeadline(time.Now().Add(timeouts.MessageTimeout))

	var data []byte
	var err error

	switch message.Type {
	case websocket.TextMessage:
		if str, ok := message.Data.(string); ok {
			data = []byte(str)
		} else {
			data, err = json.Marshal(message.Data)
			if err != nil {
				return err
			}
		}
	case websocket.BinaryMessage:
		if bytes, ok := message.Data.([]byte); ok {
			data = bytes
		} else {
			data, err = json.Marshal(message.Data)
			if err != nil {
				return err
			}
		}
	default:
		data, err = json.Marshal(message.Data)
		if err != nil {
			return err
		}
		message.Type = websocket.TextMessage
	}

	err = conn.wsConn.WriteMessage(message.Type, data)
	if err == nil {
		conn.updateStats(int64(len(data)), 0, 1, 0)
	}

	return err
}

// ping 发送ping
func (conn *WebSocketConnection) ping() error {
	timeouts := conn.manager.timeoutCtrl.GetWebSocketTimeouts()
	conn.wsConn.SetWriteDeadline(time.Now().Add(timeouts.PingTimeout))
	return conn.wsConn.WriteMessage(websocket.PingMessage, nil)
}

// updateLastActiveTime 更新最后活动时间
func (conn *WebSocketConnection) updateLastActiveTime() {
	conn.mutex.Lock()
	defer conn.mutex.Unlock()
	conn.lastActiveAt = time.Now()
}

// updateStats 更新统计信息
func (conn *WebSocketConnection) updateStats(bytesSent, bytesReceived, messagesSent, messagesReceived int64) {
	conn.mutex.Lock()
	defer conn.mutex.Unlock()

	conn.bytesSent += bytesSent
	conn.bytesReceived += bytesReceived
	conn.messagesSent += messagesSent
	conn.messagesReceived += messagesReceived
}

// isClosed 检查连接是否已关闭
func (conn *WebSocketConnection) isClosed() bool {
	conn.mutex.RLock()
	defer conn.mutex.RUnlock()
	return conn.closed
}

// close 关闭连接
func (conn *WebSocketConnection) close(reason string) {
	conn.mutex.Lock()
	if conn.closed {
		conn.mutex.Unlock()
		return
	}
	conn.closed = true
	conn.mutex.Unlock()

	// 关闭WebSocket连接
	conn.wsConn.Close()

	// 关闭通道
	close(conn.stopChan)

	// 调用断开连接处理器
	conn.manager.handleDisconnect(conn, reason)

	conn.logger.Infof("WebSocket connection closed: %s (reason: %s)", conn.ID, reason)
}

// getRemoteAddr 获取远程地址
func (conn *WebSocketConnection) getRemoteAddr() string {
	if conn.request != nil {
		return conn.request.RemoteAddr
	}
	return ""
}

// getUserAgent 获取用户代理
func (conn *WebSocketConnection) getUserAgent() string {
	if conn.request != nil {
		return conn.request.UserAgent()
	}
	return ""
}

// 错误定义
var (
	ErrConnectionClosed = fmt.Errorf("websocket connection is closed")
	ErrSendTimeout      = fmt.Errorf("send message timeout")
)
