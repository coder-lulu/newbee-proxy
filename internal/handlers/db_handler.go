package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

    "github.com/coder-lulu/newbee-proxy/internal/svc"
    "github.com/coder-lulu/newbee-proxy/plugins/common"
    "github.com/coder-lulu/newbee-proxy/plugins/db"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
)

// DbHandler 数据库处理器
type DbHandler struct {
	svcCtx   *svc.ServiceContext
	logger   logx.Logger
	upgrader websocket.Upgrader
}

// NewDbHandler 创建新的数据库处理器
func NewDbHandler(svcCtx *svc.ServiceContext) *DbHandler {
	return &DbHandler{
		svcCtx: svcCtx,
		logger: logx.WithContext(context.Background()),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // 允许跨域，生产环境需要更严格的检查
			},
		},
	}
}

// TestConnection 测试数据库连接
func (h *DbHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type     string            `json:"type"` // mysql, postgresql, etc.
		Host     string            `json:"host"`
		Port     int               `json:"port"`
		Database string            `json:"database"`
		Username string            `json:"username"`
		Password string            `json:"password"`
		SSLMode  string            `json:"ssl_mode,omitempty"`
		Params   map[string]string `json:"params,omitempty"`
		Timeout  int               `json:"timeout,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// 获取DB插件
	plugin, exists := h.svcCtx.PluginManager.GetPlugin("db")
	if !exists {
		h.writeError(w, http.StatusServiceUnavailable, "Database plugin not available", nil)
		return
	}

	dbPlugin, ok := plugin.(*db.DbPluginImpl)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "Invalid database plugin type", nil)
		return
	}

	// 构建数据库配置
	dbConfig := &db.DbConfig{
		Type:     db.DbType(req.Type),
		Host:     req.Host,
		Port:     req.Port,
		Database: req.Database,
		SSLMode:  req.SSLMode,
		Params:   req.Params,
	}

	if req.Timeout > 0 {
		dbConfig.QueryTimeout = time.Duration(req.Timeout) * time.Second
	}

	// 构建认证信息
	credentials := &common.Credentials{
		Username: req.Username,
		Password: req.Password,
		AuthType: "password",
		Timeout:  30,
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := dbPlugin.TestConnection(ctx, dbConfig, credentials); err != nil {
		h.writeError(w, http.StatusBadRequest, "Connection test failed", err)
		return
	}

	h.writeSuccess(w, map[string]interface{}{
		"message": "Connection test successful",
		"type":    req.Type,
		"host":    req.Host,
		"port":    req.Port,
	})
}

// CreateConnection 创建数据库连接
func (h *DbHandler) CreateConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type     string            `json:"type"`
		Host     string            `json:"host"`
		Port     int               `json:"port"`
		Database string            `json:"database"`
		Username string            `json:"username"`
		Password string            `json:"password"`
		SSLMode  string            `json:"ssl_mode,omitempty"`
		Params   map[string]string `json:"params,omitempty"`
		Timeout  int               `json:"timeout,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// 获取DB插件
	plugin, exists := h.svcCtx.PluginManager.GetPlugin("db")
	if !exists {
		h.writeError(w, http.StatusServiceUnavailable, "Database plugin not available", nil)
		return
	}

	dbPlugin, ok := plugin.(*db.DbPluginImpl)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "Invalid database plugin type", nil)
		return
	}

	// 构建数据库配置
	dbConfig := &db.DbConfig{
		Type:     db.DbType(req.Type),
		Host:     req.Host,
		Port:     req.Port,
		Database: req.Database,
		SSLMode:  req.SSLMode,
		Params:   req.Params,
	}

	if req.Timeout > 0 {
		dbConfig.QueryTimeout = time.Duration(req.Timeout) * time.Second
	}

	// 构建认证信息
	credentials := &common.Credentials{
		Username: req.Username,
		Password: req.Password,
		AuthType: "password",
		Timeout:  30,
	}

	// 创建连接
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	conn, err := dbPlugin.CreateDbConnection(ctx, dbConfig, credentials)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Failed to create connection", err)
		return
	}

	h.writeSuccess(w, map[string]interface{}{
		"connection_id": conn.ID(),
		"type":          req.Type,
		"host":          req.Host,
		"port":          req.Port,
		"database":      req.Database,
		"status":        "connected",
	})
}

// ExecuteSQL 执行SQL语句
func (h *DbHandler) ExecuteSQL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConnectionID string `json:"connection_id"`
		SQL          string `json:"sql"`
		MaxRows      int    `json:"max_rows,omitempty"`
		Timeout      int    `json:"timeout,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// 获取DB插件
	plugin, exists := h.svcCtx.PluginManager.GetPlugin("db")
	if !exists {
		h.writeError(w, http.StatusServiceUnavailable, "Database plugin not available", nil)
		return
	}

	dbPlugin, ok := plugin.(*db.DbPluginImpl)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "Invalid database plugin type", nil)
		return
	}

	// 设置超时时间（有限制）
	timeout := 60 * time.Second // 默认60秒
	if req.Timeout > 0 {
		requestTimeout := time.Duration(req.Timeout) * time.Second
		// 限制最大超时时间为10分钟
		if requestTimeout > 10*time.Minute {
			requestTimeout = 10 * time.Minute
		}
		// 限制最小超时时间为5秒
		if requestTimeout < 5*time.Second {
			requestTimeout = 5 * time.Second
		}
		timeout = requestTimeout
	}

	// 构建执行选项
	options := &db.ExecOptions{
		MaxRows: req.MaxRows,
		Timeout: timeout,
	}

	// 执行SQL（带超时控制）
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := dbPlugin.ExecuteSQL(ctx, req.ConnectionID, req.SQL, options)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Failed to execute SQL", err)
		return
	}

	h.writeSuccess(w, result)
}

// GetDatabases 获取数据库列表
func (h *DbHandler) GetDatabases(w http.ResponseWriter, r *http.Request) {
	connectionID := r.URL.Query().Get("connection_id")
	if connectionID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing connection_id parameter", nil)
		return
	}

	// 获取DB插件
	plugin, exists := h.svcCtx.PluginManager.GetPlugin("db")
	if !exists {
		h.writeError(w, http.StatusServiceUnavailable, "Database plugin not available", nil)
		return
	}

	dbPlugin, ok := plugin.(*db.DbPluginImpl)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "Invalid database plugin type", nil)
		return
	}

	// 获取数据库列表
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	databases, err := dbPlugin.GetDatabases(ctx, connectionID)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Failed to get databases", err)
		return
	}

	h.writeSuccess(w, map[string]interface{}{
		"databases": databases,
	})
}

// GetTables 获取表列表
func (h *DbHandler) GetTables(w http.ResponseWriter, r *http.Request) {
	connectionID := r.URL.Query().Get("connection_id")
	database := r.URL.Query().Get("database")

	if connectionID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing connection_id parameter", nil)
		return
	}

	// 获取DB插件
	plugin, exists := h.svcCtx.PluginManager.GetPlugin("db")
	if !exists {
		h.writeError(w, http.StatusServiceUnavailable, "Database plugin not available", nil)
		return
	}

	dbPlugin, ok := plugin.(*db.DbPluginImpl)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "Invalid database plugin type", nil)
		return
	}

	// 获取表列表
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tables, err := dbPlugin.GetTables(ctx, connectionID, database)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Failed to get tables", err)
		return
	}

	h.writeSuccess(w, map[string]interface{}{
		"tables": tables,
	})
}

// GetTableInfo 获取表详细信息
func (h *DbHandler) GetTableInfo(w http.ResponseWriter, r *http.Request) {
	connectionID := r.URL.Query().Get("connection_id")
	database := r.URL.Query().Get("database")
	table := r.URL.Query().Get("table")

	if connectionID == "" || table == "" {
		h.writeError(w, http.StatusBadRequest, "Missing required parameters", nil)
		return
	}

	// 获取DB插件
	plugin, exists := h.svcCtx.PluginManager.GetPlugin("db")
	if !exists {
		h.writeError(w, http.StatusServiceUnavailable, "Database plugin not available", nil)
		return
	}

	dbPlugin, ok := plugin.(*db.DbPluginImpl)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "Invalid database plugin type", nil)
		return
	}

	// 获取表信息
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tableInfo, err := dbPlugin.GetTableInfo(ctx, connectionID, database, table)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Failed to get table info", err)
		return
	}

	h.writeSuccess(w, tableInfo)
}

// CloseConnection 关闭数据库连接
func (h *DbHandler) CloseConnection(w http.ResponseWriter, r *http.Request) {
	connectionID := r.URL.Query().Get("connection_id")
	if connectionID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing connection_id parameter", nil)
		return
	}

	// 获取DB插件
	plugin, exists := h.svcCtx.PluginManager.GetPlugin("db")
	if !exists {
		h.writeError(w, http.StatusServiceUnavailable, "Database plugin not available", nil)
		return
	}

	dbPlugin, ok := plugin.(*db.DbPluginImpl)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "Invalid database plugin type", nil)
		return
	}

	// 关闭连接
	if err := dbPlugin.CloseConnection(connectionID); err != nil {
		h.writeError(w, http.StatusBadRequest, "Failed to close connection", err)
		return
	}

	h.writeSuccess(w, map[string]interface{}{
		"message": "Connection closed successfully",
	})
}

// GetConnectionStats 获取连接统计信息
func (h *DbHandler) GetConnectionStats(w http.ResponseWriter, r *http.Request) {
	// 获取DB插件
	plugin, exists := h.svcCtx.PluginManager.GetPlugin("db")
	if !exists {
		h.writeError(w, http.StatusServiceUnavailable, "Database plugin not available", nil)
		return
	}

	dbPlugin, ok := plugin.(*db.DbPluginImpl)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "Invalid database plugin type", nil)
		return
	}

	// 获取连接统计信息
	stats := dbPlugin.GetStatus()
	connections := dbPlugin.ListConnections()

	h.writeSuccess(w, map[string]interface{}{
		"plugin_status": stats,
		"connections":   connections,
		"total_count":   len(connections),
	})
}

// HandleWebSocket 处理WebSocket连接用于连续SQL执行
func (h *DbHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	connectionID := r.URL.Query().Get("connection_id")
	if connectionID == "" {
		h.logger.Error("Missing connection_id parameter for WebSocket")
		http.Error(w, "Missing connection_id parameter", http.StatusBadRequest)
		return
	}

	// 升级WebSocket连接
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Errorf("Failed to upgrade WebSocket: %v", err)
		return
	}
	defer conn.Close()

	h.logger.Infof("New DB WebSocket connection for connection_id: %s", connectionID)

	// 获取DB插件
	plugin, exists := h.svcCtx.PluginManager.GetPlugin("db")
	if !exists {
		h.sendWebSocketError(conn, "Database plugin not available")
		return
	}

	dbPlugin, ok := plugin.(*db.DbPluginImpl)
	if !ok {
		h.sendWebSocketError(conn, "Invalid database plugin type")
		return
	}

	// 处理WebSocket消息
	for {
		var msg struct {
			Type    string `json:"type"` // query, update, command
			SQL     string `json:"sql"`
			MaxRows int    `json:"max_rows,omitempty"`
			Timeout int    `json:"timeout,omitempty"`
		}

		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.logger.Errorf("WebSocket read error: %v", err)
			}
			break
		}

		h.logger.Infof("Received WebSocket message: type=%s, sql=%s", msg.Type, msg.SQL)

		// 处理超时设置
		timeout := 60 * time.Second // 默认60秒
		if msg.Timeout > 0 {
			requestTimeout := time.Duration(msg.Timeout) * time.Second
			// 限制最大超时时间为10分钟
			if requestTimeout > 10*time.Minute {
				requestTimeout = 10 * time.Minute
			}
			// 限制最小超时时间为5秒
			if requestTimeout < 5*time.Second {
				requestTimeout = 5 * time.Second
			}
			timeout = requestTimeout
		}

		// 处理命令
		ctx, cancel := context.WithTimeout(context.Background(), timeout)

		switch msg.Type {
		case "query", "update":
			options := &db.ExecOptions{
				MaxRows: msg.MaxRows,
				Timeout: timeout,
			}

			result, err := dbPlugin.ExecuteSQL(ctx, connectionID, msg.SQL, options)
			cancel()

			if err != nil {
				h.sendWebSocketMessage(conn, map[string]interface{}{
					"type":  "error",
					"error": err.Error(),
				})
			} else {
				h.sendWebSocketMessage(conn, map[string]interface{}{
					"type":   "result",
					"result": result,
				})
			}

		case "ping":
			cancel()
			h.sendWebSocketMessage(conn, map[string]interface{}{
				"type":    "pong",
				"message": "Database connection is alive",
			})

		default:
			cancel()
			h.sendWebSocketMessage(conn, map[string]interface{}{
				"type":  "error",
				"error": "Unknown message type: " + msg.Type,
			})
		}
	}

	h.logger.Infof("DB WebSocket connection closed for connection_id: %s", connectionID)
}

// 辅助方法

func (h *DbHandler) writeSuccess(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    data,
	})
}

func (h *DbHandler) writeError(w http.ResponseWriter, statusCode int, message string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"success": false,
		"message": message,
	}

	if err != nil {
		response["error"] = err.Error()
		h.logger.Errorf("%s: %v", message, err)
	}

	json.NewEncoder(w).Encode(response)
}

func (h *DbHandler) sendWebSocketMessage(conn *websocket.Conn, message interface{}) {
	if err := conn.WriteJSON(message); err != nil {
		h.logger.Errorf("Failed to send WebSocket message: %v", err)
	}
}

func (h *DbHandler) sendWebSocketError(conn *websocket.Conn, message string) {
	h.sendWebSocketMessage(conn, map[string]interface{}{
		"type":  "error",
		"error": message,
	})
}
