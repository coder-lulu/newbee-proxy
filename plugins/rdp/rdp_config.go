package rdp

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// GuacdConfig Guacamole守护进程配置
type GuacdConfig struct {
	Address             string        `yaml:"address" json:"address"`                             // 主要guacd服务地址
	FallbackAddresses   []string      `yaml:"fallback_addresses" json:"fallback_addresses"`       // 备用guacd服务地址列表
	ConnectTimeout      time.Duration `yaml:"connect_timeout" json:"connect_timeout"`             // 连接超时时间
	HealthCheckInterval time.Duration `yaml:"health_check_interval" json:"health_check_interval"` // 健康检查间隔
	MaxConnections      int           `yaml:"max_connections" json:"max_connections"`             // 最大连接数
}

// ConnectionConfig RDP连接配置
type ConnectionConfig struct {
	DefaultWidth  int `yaml:"default_width" json:"default_width"`   // 默认屏幕宽度
	DefaultHeight int `yaml:"default_height" json:"default_height"` // 默认屏幕高度
	ColorDepth    int `yaml:"color_depth" json:"color_depth"`       // 颜色深度
	DPI           int `yaml:"dpi" json:"dpi"`                       // DPI设置
}

// RDPPluginConfig RDP插件完整配置
type RDPPluginConfig struct {
	MaxConnections    int              `yaml:"max_connections" json:"max_connections"`       // 最大连接数
	ConnectionTimeout time.Duration    `yaml:"connection_timeout" json:"connection_timeout"` // 连接超时
	Guacd             GuacdConfig      `yaml:"guacd" json:"guacd"`                           // Guacamole配置
	Connection        ConnectionConfig `yaml:"connection" json:"connection"`                 // 连接配置
}

// DefaultRDPConfig 默认RDP配置
func DefaultRDPConfig() *RDPPluginConfig {
	return &RDPPluginConfig{
		MaxConnections:    20,
		ConnectionTimeout: 60 * time.Second,
		Guacd: GuacdConfig{
			Address:             "localhost:4822",
			FallbackAddresses:   []string{"127.0.0.1:4822"},
			ConnectTimeout:      10 * time.Second,
			HealthCheckInterval: 30 * time.Second,
			MaxConnections:      10,
		},
		Connection: ConnectionConfig{
			DefaultWidth:  1920,
			DefaultHeight: 1080,
			ColorDepth:    32,
			DPI:           96,
		},
	}
}

// GuacdManager Guacamole守护进程管理器 - 简化版本，不使用连接池
type GuacdManager struct {
	config *GuacdConfig
	logger logx.Logger

	// 健康检查
	healthTicker *time.Ticker
	stopCh       chan struct{}
	mutex        sync.RWMutex
	healthy      bool
}

// NewGuacdManager 创建新的Guacd管理器
func NewGuacdManager(config *GuacdConfig) *GuacdManager {
	manager := &GuacdManager{
		config:  config,
		logger:  logx.WithContext(nil),
		stopCh:  make(chan struct{}),
		healthy: true,
	}

	// 启动健康检查
	manager.startHealthCheck()

	return manager
}

// GetConnection 获取到guacd的连接 - 每次创建新连接
func (gm *GuacdManager) GetConnection() (net.Conn, error) {
	gm.mutex.RLock()
	defer gm.mutex.RUnlock()

	if !gm.healthy {
		return nil, fmt.Errorf("guacd服务不健康")
	}

	// 直接创建新连接，不使用连接池
	conn, err := gm.createConnection()
	if err != nil {
		gm.logger.Errorf("创建guacd连接失败: %v", err)
		return nil, err
	}

	gm.logger.Infof("成功创建新的guacd连接: %s", gm.config.Address)
	return conn, nil
}

// createConnection 创建新的连接
func (gm *GuacdManager) createConnection() (net.Conn, error) {
	// 尝试主地址
	conn, err := gm.dialWithTimeout(gm.config.Address)
	if err == nil {
		return conn, nil
	}

	gm.logger.Errorf("主地址连接失败 %s: %v", gm.config.Address, err)

	// 尝试备用地址
	for _, addr := range gm.config.FallbackAddresses {
		conn, err := gm.dialWithTimeout(addr)
		if err == nil {
			gm.logger.Infof("使用备用地址连接成功: %s", addr)
			return conn, nil
		}
		gm.logger.Errorf("备用地址连接失败 %s: %v", addr, err)
	}

	return nil, fmt.Errorf("所有guacd地址都无法连接")
}

// dialWithTimeout 带超时的连接
func (gm *GuacdManager) dialWithTimeout(address string) (net.Conn, error) {
	return net.DialTimeout("tcp", address, gm.config.ConnectTimeout)
}

// startHealthCheck 启动健康检查
func (gm *GuacdManager) startHealthCheck() {
	if gm.config.HealthCheckInterval <= 0 {
		gm.logger.Info("健康检查已禁用")
		return
	}

	gm.healthTicker = time.NewTicker(gm.config.HealthCheckInterval)

	go func() {
		defer gm.healthTicker.Stop()

		for {
			select {
			case <-gm.healthTicker.C:
				gm.performHealthCheck()
			case <-gm.stopCh:
				return
			}
		}
	}()

	gm.logger.Infof("健康检查已启动，间隔: %v", gm.config.HealthCheckInterval)
}

// performHealthCheck 执行健康检查
func (gm *GuacdManager) performHealthCheck() {
	conn, err := gm.dialWithTimeout(gm.config.Address)
	if err != nil {
		gm.mutex.Lock()
		gm.healthy = false
		gm.mutex.Unlock()
		gm.logger.Errorf("健康检查失败: %v", err)
		return
	}

	conn.Close()

	gm.mutex.Lock()
	gm.healthy = true
	gm.mutex.Unlock()
	gm.logger.Infof("健康检查通过: %s", gm.config.Address)
}

// Close 关闭管理器
func (gm *GuacdManager) Close() error {
	gm.logger.Info("正在关闭Guacd管理器...")

	// 停止健康检查
	close(gm.stopCh)

	if gm.healthTicker != nil {
		gm.healthTicker.Stop()
	}

	gm.logger.Info("Guacd管理器已关闭")
	return nil
}

// IsHealthy 检查服务是否健康
func (gm *GuacdManager) IsHealthy() bool {
	gm.mutex.RLock()
	defer gm.mutex.RUnlock()
	return gm.healthy
}

// GetConfig 获取配置
func (gm *GuacdManager) GetConfig() *GuacdConfig {
	return gm.config
}

// GetStats 获取统计信息
func (gm *GuacdManager) GetStats() map[string]interface{} {
	gm.mutex.RLock()
	defer gm.mutex.RUnlock()

	return map[string]interface{}{
		"address":         gm.config.Address,
		"healthy":         gm.healthy,
		"connect_timeout": gm.config.ConnectTimeout.String(),
		"health_interval": gm.config.HealthCheckInterval.String(),
	}
}
