package db

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

    "github.com/coder-lulu/newbee-proxy/plugins/common"

	"github.com/gorilla/websocket"
)

// DbWebSocketSession 数据库WebSocket会话，支持连续执行
type DbWebSocketSession struct {
	// 基础信息
	sessionID    string
	connectionID string
	userID       string
	clientID     string

	// WebSocket连接
	wsConn *websocket.Conn
	dbConn DbConnection
	plugin *DbPluginImpl

	// 状态管理
	active    bool
	startTime time.Time
	mutex     sync.RWMutex

	// 消息处理
	messageChan chan *WSMessage
	stopChan    chan struct{}

	// 事件处理器
	messageHandler func(*WSMessage)
	errorHandler   func(error)
	closeHandler   func()

	// 连续执行上下文
	execContext *ExecContext

	// 会话指标
	metrics *SessionMetrics

	// 上下文
	ctx    context.Context
	cancel context.CancelFunc
}

// ExecContext 执行上下文，用于连续执行
type ExecContext struct {
	CurrentDatabase string                 `json:"current_database"`
	Variables       map[string]interface{} `json:"variables"`
	Transaction     Transaction            `json:"-"`
	InTransaction   bool                   `json:"in_transaction"`
	History         []string               `json:"history"`
	LastResult      *QuickExecResult       `json:"last_result"`
	WorkingMode     string                 `json:"working_mode"` // single, batch, interactive
	MaxHistorySize  int                    `json:"max_history_size"`
}

// NewDbWebSocketSession 创建数据库WebSocket会话
func NewDbWebSocketSession(sessionID, connectionID, userID string, wsConn *websocket.Conn, dbConn DbConnection, plugin *DbPluginImpl) *DbWebSocketSession {
	ctx, cancel := context.WithCancel(context.Background())

	return &DbWebSocketSession{
		sessionID:    sessionID,
		connectionID: connectionID,
		userID:       userID,
		wsConn:       wsConn,
		dbConn:       dbConn,
		plugin:       plugin,
		active:       false,
		startTime:    time.Now(),
		messageChan:  make(chan *WSMessage, 100),
		stopChan:     make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
		execContext: &ExecContext{
			Variables:      make(map[string]interface{}),
			History:        make([]string, 0),
			WorkingMode:    "interactive",
			MaxHistorySize: 100,
		},
		metrics: &SessionMetrics{
			SessionID:    sessionID,
			ConnectionID: connectionID,
			StartTime:    time.Now(),
			LastActivity: time.Now(),
		},
	}
}

// Start 启动会话
func (s *DbWebSocketSession) Start() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.active {
		return fmt.Errorf("session already active")
	}

	s.active = true
	s.startTime = time.Now()

	// 启动消息处理协程
	go s.handleMessages()
	go s.readMessages()
	go s.writeMessages()

	// 发送会话启动消息
	s.sendStatus("started", map[string]interface{}{
		"session_id":    s.sessionID,
		"connection_id": s.connectionID,
		"user_id":       s.userID,
		"database_info": s.dbConn.GetDbInfo(),
	})

	LogWebSocket("start", s.sessionID, s.connectionID, time.Since(s.startTime), nil)
	return nil
}

// Stop 停止会话
func (s *DbWebSocketSession) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.active {
		return fmt.Errorf("session not active")
	}

	s.active = false

	// 如果有未完成的事务，回滚
	if s.execContext.InTransaction && s.execContext.Transaction != nil {
		s.execContext.Transaction.Rollback()
		s.execContext.InTransaction = false
		s.execContext.Transaction = nil
	}

	// 停止协程
	s.cancel()
	close(s.stopChan)

	// 关闭WebSocket连接
	if s.wsConn != nil {
		s.wsConn.Close()
	}

	// 调用关闭处理器
	if s.closeHandler != nil {
		s.closeHandler()
	}

	duration := time.Since(s.startTime)
	s.metrics.Duration = duration

	LogWebSocket("stop", s.sessionID, s.connectionID, duration, nil)
	return nil
}

// IsActive 检查会话是否活跃
func (s *DbWebSocketSession) IsActive() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.active
}

// HandleData 处理WebSocket数据
func (s *DbWebSocketSession) HandleData(data []byte) error {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	msg.SessionID = s.sessionID
	msg.Timestamp = time.Now()

	select {
	case s.messageChan <- &msg:
		return nil
	default:
		return fmt.Errorf("message queue full")
	}
}

// SendData 发送数据到WebSocket
func (s *DbWebSocketSession) SendData(data []byte) error {
	if !s.IsActive() {
		return fmt.Errorf("session not active")
	}

	return s.wsConn.WriteMessage(websocket.TextMessage, data)
}

// GetMetrics 获取会话指标
func (s *DbWebSocketSession) GetMetrics() *common.SessionMetrics {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return &common.SessionMetrics{
		SessionID:     s.metrics.SessionID,
		ConnectionID:  s.metrics.ConnectionID,
		Duration:      time.Since(s.metrics.StartTime),
		BytesSent:     s.metrics.BytesSent,
		BytesReceived: s.metrics.BytesReceived,
		CommandsCount: int(s.metrics.CommandsExecuted),
		ErrorsCount:   int(s.metrics.ErrorsCount),
		LastActivity:  s.metrics.LastActivity,
	}
}

// ExecuteSQL 执行SQL（支持连续执行上下文）
func (s *DbWebSocketSession) ExecuteSQL(sql string, options map[string]interface{}) (*QuickExecResult, error) {
	if !s.IsActive() {
		return nil, fmt.Errorf("session not active")
	}

	// 更新活动时间
	s.mutex.Lock()
	s.metrics.LastActivity = time.Now()
	s.metrics.CommandsExecuted++
	s.mutex.Unlock()

	// 添加到历史记录
	s.addToHistory(sql)

	// 创建执行器
	quickExec := NewQuickExec(s.plugin)

	// 构建选项
	execOptions := []QuickExecOption{
		WithTimeout(30 * time.Second),
		WithQuickOperator(s.userID),
		WithRemark(fmt.Sprintf("WebSocket session: %s", s.sessionID)),
	}

	if maxRows, ok := options["max_rows"].(int); ok {
		execOptions = append(execOptions, WithMaxRows(maxRows))
	}

	// 执行SQL
	target := s.buildTarget()
	result, err := quickExec.ExecuteSQL(target, sql, execOptions...)

	// 更新上下文
	s.execContext.LastResult = result

	if err != nil {
		s.mutex.Lock()
		s.metrics.ErrorsCount++
		s.mutex.Unlock()
	} else {
		if result.RowsAffected > 0 {
			s.mutex.Lock()
			s.metrics.UpdatesExecuted++
			s.mutex.Unlock()
		} else {
			s.mutex.Lock()
			s.metrics.QueriesExecuted++
			s.mutex.Unlock()
		}
	}

	return result, err
}

// BeginTransaction 开始事务
func (s *DbWebSocketSession) BeginTransaction() error {
	if s.execContext.InTransaction {
		return fmt.Errorf("transaction already active")
	}

	tx, err := s.dbConn.BeginTransaction()
	if err != nil {
		return err
	}

	s.execContext.Transaction = tx
	s.execContext.InTransaction = true

	s.sendStatus("transaction_started", map[string]interface{}{
		"transaction_id": fmt.Sprintf("tx_%s_%d", s.sessionID, time.Now().Unix()),
	})

	return nil
}

// CommitTransaction 提交事务
func (s *DbWebSocketSession) CommitTransaction() error {
	if !s.execContext.InTransaction || s.execContext.Transaction == nil {
		return fmt.Errorf("no active transaction")
	}

	err := s.execContext.Transaction.Commit()
	s.execContext.InTransaction = false
	s.execContext.Transaction = nil

	status := "transaction_committed"
	if err != nil {
		status = "transaction_commit_failed"
	}

	s.sendStatus(status, map[string]interface{}{
		"error": err,
	})

	return err
}

// RollbackTransaction 回滚事务
func (s *DbWebSocketSession) RollbackTransaction() error {
	if !s.execContext.InTransaction || s.execContext.Transaction == nil {
		return fmt.Errorf("no active transaction")
	}

	err := s.execContext.Transaction.Rollback()
	s.execContext.InTransaction = false
	s.execContext.Transaction = nil

	status := "transaction_rolled_back"
	if err != nil {
		status = "transaction_rollback_failed"
	}

	s.sendStatus(status, map[string]interface{}{
		"error": err,
	})

	return err
}

// GetDatabases 获取数据库列表
func (s *DbWebSocketSession) GetDatabases() ([]string, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	if baseConn, ok := s.dbConn.(common.Connection); ok {
		return s.plugin.GetDatabases(ctx, baseConn.ID())
	}
	return nil, fmt.Errorf("invalid connection type")
}

// GetTables 获取表列表
func (s *DbWebSocketSession) GetTables(database string) ([]*TableInfo, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	if baseConn, ok := s.dbConn.(common.Connection); ok {
		return s.plugin.GetTables(ctx, baseConn.ID(), database)
	}
	return nil, fmt.Errorf("invalid connection type")
}

// SwitchDatabase 切换数据库
func (s *DbWebSocketSession) SwitchDatabase(database string) error {
	err := s.dbConn.SetDatabase(database)
	if err == nil {
		s.execContext.CurrentDatabase = database
		s.sendStatus("database_switched", map[string]interface{}{
			"database": database,
		})
	}
	return err
}

// 内部方法

func (s *DbWebSocketSession) handleMessages() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.stopChan:
			return
		case msg := <-s.messageChan:
			s.processMessage(msg)
		}
	}
}

func (s *DbWebSocketSession) processMessage(msg *WSMessage) {
	switch msg.Type {
	case WSMsgTypeCommand:
		s.handleCommand(msg)
	case WSMsgTypeHeartbeat:
		s.handleHeartbeat(msg)
	default:
		if s.messageHandler != nil {
			s.messageHandler(msg)
		}
	}
}

func (s *DbWebSocketSession) handleCommand(msg *WSMessage) {
	cmd, ok := msg.Data.(map[string]interface{})
	if !ok {
		s.sendError(msg.RequestID, "invalid command format")
		return
	}

	cmdType, ok := cmd["type"].(string)
	if !ok {
		s.sendError(msg.RequestID, "command type is required")
		return
	}

	switch cmdType {
	case "execute_sql":
		s.handleExecuteSQL(cmd, msg.RequestID)
	case "get_databases":
		s.handleGetDatabases(msg.RequestID)
	case "get_tables":
		s.handleGetTables(cmd, msg.RequestID)
	case "switch_database":
		s.handleSwitchDatabase(cmd, msg.RequestID)
	case "begin_transaction":
		s.handleBeginTransaction(msg.RequestID)
	case "commit_transaction":
		s.handleCommitTransaction(msg.RequestID)
	case "rollback_transaction":
		s.handleRollbackTransaction(msg.RequestID)
	case "get_context":
		s.handleGetContext(msg.RequestID)
	case "clear_history":
		s.handleClearHistory(msg.RequestID)
	default:
		s.sendError(msg.RequestID, fmt.Sprintf("unknown command type: %s", cmdType))
	}
}

func (s *DbWebSocketSession) handleExecuteSQL(cmd map[string]interface{}, requestID string) {
	sql, ok := cmd["sql"].(string)
	if !ok {
		s.sendError(requestID, "sql parameter is required")
		return
	}

	options := make(map[string]interface{})
	if opts, ok := cmd["options"].(map[string]interface{}); ok {
		options = opts
	}

	result, err := s.ExecuteSQL(sql, options)
	if err != nil {
		s.sendError(requestID, err.Error())
		return
	}

	s.sendResult(requestID, result)
}

func (s *DbWebSocketSession) handleGetDatabases(requestID string) {
	databases, err := s.GetDatabases()
	if err != nil {
		s.sendError(requestID, err.Error())
		return
	}

	s.sendResult(requestID, map[string]interface{}{
		"databases": databases,
	})
}

func (s *DbWebSocketSession) handleGetTables(cmd map[string]interface{}, requestID string) {
	database, ok := cmd["database"].(string)
	if !ok {
		s.sendError(requestID, "database parameter is required")
		return
	}

	tables, err := s.GetTables(database)
	if err != nil {
		s.sendError(requestID, err.Error())
		return
	}

	s.sendResult(requestID, map[string]interface{}{
		"tables": tables,
	})
}

func (s *DbWebSocketSession) handleSwitchDatabase(cmd map[string]interface{}, requestID string) {
	database, ok := cmd["database"].(string)
	if !ok {
		s.sendError(requestID, "database parameter is required")
		return
	}

	err := s.SwitchDatabase(database)
	if err != nil {
		s.sendError(requestID, err.Error())
		return
	}

	s.sendResult(requestID, map[string]interface{}{
		"success":  true,
		"database": database,
	})
}

func (s *DbWebSocketSession) handleBeginTransaction(requestID string) {
	err := s.BeginTransaction()
	if err != nil {
		s.sendError(requestID, err.Error())
		return
	}

	s.sendResult(requestID, map[string]interface{}{
		"success":             true,
		"transaction_started": true,
	})
}

func (s *DbWebSocketSession) handleCommitTransaction(requestID string) {
	err := s.CommitTransaction()
	if err != nil {
		s.sendError(requestID, err.Error())
		return
	}

	s.sendResult(requestID, map[string]interface{}{
		"success":               true,
		"transaction_committed": true,
	})
}

func (s *DbWebSocketSession) handleRollbackTransaction(requestID string) {
	err := s.RollbackTransaction()
	if err != nil {
		s.sendError(requestID, err.Error())
		return
	}

	s.sendResult(requestID, map[string]interface{}{
		"success":                 true,
		"transaction_rolled_back": true,
	})
}

func (s *DbWebSocketSession) handleGetContext(requestID string) {
	s.sendResult(requestID, s.execContext)
}

func (s *DbWebSocketSession) handleClearHistory(requestID string) {
	s.execContext.History = make([]string, 0)
	s.sendResult(requestID, map[string]interface{}{
		"success":         true,
		"history_cleared": true,
	})
}

func (s *DbWebSocketSession) handleHeartbeat(msg *WSMessage) {
	response := &WSMessage{
		Type:      WSMsgTypeHeartbeat,
		Data:      map[string]interface{}{"pong": time.Now().Unix()},
		Timestamp: time.Now(),
		SessionID: s.sessionID,
		RequestID: msg.RequestID,
	}

	select {
	case s.messageChan <- response:
	default:
		// 忽略
	}
}

func (s *DbWebSocketSession) readMessages() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.stopChan:
			return
		default:
			if s.wsConn == nil {
				return
			}

			_, messageData, err := s.wsConn.ReadMessage()
			if err != nil {
				if s.errorHandler != nil {
					s.errorHandler(fmt.Errorf("websocket read error: %w", err))
				}
				return
			}

			if err := s.HandleData(messageData); err != nil {
				if s.errorHandler != nil {
					s.errorHandler(err)
				}
			}
		}
	}
}

func (s *DbWebSocketSession) writeMessages() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.stopChan:
			return
		case msg := <-s.messageChan:
			if s.wsConn == nil {
				return
			}

			data, err := json.Marshal(msg)
			if err != nil {
				if s.errorHandler != nil {
					s.errorHandler(fmt.Errorf("message marshal error: %w", err))
				}
				continue
			}

			if err := s.wsConn.WriteMessage(websocket.TextMessage, data); err != nil {
				if s.errorHandler != nil {
					s.errorHandler(fmt.Errorf("websocket write error: %w", err))
				}
				return
			}

			s.mutex.Lock()
			s.metrics.BytesSent += int64(len(data))
			s.mutex.Unlock()
		}
	}
}

func (s *DbWebSocketSession) sendResult(requestID string, data interface{}) {
	msg := &WSMessage{
		Type:      WSMsgTypeResult,
		Data:      data,
		Timestamp: time.Now(),
		SessionID: s.sessionID,
		RequestID: requestID,
	}

	select {
	case s.messageChan <- msg:
	default:
		// 忽略
	}
}

func (s *DbWebSocketSession) sendError(requestID, errorMsg string) {
	msg := &WSMessage{
		Type:      WSMsgTypeError,
		Data:      map[string]interface{}{"error": errorMsg},
		Timestamp: time.Now(),
		SessionID: s.sessionID,
		RequestID: requestID,
	}

	select {
	case s.messageChan <- msg:
	default:
		// 忽略
	}
}

func (s *DbWebSocketSession) sendStatus(status string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["status"] = status

	msg := &WSMessage{
		Type:      WSMsgTypeStatus,
		Data:      data,
		Timestamp: time.Now(),
		SessionID: s.sessionID,
	}

	select {
	case s.messageChan <- msg:
	default:
		// 忽略
	}
}

func (s *DbWebSocketSession) addToHistory(sql string) {
	s.execContext.History = append(s.execContext.History, sql)

	// 限制历史记录大小
	if len(s.execContext.History) > s.execContext.MaxHistorySize {
		s.execContext.History = s.execContext.History[1:]
	}
}

func (s *DbWebSocketSession) buildTarget() string {
	dbInfo := s.dbConn.GetDbInfo()
	return fmt.Sprintf("%s://%s:%d/%s", dbInfo.Type, dbInfo.Host, dbInfo.Port, dbInfo.Database)
}

// OnMessage 设置消息处理器
func (s *DbWebSocketSession) OnMessage(handler func(*WSMessage)) {
	s.messageHandler = handler
}

// OnError 设置错误处理器
func (s *DbWebSocketSession) OnError(handler func(error)) {
	s.errorHandler = handler
}

// OnClose 设置关闭处理器
func (s *DbWebSocketSession) OnClose(handler func()) {
	s.closeHandler = handler
}

// 实现common.Session接口
func (s *DbWebSocketSession) ID() string {
	return s.sessionID
}

func (s *DbWebSocketSession) ConnectionID() string {
	return s.connectionID
}

func (s *DbWebSocketSession) UserID() string {
	return s.userID
}
