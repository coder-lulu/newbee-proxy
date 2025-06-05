package db

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketSessionImpl WebSocket会话实现
type WebSocketSessionImpl struct {
	sessionID    string
	connectionID string
	conn         *websocket.Conn
	dbConn       DbConnection

	// 状态管理
	active bool
	mutex  sync.RWMutex

	// 事件处理器
	messageHandler func(*WSMessage)
	errorHandler   func(error)
	closeHandler   func()

	// 指标
	metrics *SessionMetrics

	// 控制通道
	stopChan    chan struct{}
	messageChan chan *WSMessage
}

// NewWebSocketSession 创建新的WebSocket会话
func NewWebSocketSession(sessionID, connectionID string, conn *websocket.Conn, dbConn DbConnection) *WebSocketSessionImpl {
	return &WebSocketSessionImpl{
		sessionID:    sessionID,
		connectionID: connectionID,
		conn:         conn,
		dbConn:       dbConn,
		active:       false,
		stopChan:     make(chan struct{}),
		messageChan:  make(chan *WSMessage, 100),
		metrics: &SessionMetrics{
			SessionID:    sessionID,
			ConnectionID: connectionID,
			StartTime:    time.Now(),
			LastActivity: time.Now(),
		},
	}
}

// Start 启动会话
func (ws *WebSocketSessionImpl) Start(ctx context.Context) error {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	if ws.active {
		return fmt.Errorf("session already active")
	}

	ws.active = true
	ws.metrics.StartTime = time.Now()

	// 启动消息处理协程
	go ws.handleMessages(ctx)

	// 启动WebSocket读取协程
	go ws.readMessages(ctx)

	// 启动WebSocket写入协程
	go ws.writeMessages(ctx)

	// 发送会话启动消息
	startMsg := &WSMessage{
		Type:      WSMsgTypeStatus,
		Data:      map[string]interface{}{"status": "started", "session_id": ws.sessionID},
		Timestamp: time.Now(),
		SessionID: ws.sessionID,
	}

	select {
	case ws.messageChan <- startMsg:
	default:
		// 消息队列满，忽略
	}

	return nil
}

// Stop 停止会话
func (ws *WebSocketSessionImpl) Stop() error {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	if !ws.active {
		return fmt.Errorf("session not active")
	}

	ws.active = false

	// 发送停止信号
	close(ws.stopChan)

	// 关闭WebSocket连接
	if ws.conn != nil {
		ws.conn.Close()
	}

	// 更新指标
	ws.metrics.Duration = time.Since(ws.metrics.StartTime)

	// 调用关闭处理器
	if ws.closeHandler != nil {
		ws.closeHandler()
	}

	return nil
}

// IsActive 检查会话是否活跃
func (ws *WebSocketSessionImpl) IsActive() bool {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.active
}

// ExecuteCommand 执行命令
func (ws *WebSocketSessionImpl) ExecuteCommand(command string) error {
	if !ws.IsActive() {
		return fmt.Errorf("session not active")
	}

	// 解析命令
	var cmd CommandMessage
	if err := json.Unmarshal([]byte(command), &cmd); err != nil {
		return fmt.Errorf("failed to parse command: %w", err)
	}

	// 创建命令消息
	cmdMsg := &WSMessage{
		Type:      WSMsgTypeCommand,
		Data:      cmd,
		Timestamp: time.Now(),
		SessionID: ws.sessionID,
		RequestID: cmd.RequestID,
	}

	// 发送到处理队列
	select {
	case ws.messageChan <- cmdMsg:
		return nil
	default:
		return fmt.Errorf("message queue full")
	}
}

// SendMessage 发送消息
func (ws *WebSocketSessionImpl) SendMessage(message *WSMessage) error {
	if !ws.IsActive() {
		return fmt.Errorf("session not active")
	}

	message.SessionID = ws.sessionID
	message.Timestamp = time.Now()

	select {
	case ws.messageChan <- message:
		return nil
	default:
		return fmt.Errorf("message queue full")
	}
}

// OnMessage 设置消息处理器
func (ws *WebSocketSessionImpl) OnMessage(handler func(*WSMessage)) {
	ws.messageHandler = handler
}

// OnError 设置错误处理器
func (ws *WebSocketSessionImpl) OnError(handler func(error)) {
	ws.errorHandler = handler
}

// OnClose 设置关闭处理器
func (ws *WebSocketSessionImpl) OnClose(handler func()) {
	ws.closeHandler = handler
}

// GetSessionID 获取会话ID
func (ws *WebSocketSessionImpl) GetSessionID() string {
	return ws.sessionID
}

// GetConnectionID 获取连接ID
func (ws *WebSocketSessionImpl) GetConnectionID() string {
	return ws.connectionID
}

// GetMetrics 获取会话指标
func (ws *WebSocketSessionImpl) GetMetrics() *SessionMetrics {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()

	metrics := *ws.metrics
	metrics.Duration = time.Since(metrics.StartTime)
	return &metrics
}

// handleMessages 处理消息
func (ws *WebSocketSessionImpl) handleMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ws.stopChan:
			return
		case msg := <-ws.messageChan:
			ws.processMessage(ctx, msg)
		}
	}
}

// processMessage 处理单个消息
func (ws *WebSocketSessionImpl) processMessage(ctx context.Context, msg *WSMessage) {
	ws.mutex.Lock()
	ws.metrics.LastActivity = time.Now()
	ws.mutex.Unlock()

	switch msg.Type {
	case WSMsgTypeCommand:
		ws.handleCommand(ctx, msg)
	case WSMsgTypeHeartbeat:
		ws.handleHeartbeat(msg)
	default:
		// 其他消息类型直接转发
		if ws.messageHandler != nil {
			ws.messageHandler(msg)
		}
	}
}

// handleCommand 处理命令
func (ws *WebSocketSessionImpl) handleCommand(ctx context.Context, msg *WSMessage) {
	cmd, ok := msg.Data.(CommandMessage)
	if !ok {
		ws.sendError(msg.RequestID, "invalid command format")
		return
	}

	ws.mutex.Lock()
	ws.metrics.CommandsExecuted++
	ws.mutex.Unlock()

	switch cmd.Type {
	case "execute_sql":
		ws.handleExecuteSQL(ctx, cmd, msg.RequestID)
	case "get_databases":
		ws.handleGetDatabases(ctx, cmd, msg.RequestID)
	case "get_tables":
		ws.handleGetTables(ctx, cmd, msg.RequestID)
	case "get_table_info":
		ws.handleGetTableInfo(ctx, cmd, msg.RequestID)
	default:
		ws.sendError(msg.RequestID, fmt.Sprintf("unknown command type: %s", cmd.Type))
	}
}

// handleExecuteSQL 处理SQL执行命令
func (ws *WebSocketSessionImpl) handleExecuteSQL(ctx context.Context, cmd CommandMessage, requestID string) {
	sql, ok := cmd.Data["sql"].(string)
	if !ok {
		ws.sendError(requestID, "sql parameter is required")
		return
	}

	// 检测SQL类型
	utils := &MySQLUtils{}
	sqlType := utils.DetectSQLType(sql)

	var result interface{}
	var err error

	if sqlType == SqlTypeSelect {
		ws.mutex.Lock()
		ws.metrics.QueriesExecuted++
		ws.mutex.Unlock()

		result, err = ws.dbConn.ExecuteQuery(ctx, sql)
	} else {
		ws.mutex.Lock()
		ws.metrics.UpdatesExecuted++
		ws.mutex.Unlock()

		result, err = ws.dbConn.ExecuteUpdate(ctx, sql)
	}

	if err != nil {
		ws.mutex.Lock()
		ws.metrics.ErrorsCount++
		ws.mutex.Unlock()

		ws.sendError(requestID, err.Error())
		return
	}

	ws.sendResult(requestID, result)
}

// handleGetDatabases 处理获取数据库列表命令
func (ws *WebSocketSessionImpl) handleGetDatabases(ctx context.Context, cmd CommandMessage, requestID string) {
	databases, err := ws.dbConn.GetDatabases(ctx)
	if err != nil {
		ws.sendError(requestID, err.Error())
		return
	}

	ws.sendResult(requestID, map[string]interface{}{
		"databases": databases,
	})
}

// handleGetTables 处理获取表列表命令
func (ws *WebSocketSessionImpl) handleGetTables(ctx context.Context, cmd CommandMessage, requestID string) {
	database, ok := cmd.Data["database"].(string)
	if !ok {
		ws.sendError(requestID, "database parameter is required")
		return
	}

	tables, err := ws.dbConn.GetTables(ctx, database)
	if err != nil {
		ws.sendError(requestID, err.Error())
		return
	}

	ws.sendResult(requestID, map[string]interface{}{
		"tables": tables,
	})
}

// handleGetTableInfo 处理获取表信息命令
func (ws *WebSocketSessionImpl) handleGetTableInfo(ctx context.Context, cmd CommandMessage, requestID string) {
	database, ok := cmd.Data["database"].(string)
	if !ok {
		ws.sendError(requestID, "database parameter is required")
		return
	}

	table, ok := cmd.Data["table"].(string)
	if !ok {
		ws.sendError(requestID, "table parameter is required")
		return
	}

	columns, err := ws.dbConn.GetTableColumns(ctx, database, table)
	if err != nil {
		ws.sendError(requestID, err.Error())
		return
	}

	ws.sendResult(requestID, map[string]interface{}{
		"columns": columns,
	})
}

// handleHeartbeat 处理心跳
func (ws *WebSocketSessionImpl) handleHeartbeat(msg *WSMessage) {
	response := &WSMessage{
		Type:      WSMsgTypeHeartbeat,
		Data:      map[string]interface{}{"pong": time.Now().Unix()},
		Timestamp: time.Now(),
		SessionID: ws.sessionID,
		RequestID: msg.RequestID,
	}

	select {
	case ws.messageChan <- response:
	default:
		// 忽略
	}
}

// sendResult 发送结果
func (ws *WebSocketSessionImpl) sendResult(requestID string, data interface{}) {
	msg := &WSMessage{
		Type:      WSMsgTypeResult,
		Data:      data,
		Timestamp: time.Now(),
		SessionID: ws.sessionID,
		RequestID: requestID,
	}

	select {
	case ws.messageChan <- msg:
	default:
		// 忽略
	}
}

// sendError 发送错误
func (ws *WebSocketSessionImpl) sendError(requestID, errorMsg string) {
	msg := &WSMessage{
		Type:      WSMsgTypeError,
		Data:      map[string]interface{}{"error": errorMsg},
		Timestamp: time.Now(),
		SessionID: ws.sessionID,
		RequestID: requestID,
	}

	select {
	case ws.messageChan <- msg:
	default:
		// 忽略
	}
}

// readMessages 读取WebSocket消息
func (ws *WebSocketSessionImpl) readMessages(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			if ws.errorHandler != nil {
				ws.errorHandler(fmt.Errorf("websocket read panic: %v", r))
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ws.stopChan:
			return
		default:
			if ws.conn == nil {
				return
			}

			_, messageData, err := ws.conn.ReadMessage()
			if err != nil {
				if ws.errorHandler != nil {
					ws.errorHandler(fmt.Errorf("websocket read error: %w", err))
				}
				return
			}

			var msg WSMessage
			if err := json.Unmarshal(messageData, &msg); err != nil {
				if ws.errorHandler != nil {
					ws.errorHandler(fmt.Errorf("message unmarshal error: %w", err))
				}
				continue
			}

			select {
			case ws.messageChan <- &msg:
			default:
				// 消息队列满，忽略
			}
		}
	}
}

// writeMessages 写入WebSocket消息
func (ws *WebSocketSessionImpl) writeMessages(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			if ws.errorHandler != nil {
				ws.errorHandler(fmt.Errorf("websocket write panic: %v", r))
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ws.stopChan:
			return
		case msg := <-ws.messageChan:
			if ws.conn == nil {
				return
			}

			data, err := json.Marshal(msg)
			if err != nil {
				if ws.errorHandler != nil {
					ws.errorHandler(fmt.Errorf("message marshal error: %w", err))
				}
				continue
			}

			if err := ws.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				if ws.errorHandler != nil {
					ws.errorHandler(fmt.Errorf("websocket write error: %w", err))
				}
				return
			}
		}
	}
}

// CommandMessage 命令消息
type CommandMessage struct {
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data"`
	RequestID string                 `json:"request_id"`
}
