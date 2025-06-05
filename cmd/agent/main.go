package main

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"newbee-agent/internal/config"
	"newbee-agent/internal/handlers"
	"newbee-agent/internal/svc"
	"newbee-agent/internal/types"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/agent.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	if err := conf.Load(*configFile, &c); err != nil {
		logx.Errorf("Failed to load config: %v", err)
		os.Exit(1)
	}

	if err := logx.SetUp(c.Log); err != nil {
		logx.Errorf("Failed to setup log: %v", err)
		os.Exit(1)
	}
	defer logx.Close()

	logx.Infof("Starting Agent %s", c.Agent.Version)

	svcCtx := svc.NewServiceContext(c)

	// 启动服务上下文
	if err := svcCtx.Start(); err != nil {
		logx.Errorf("Failed to start service context: %v", err)
		os.Exit(1)
	}

	server := rest.MustNewServer(c.RestConf)

	// 添加CORS中间件
	server.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// 设置CORS头
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			// 处理预检请求
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next(w, r)
		}
	})

	// 注册HTTP处理器
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/health",
		Handler: healthCheckHandler(svcCtx),
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/status",
		Handler: statusHandler(svcCtx),
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/metrics",
		Handler: metricsHandler(svcCtx),
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/plugins",
		Handler: pluginsHandler(svcCtx),
	})

	// 添加WebSocket隧道路由

	// 保留原有SSH路由以兼容现有客户端 - 使用原有的SSH插件
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/ws/ssh",
		Handler: handlers.WebSocketTunnelHandler(svcCtx),
	})

	// 添加Telnet WebSocket隧道路由
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/ws/telnet",
		Handler: handlers.WebSocketTelnetTunnelHandler(svcCtx),
	})

	// 添加标准Guacamole WebSocket隧道路由 - 支持多协议（RDP/SSH/VNC/TELNET）
	guacHandler := handlers.NewGuacamoleWebSocketHandler(svcCtx)
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/rdp/websocket",
		Handler: guacHandler.HandleGuacamoleWebSocket,
	})

	// SSH通过guacd的路由
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/ssh/websocket",
		Handler: guacHandler.HandleGuacamoleWebSocket,
	})

	// VNC通过guacd的路由
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/vnc/websocket",
		Handler: guacHandler.HandleGuacamoleWebSocket,
	})

	// TELNET通过guacd的路由
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/telnet/websocket",
		Handler: guacHandler.HandleGuacamoleWebSocket,
	})

	// 兼容性路由 - 指向同一个处理器
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/rdp/guacamole",
		Handler: guacHandler.HandleGuacamoleWebSocket,
	})

	// 短路径路由 - 简化前端连接
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/guacamole",
		Handler: guacHandler.HandleGuacamoleWebSocket,
	})

	// 添加统一的终端大小调整路由
	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/ssh/resize",
		Handler: handlers.ResizeTerminalHandler(svcCtx),
	})

	// 添加任务执行路由
	taskHandler := handlers.NewTaskHandler(svcCtx)
	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/task/command",
		Handler: taskHandler.ExecuteCommand,
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/task/script",
		Handler: taskHandler.ExecuteScript,
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/task/status/:taskId",
		Handler: taskHandler.GetTaskStatus,
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/task/active",
		Handler: taskHandler.GetActiveTasks,
	})

	// 添加获取任务结果路由
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/task/result/:taskId",
		Handler: taskHandler.GetTaskResult,
	})

	// 添加任务统计路由
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/task/stats",
		Handler: taskHandler.GetTaskStats,
	})

	// 添加数据库管理路由
	dbHandler := handlers.NewDbHandler(svcCtx)
	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/db/test",
		Handler: dbHandler.TestConnection,
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/db/connect",
		Handler: dbHandler.CreateConnection,
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/db/execute",
		Handler: dbHandler.ExecuteSQL,
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/db/databases",
		Handler: dbHandler.GetDatabases,
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/db/tables",
		Handler: dbHandler.GetTables,
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/db/table/info",
		Handler: dbHandler.GetTableInfo,
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodDelete,
		Path:    "/api/db/disconnect",
		Handler: dbHandler.CloseConnection,
	})

	// 添加数据库连接统计路由
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/db/stats",
		Handler: dbHandler.GetConnectionStats,
	})

	// 添加数据库WebSocket路由
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/db/websocket",
		Handler: dbHandler.HandleWebSocket,
	})

	// 打印启动信息
	logx.Infof("Agent listening on port %d", c.Port)
	logx.Info("=== SSH连接路由 ===")
	logx.Infof("统一SSH WebSocket隧道 (支持跳板机): /ws/ssh/unified")
	logx.Infof("原有SSH WebSocket隧道 (兼容模式): /ws/ssh")
	logx.Infof("SSH通过Guacamole协议: /api/ssh/websocket")
	logx.Info("=== 远程桌面连接路由 ===")
	logx.Infof("RDP WebSocket隧道: /api/rdp/websocket")
	logx.Infof("VNC WebSocket隧道: /api/vnc/websocket")
	logx.Info("=== 终端连接路由 ===")
	logx.Infof("Telnet WebSocket隧道 (直接): /ws/telnet")
	logx.Infof("Telnet通过Guacamole协议: /api/telnet/websocket")
	logx.Info("=== 兼容性路由 ===")
	logx.Infof("Guacamole兼容路由: /api/rdp/guacamole")
	logx.Infof("Guacamole短路径: /guacamole")
	logx.Info("=== 管理API ===")
	logx.Infof("终端大小调整API: /api/ssh/resize")
	logx.Infof("命令执行API: /api/task/command")
	logx.Infof("脚本执行API: /api/task/script")
	logx.Infof("任务状态API: /api/task/status/:taskId")
	logx.Infof("任务结果API: /api/task/result/:taskId")
	logx.Infof("活跃任务API: /api/task/active")
	logx.Infof("任务统计API: /api/task/stats")
	logx.Info("=== 数据库管理API ===")
	logx.Infof("测试连接API: /api/db/test")
	logx.Infof("创建连接API: /api/db/connect")
	logx.Infof("执行SQL API: /api/db/execute")
	logx.Infof("获取数据库列表API: /api/db/databases")
	logx.Infof("获取表列表API: /api/db/tables")
	logx.Infof("获取表信息API: /api/db/table/info")
	logx.Infof("关闭连接API: /api/db/disconnect")
	logx.Infof("数据库WebSocket: /api/db/websocket")

	// 创建关闭信号通道
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 启动HTTP服务器 - 使用goroutine并处理错误
	serverErr := make(chan error, 1)
	go func() {
		logx.Info("Starting HTTP server...")
		server.Start()
	}()

	// 等待退出信号或服务器错误
	select {
	case <-quit:
		logx.Info("Received shutdown signal")
	case err := <-serverErr:
		logx.Errorf("HTTP server error: %v", err)
	}

	logx.Info("Shutting down Agent...")

	// 创建带超时的关闭上下文
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// 使用通道协调关闭过程
	shutdownDone := make(chan struct{})

	go func() {
		defer close(shutdownDone)

		// 关闭HTTP服务器
		logx.Info("Stopping HTTP server...")
		server.Stop()
		logx.Info("HTTP server stopped")

		// 关闭服务上下文
		logx.Info("Stopping service context...")
		if err := svcCtx.Stop(); err != nil {
			logx.Errorf("Failed to stop service context: %v", err)
		} else {
			logx.Info("Service context stopped")
		}

		logx.Info("All components stopped")
	}()

	// 等待关闭完成或超时
	select {
	case <-shutdownDone:
		logx.Info("Graceful shutdown completed")
	case <-shutdownCtx.Done():
		logx.Infof("Shutdown timeout reached, forcing exit")
		// 强制退出
		os.Exit(1)
	}

	logx.Info("Agent stopped successfully")

	// 额外等待确保日志输出完成
	time.Sleep(100 * time.Millisecond)
}

// registerHandlers 注册HTTP路由
func registerHandlers(server *rest.Server, svcCtx *svc.ServiceContext) {
	// 健康检查接口
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/health",
		Handler: healthCheckHandler(svcCtx),
	})

	// Agent状态接口
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/status",
		Handler: statusHandler(svcCtx),
	})

	// 监控指标接口
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/metrics",
		Handler: metricsHandler(svcCtx),
	})

	// 插件状态接口
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/plugins",
		Handler: pluginsHandler(svcCtx),
	})
}

// healthCheckHandler 健康检查处理器
func healthCheckHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 安全获取状态信息，防止崩溃
		var status *types.AgentStatus

		// 使用recover来捕获可能的panic
		defer func() {
			if r := recover(); r != nil {
				logx.Errorf("Health check handler panic: %v", r)
				http.Error(w, "Internal server error", 500)
			}
		}()

		if svcCtx != nil {
			status = svcCtx.GetStatus()
		}

		if status == nil {
			// 如果无法获取状态，返回基本的健康信息
			response := map[string]interface{}{
				"status":          "error",
				"agent_id":        "unknown",
				"version":         "unknown",
				"uptime":          0,
				"active_sessions": 0,
				"ops_connected":   false,
				"loaded_plugins":  []string{},
				"capabilities":    []string{},
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(response)
			return
		}

		// 获取插件详细状态
		pluginStatuses := make(map[string]interface{})
		if svcCtx.PluginManager != nil {
			allPluginStatus := svcCtx.PluginManager.GetAllPluginStatus()
			for name, pluginStatus := range allPluginStatus {
				pluginStatuses[name] = map[string]interface{}{
					"status":      pluginStatus.Status,
					"version":     pluginStatus.Version,
					"connections": pluginStatus.Connections,
					"sessions":    pluginStatus.Sessions,
				}
			}
		}

		// 获取Guacd服务状态（如果RDP插件已加载）
		guacdStatus := "unknown"
		if rdpPlugin, exists := svcCtx.PluginManager.GetPlugin("rdp"); exists {
			if rdpPlugin.IsRunning() {
				guacdStatus = "connected"
			} else {
				guacdStatus = "disconnected"
			}
		}

		response := map[string]interface{}{
			"status":          status.Status,
			"agent_id":        status.ID,
			"version":         status.Version,
			"uptime":          status.Uptime.Seconds(),
			"active_sessions": status.ActiveSessions,
			"ops_connected":   status.ConnectedToOPS,
			"loaded_plugins":  status.LoadedPlugins,
			"capabilities":    status.Capabilities,
			"plugin_details":  pluginStatuses,
			"guacd_status":    guacdStatus,
			"supported_features": map[string]bool{
				"ssh":           true,
				"telnet":        true,
				"rdp":           true,
				"vnc":           true,
				"db_connection": true,
				"websocket":     true,
				"file_transfer": false, // 待实现
				"monitoring":    true,
			},
			"network_info": map[string]interface{}{
				"local_ip":         status.LocalIP,
				"public_ip":        status.PublicIP,
				"network_segments": status.NetworkSegments,
			},
			"resource_usage": map[string]interface{}{
				"memory_usage": status.MemoryUsage,
				"cpu_usage":    status.CPUUsage,
			},
			"last_heartbeat": status.LastHeartbeat.Unix(),
			"start_time":     status.StartTime.Unix(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(response)
	}
}

// statusHandler Agent状态处理器
func statusHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 使用recover来捕获可能的panic
		defer func() {
			if r := recover(); r != nil {
				logx.Errorf("Status handler panic: %v", r)
				http.Error(w, "Internal server error", 500)
			}
		}()

		var status *types.AgentStatus
		if svcCtx != nil {
			status = svcCtx.GetStatus()
		}

		if status == nil {
			http.Error(w, "Unable to get agent status", 503)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(status)
	}
}

// metricsHandler 监控指标处理器
func metricsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		activeSessionCount := 0
		loadedPluginCount := 0

		// 安全检查，防止nil指针
		if svcCtx.SessionManager != nil {
			activeSessionCount = svcCtx.SessionManager.GetActiveSessionCount()
		}

		if svcCtx.PluginManager != nil {
			loadedPluginCount = len(svcCtx.PluginManager.GetLoadedPlugins())
		}

		metrics := map[string]interface{}{
			"total_requests":      0,
			"successful_requests": 0,
			"failed_requests":     0,
			"active_sessions":     activeSessionCount,
			"loaded_plugins":      loadedPluginCount,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(metrics)
	}
}

// pluginsHandler 插件状态处理器
func pluginsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var plugins []string

		// 安全检查，防止nil指针
		if svcCtx.PluginManager != nil {
			plugins = svcCtx.PluginManager.GetLoadedPlugins()
		} else {
			plugins = []string{}
		}

		response := map[string]interface{}{
			"loaded_plugins": plugins,
			"total_count":    len(plugins),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(response)
	}
}

// staticFileHandler 静态文件处理器
func staticFileHandler(filename, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 使用defer捕获panic
		defer func() {
			if r := recover(); r != nil {
				logx.Errorf("Static file handler panic: %v", r)
				http.Error(w, "Internal server error", 500)
			}
		}()

		// 读取文件内容
		content, err := os.ReadFile(filename)
		if err != nil {
			logx.Errorf("Failed to read static file %s: %v", filename, err)
			http.NotFound(w, r)
			return
		}

		// 设置响应头
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		// 发送文件内容
		w.WriteHeader(200)
		w.Write(content)
	}
}
