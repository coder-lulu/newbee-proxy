package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"newbee-agent/internal/svc"
	"newbee-agent/plugins/guac"
	"newbee-agent/plugins/ws"

	"github.com/zeromicro/go-zero/core/logx"
)

// GuacamoleWebSocketHandler 基于开源Guacamole实现的WebSocket处理器
type GuacamoleWebSocketHandler struct {
	svcCtx *svc.ServiceContext
	logger logx.Logger
	server *guac.Server
}

// NewGuacamoleWebSocketHandler 创建基于开源实现的WebSocket处理器
func NewGuacamoleWebSocketHandler(svcCtx *svc.ServiceContext) *GuacamoleWebSocketHandler {
	handler := &GuacamoleWebSocketHandler{
		svcCtx: svcCtx,
		logger: svcCtx.Logger,
	}

	// 创建Guacamole服务器，使用开源的DoConnect函数
	handler.server = guac.NewServer(handler.connect)

	return handler
}

// HandleGuacamoleWebSocket 处理WebSocket连接，完全基于开源方案
func (h *GuacamoleWebSocketHandler) HandleGuacamoleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 使用现有的WebSocket升级器
	conn, err := ws.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Errorf("WebSocket升级失败: %v", err)
		return
	}
	defer conn.Close()

	h.logger.Info("新的Guacamole WebSocket连接建立")

	// 创建隧道连接
	tunnel, err := h.connect(r)
	if err != nil {
		h.logger.Errorf("创建Guacamole隧道失败: %v", err)
		return
	}
	defer tunnel.Close()

	h.logger.Infof("Guacamole隧道创建成功，UUID: %s", tunnel.GetUUID())

	// 启动双向数据传输
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	// 错误通道，用于传递goroutine中的错误
	errChan := make(chan error, 2)

	// WebSocket到Guacd的数据传输
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				h.logger.Errorf("WsToGuacd goroutine panic: %v", r)
				errChan <- fmt.Errorf("WsToGuacd panic: %v", r)
			}
		}()

		writer := tunnel.AcquireWriter()
		defer tunnel.ReleaseWriter()

		// 使用context控制goroutine生命周期
		go func() {
			<-ctx.Done()
			// 当context取消时，尝试关闭连接以中断阻塞的读写操作
			if conn != nil {
				conn.Close()
			}
		}()

		guac.WsToGuacd(conn, tunnel, writer)
		errChan <- fmt.Errorf("WsToGuacd completed")
	}()

	// Guacd到WebSocket的数据传输
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				h.logger.Errorf("GuacdToWs goroutine panic: %v", r)
				errChan <- fmt.Errorf("GuacdToWs panic: %v", r)
			}
		}()

		reader := tunnel.AcquireReader()
		defer tunnel.ReleaseReader()

		// 使用context控制goroutine生命周期
		go func() {
			<-ctx.Done()
			// 当context取消时，尝试关闭隧道以中断阻塞的读写操作
			if tunnel != nil {
				tunnel.Close()
			}
		}()

		guac.GuacdToWs(conn, tunnel, reader)
		errChan <- fmt.Errorf("GuacdToWs completed")
	}()

	// 监听第一个完成/错误的goroutine
	go func() {
		select {
		case err := <-errChan:
			h.logger.Infof("数据传输完成或出错: %v", err)
			cancel() // 取消context，触发其他goroutine退出
		case <-ctx.Done():
			h.logger.Info("Context已取消，停止数据传输")
		}
	}()

	// 等待连接结束或超时
	select {
	case <-ctx.Done():
		h.logger.Info("Guacamole WebSocket连接上下文取消")
	case <-time.After(1 * time.Hour): // 设置最大连接时间为1小时
		h.logger.Info("Guacamole WebSocket连接超时，强制关闭")
		cancel()
	}

	// 等待所有goroutine完成，但设置超时避免无限等待
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		h.logger.Info("所有数据传输goroutine已完成")
	case <-time.After(10 * time.Second):
		h.logger.Errorf("等待goroutine完成超时，可能存在goroutine泄漏")
	}
}

// connect 创建到guacd的连接，使用开源的DoConnect函数
func (h *GuacamoleWebSocketHandler) connect(r *http.Request) (guac.Tunnel, error) {
	query := r.URL.Query()

	// 从查询参数中提取连接参数
	parameters := map[string]string{
		"scheme":   "rdp", // 默认使用RDP协议
		"hostname": query.Get("hostname"),
		"port":     query.Get("port"),
		"username": query.Get("username"),
		"password": query.Get("password"),
	}

	// 从配置读取默认值
	config := h.svcCtx.Config.Plugins.RDP

	// 设置默认端口
	if parameters["port"] == "" {
		parameters["port"] = "3389"
	}

	// 设置默认屏幕尺寸（从配置读取）
	if config.Connection != nil {
		if query.Get("width") == "" && config.Connection.DefaultWidth > 0 {
			query.Set("width", strconv.Itoa(config.Connection.DefaultWidth))
		}
		if query.Get("height") == "" && config.Connection.DefaultHeight > 0 {
			query.Set("height", strconv.Itoa(config.Connection.DefaultHeight))
		}

		// 设置颜色深度
		if config.Connection.ColorDepth > 0 {
			parameters["color-depth"] = strconv.Itoa(config.Connection.ColorDepth)
		}

		// 设置DPI
		if config.Connection.DPI > 0 {
			parameters["dpi"] = strconv.Itoa(config.Connection.DPI)
		}
	}

	// 验证必需参数
	if parameters["hostname"] == "" || parameters["username"] == "" || parameters["password"] == "" {
		return nil, fmt.Errorf("缺少必需参数: hostname, username, password")
	}

	h.logger.Infof("创建RDP连接: %s:%s@%s:%s",
		parameters["username"], "***", parameters["hostname"], parameters["port"])

	// 从配置构建Guacd配置
	guacdConfig := h.buildGuacdConfig()

	// 使用配置的DoConnectWithConfig函数创建隧道
	username := parameters["username"]
	return guac.DoConnectWithConfig(query, parameters, username, guacdConfig)
}

// buildGuacdConfig 从配置文件构建Guacd配置
func (h *GuacamoleWebSocketHandler) buildGuacdConfig() *guac.GuacdConfig {
	rdpConfig := h.svcCtx.Config.Plugins.RDP

	// 使用默认配置
	config := guac.DefaultGuacdConfig()

	// 如果配置文件中有Guacd配置，则覆盖默认值
	if rdpConfig.Guacd != nil {
		if rdpConfig.Guacd.Address != "" {
			config.Address = rdpConfig.Guacd.Address
		}

		if len(rdpConfig.Guacd.Fallbacks) > 0 {
			config.FallbackAddresses = rdpConfig.Guacd.Fallbacks
		}

		// 解析超时时间
		if rdpConfig.Guacd.ConnectTimeout != "" {
			if timeout, err := time.ParseDuration(rdpConfig.Guacd.ConnectTimeout); err == nil {
				config.ConnectTimeout = timeout
			} else {
				h.logger.Errorf("解析连接超时配置失败: %v", err)
			}
		}

		if rdpConfig.Guacd.HealthCheckInterval != "" {
			if interval, err := time.ParseDuration(rdpConfig.Guacd.HealthCheckInterval); err == nil {
				config.HealthCheckInterval = interval
			} else {
				h.logger.Errorf("解析健康检查间隔配置失败: %v", err)
			}
		}
	}

	h.logger.Infof("使用Guacd配置: 主地址=%s, 备用地址=%v, 连接超时=%v",
		config.Address, config.FallbackAddresses, config.ConnectTimeout)

	return config
}

// GetTunnelUUID 获取隧道UUID（用于HTTP连接模式）
func (h *GuacamoleWebSocketHandler) GetTunnelUUID(w http.ResponseWriter, r *http.Request) {
	tunnel, err := h.connect(r)
	if err != nil {
		h.logger.Errorf("创建隧道失败: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(tunnel.GetUUID()))
}

// ServeHTTP 处理HTTP隧道请求（用于非WebSocket模式）
func (h *GuacamoleWebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.server.ServeHTTP(w, r)
}
