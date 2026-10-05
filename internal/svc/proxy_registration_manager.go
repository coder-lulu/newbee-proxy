package svc

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/coder-lulu/newbee-proxy/internal/client"
	"github.com/coder-lulu/newbee-proxy/internal/config"
	"github.com/coder-lulu/newbee-proxy/internal/metrics"

	"github.com/zeromicro/go-zero/core/logx"
)

// ProxyRegistrationManager 负责向 Ops Center 注册与心跳
type ProxyRegistrationManager struct {
	cfg              config.OpsCenterConf
	logger           logx.Logger
	client           *client.OpsCenterClient
	svcCtx           *ServiceContext
	metricsCollector *metrics.SystemMetrics
	quit             chan struct{}
}

func NewProxyRegistrationManager(c config.OpsCenterConf, logger logx.Logger, svc *ServiceContext) *ProxyRegistrationManager {
	return &ProxyRegistrationManager{
		cfg:              c,
		logger:           logger,
		svcCtx:           svc,
		metricsCollector: metrics.NewSystemMetrics(),
		quit:             make(chan struct{}),
		client: func() *client.OpsCenterClient {
			oc := client.NewOpsCenterClient(c.Endpoints, logger)
			oc.PSK = c.PSK
			return oc
		}(),
	}
}

func (m *ProxyRegistrationManager) Start() {
	if !m.cfg.Enabled || len(m.cfg.Endpoints) == 0 {
		m.logger.Info("OpsCenter registration disabled or no endpoints")
		return
	}
	// 立即注册
	if err := m.register(); err != nil {
		m.logger.Errorf("register failed: %v", err)
	}
	// 心跳循环
	interval := m.cfg.HeartbeatSeconds
	if interval <= 0 {
		interval = 30
	}
	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := m.heartbeat(); err != nil {
					m.logger.Errorf("heartbeat failed: %v", err)
				}
			case <-m.quit:
				return
			}
		}
	}()
}

func (m *ProxyRegistrationManager) Stop() { close(m.quit) }

func (m *ProxyRegistrationManager) workerID() string {
	// 优先使用WorkerID，然后ProxyID
	if m.cfg.WorkerID != "" {
		return m.cfg.WorkerID
	}
	if m.cfg.ProxyID != "" {
		return m.cfg.ProxyID
	}
	return ""
}

func (m *ProxyRegistrationManager) zone() string {
	// 优先使用Zone，然后使用AZ（向后兼容）
	if m.cfg.Zone != "" {
		return m.cfg.Zone
	}
	return m.cfg.AZ
}

func (m *ProxyRegistrationManager) httpEndpoint() string {
	scheme := "http"
	if m.svcCtx.Config.Security.EnableTLS {
		scheme = "https"
	}
	host := m.getIP() // 使用统一的IP获取逻辑
	return fmt.Sprintf("%s://%s:%d", scheme, host, m.svcCtx.Config.Port)
}

func (m *ProxyRegistrationManager) wsEndpoint() string {
	scheme := "ws"
	if m.svcCtx.Config.Security.EnableTLS {
		scheme = "wss"
	}
	host := m.getIP() // 使用统一的IP获取逻辑
	return fmt.Sprintf("%s://%s:%d", scheme, host, m.svcCtx.Config.Port)
}

func (m *ProxyRegistrationManager) grpcEndpoint() string {
	host := m.getIP() // 使用统一的IP获取逻辑
	// 从Grpc.ListenOn提取端口（格式：":9000" 或 "0.0.0.0:9000"）
	port := "9000" // 默认端口
	if m.svcCtx.Config.Grpc.Enabled && m.svcCtx.Config.Grpc.ListenOn != "" {
		listenAddr := m.svcCtx.Config.Grpc.ListenOn
		if len(listenAddr) > 0 && listenAddr[0] == ':' {
			port = listenAddr[1:] // 去掉冒号
		}
	}
	return fmt.Sprintf("%s:%s", host, port)
}

func (m *ProxyRegistrationManager) register() error {
	// Ops API 使用 proxy_id，身份仍优先取 WorkerID。
	payload := map[string]any{
		"worker_id":    m.workerID(),
		"proxy_id":     m.workerID(),
		"name":         m.workerID(),
		"ip":           m.getIP(),
		"port":         m.svcCtx.Config.Port,
		"version":      "",
		"region":       m.cfg.Region,
		"zone":         m.zone(),
		"capabilities": []string{},
		"tags":         m.cfg.Tags,
		"endpoints": map[string]string{
			"http": m.httpEndpoint(),
			"ws":   m.wsEndpoint(),
			"grpc": m.grpcEndpoint(),
		},
		"health_check_url": m.cfg.HealthCheckURL,
		"max_sessions":     m.svcCtx.Config.Limits.MaxConcurrentSessions,
		"local_ip":         m.svcCtx.Config.Network.LocalIP,
		"public_ip":        m.svcCtx.Config.Network.PublicIP,
		"network_segments": m.svcCtx.Config.Network.NetworkSegments,
		"metadata":         m.buildMetadata(),
		"psk":              m.cfg.PSK,
	}

	m.logger.Infof("Registering Worker with Ops Center, worker_id=%s, endpoint=%s",
		m.workerID(), m.cfg.Endpoints[0])

	return m.client.Register(payload)
}

func (m *ProxyRegistrationManager) buildMetadata() map[string]string {
	metadata := make(map[string]string)

	// 添加Labels
	for k, v := range m.cfg.Labels {
		metadata[k] = v
	}

	// 不再附带 agent 元数据
	metadata["agent_id"] = m.workerID()

	return metadata
}

// detectLocalIP 自动检测本机IP地址
func detectLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ip := ipnet.IP.String()
				// 优先返回私有网段IP（10.x, 172.16-31.x, 192.168.x）
				if strings.HasPrefix(ip, "10.") ||
					strings.HasPrefix(ip, "192.168.") ||
					(strings.HasPrefix(ip, "172.") && ip >= "172.16." && ip <= "172.31.") {
					return ip
				}
			}
		}
	}

	// 如果没有私有IP，返回第一个非回环IPv4
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return ""
}

func (m *ProxyRegistrationManager) getIP() string {
	// 1. 优先使用配置的PublicIP（如果不是占位符）
	if m.svcCtx.Config.Network.PublicIP != "" &&
		m.svcCtx.Config.Network.PublicIP != "8.8.8.8" &&
		m.svcCtx.Config.Network.PublicIP != "0.0.0.0" {
		return m.svcCtx.Config.Network.PublicIP
	}

	// 2. 尝试使用配置的LocalIP（如果不是占位符）
	if m.svcCtx.Config.Network.LocalIP != "" &&
		m.svcCtx.Config.Network.LocalIP != "127.0.0.1" &&
		m.svcCtx.Config.Network.LocalIP != "0.0.0.0" {
		return m.svcCtx.Config.Network.LocalIP
	}

	// 3. 自动检测本机IP
	detectedIP := detectLocalIP()
	if detectedIP != "" {
		m.logger.Infof("Auto-detected local IP: %s", detectedIP)
		return detectedIP
	}

	// 4. 兜底返回127.0.0.1
	m.logger.Infof("Failed to detect IP, using 127.0.0.1")
	return "127.0.0.1"
}

func (m *ProxyRegistrationManager) heartbeat() error {
	// 收集系统指标
	if err := m.metricsCollector.Collect(); err != nil {
		m.logger.Errorf("Failed to collect system metrics: %v", err)
	}

	systemMetrics := m.metricsCollector.GetMetrics()

	// 获取活跃会话数
	activeSessions := 0
	if m.svcCtx.SessionManager != nil {
		activeSessions = m.svcCtx.SessionManager.GetActiveSessionCount()
	}

	// Ops API 使用 proxy_id，身份仍优先取 WorkerID。
	payload := map[string]any{
		"worker_id":       m.workerID(),
		"proxy_id":        m.workerID(),
		"proxy_status":    "online",
		"status":          "online",
		"cpu_usage":       systemMetrics.CPUUsage,
		"memory_usage":    systemMetrics.MemoryUsage,
		"disk_usage":      systemMetrics.DiskUsage,
		"network_in":      systemMetrics.NetworkInDelta,
		"network_out":     systemMetrics.NetworkOutDelta,
		"active_sessions": activeSessions,
		"psk":             m.cfg.PSK,
	}

	return m.client.Heartbeat(payload)
}
