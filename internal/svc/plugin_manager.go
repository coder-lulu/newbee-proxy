package svc

import (
	"fmt"
	"sync"

    "github.com/coder-lulu/newbee-proxy/internal/config"
    "github.com/coder-lulu/newbee-proxy/plugins/common"
    "github.com/coder-lulu/newbee-proxy/plugins/db"
    "github.com/coder-lulu/newbee-proxy/plugins/rdp"
    "github.com/coder-lulu/newbee-proxy/plugins/ssh"
    httpplugin "github.com/coder-lulu/newbee-proxy/plugins/http"
    portfwd "github.com/coder-lulu/newbee-proxy/plugins/portfwd"
    snmpplugin "github.com/coder-lulu/newbee-proxy/plugins/snmp"

	"github.com/zeromicro/go-zero/core/logx"
)

// PluginManager 插件管理器
type PluginManager struct {
	plugins map[string]common.ProtocolPlugin
	config  config.PluginConfigs
	logger  logx.Logger
	mutex   sync.RWMutex
	started bool
}

// NewPluginManager 创建新的插件管理器
func NewPluginManager(config config.PluginConfigs, logger logx.Logger) *PluginManager {
	return &PluginManager{
		plugins: make(map[string]common.ProtocolPlugin),
		config:  config,
		logger:  logger,
	}
}

// Start 启动插件管理器
func (pm *PluginManager) Start() error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if pm.started {
		return nil
	}

	pm.logger.Info("Starting plugin manager...")

	// 加载内置插件
	if err := pm.loadBuiltinPlugins(); err != nil {
		return fmt.Errorf("failed to load builtin plugins: %w", err)
	}

	pm.started = true
	pm.logger.Info("Plugin manager started")
	return nil
}

// Stop 停止插件管理器
func (pm *PluginManager) Stop() error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if !pm.started {
		return nil
	}

	pm.logger.Info("Stopping plugin manager...")

	// 停止所有插件
	for name, plugin := range pm.plugins {
		if plugin.IsRunning() {
			if err := plugin.Stop(); err != nil {
				pm.logger.Errorf("Error stopping plugin %s: %v", name, err)
			}
		}
	}

	// 清空插件映射
	pm.plugins = make(map[string]common.ProtocolPlugin)

	pm.started = false
	pm.logger.Info("Plugin manager stopped")
	return nil
}

// LoadPlugin 手动加载插件
func (pm *PluginManager) LoadPlugin(name string, plugin common.ProtocolPlugin) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin %s already loaded", name)
	}

	pm.plugins[name] = plugin
	pm.logger.Infof("Plugin %s loaded successfully", name)
	return nil
}

// UnloadPlugin 卸载插件
func (pm *PluginManager) UnloadPlugin(name string) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	return pm.unloadPlugin(name)
}

// unloadPlugin 内部卸载插件方法（无锁）
func (pm *PluginManager) unloadPlugin(name string) error {
	plugin, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	// 停止插件
	if plugin.IsRunning() {
		if err := plugin.Stop(); err != nil {
			pm.logger.Errorf("Error stopping plugin %s: %v", name, err)
		}
	}

	delete(pm.plugins, name)
	pm.logger.Infof("Plugin %s unloaded", name)
	return nil
}

// GetLoadedPlugins 获取已加载的插件列表
func (pm *PluginManager) GetLoadedPlugins() []string {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	plugins := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		plugins = append(plugins, name)
	}
	return plugins
}

// IsPluginLoaded 检查插件是否已加载
func (pm *PluginManager) IsPluginLoaded(name string) bool {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	_, exists := pm.plugins[name]
	return exists
}

// GetPlugin 获取插件实例
func (pm *PluginManager) GetPlugin(name string) (common.ProtocolPlugin, bool) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	plugin, exists := pm.plugins[name]
	return plugin, exists
}

// GetPluginStatus 获取插件状态
func (pm *PluginManager) GetPluginStatus(name string) (*common.PluginStatus, error) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found", name)
	}

	return plugin.GetStatus(), nil
}

// GetAllPluginStatus 获取所有插件状态
func (pm *PluginManager) GetAllPluginStatus() map[string]*common.PluginStatus {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	status := make(map[string]*common.PluginStatus)
	for name, plugin := range pm.plugins {
		status[name] = plugin.GetStatus()
	}

	return status
}

// GetPluginMetrics 获取插件指标
func (pm *PluginManager) GetPluginMetrics(name string) (*common.PluginMetrics, error) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found", name)
	}

	return plugin.GetMetrics(), nil
}

// StartPlugin 启动插件
func (pm *PluginManager) StartPlugin(name string) error {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	if plugin.IsRunning() {
		return fmt.Errorf("plugin %s is already running", name)
	}

	return plugin.Start()
}

// StopPlugin 停止插件
func (pm *PluginManager) StopPlugin(name string) error {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	if !plugin.IsRunning() {
		return fmt.Errorf("plugin %s is not running", name)
	}

	return plugin.Stop()
}

// loadBuiltinPlugins 加载内置插件
func (pm *PluginManager) loadBuiltinPlugins() error {
	pm.logger.Info("Loading builtin plugins...")

	// 加载SSH插件
	if err := pm.loadSSHPlugin(); err != nil {
		return fmt.Errorf("failed to load SSH plugin: %w", err)
	}

	// 加载RDP插件
	if err := pm.loadRDPPlugin(); err != nil {
		return fmt.Errorf("failed to load RDP plugin: %w", err)
	}

    // 加载DB插件
    if err := pm.loadDbPlugin(); err != nil {
        return fmt.Errorf("failed to load DB plugin: %w", err)
    }

    // 加载HTTP插件
    if err := pm.loadHTTPPlugin(); err != nil {
        return fmt.Errorf("failed to load HTTP plugin: %w", err)
    }

    // 加载端口转发插件
    if err := pm.loadPortFwdPlugin(); err != nil {
        return fmt.Errorf("failed to load PortFwd plugin: %w", err)
    }

    // 加载 SNMP 插件（默认启用）
    if err := pm.loadSNMPPlugin(); err != nil {
        return fmt.Errorf("failed to load SNMP plugin: %w", err)
    }

	pm.logger.Info("Builtin plugins loaded successfully")
	return nil
}

// loadSSHPlugin 加载SSH插件
func (pm *PluginManager) loadSSHPlugin() error {
	pm.logger.Info("Loading SSH plugin...")

	// 创建SSH插件实例
	sshPlugin := ssh.NewSSHPlugin()

	// 准备SSH插件配置
	sshConfig := make(map[string]interface{})
	sshConfig["max_connections"] = pm.config.SSH.MaxConnections
	sshConfig["keep_alive_interval"] = pm.config.SSH.KeepAliveInterval
	sshConfig["connection_timeout"] = pm.config.SSH.ConnectionTimeout

	// 初始化插件
	if err := sshPlugin.Initialize(sshConfig); err != nil {
		return fmt.Errorf("failed to initialize SSH plugin: %w", err)
	}

	// 启动插件
	if err := sshPlugin.Start(); err != nil {
		return fmt.Errorf("failed to start SSH plugin: %w", err)
	}

	// 注册插件
	pm.plugins[sshPlugin.Name()] = sshPlugin

	pm.logger.Infof("SSH plugin loaded and started: %s v%s", sshPlugin.Name(), sshPlugin.Version())
	return nil
}

// loadRDPPlugin 加载RDP插件
func (pm *PluginManager) loadRDPPlugin() error {
	pm.logger.Info("Loading RDP plugin...")

	// 创建RDP插件实例
	rdpPlugin := rdp.NewRDPPlugin()

	// 准备RDP插件配置 - 从配置文件读取真实配置
	rdpConfig := make(map[string]interface{})

	// 检查配置文件中是否有RDP配置
	if pm.config.RDP.MaxConnections > 0 { // 简单检查是否有RDP配置
		pm.logger.Info("Found RDP configuration in config file, loading...")

		// 构建完整的RDP配置结构
		rdpFullConfig := map[string]interface{}{
			"MaxConnections":    pm.config.RDP.MaxConnections,
			"ConnectionTimeout": pm.config.RDP.ConnectionTimeout,
		}

		// 添加Guacd配置
		if pm.config.RDP.Guacd != nil {
			guacdConfig := map[string]interface{}{
				"Address":             pm.config.RDP.Guacd.Address,
				"Fallbacks":           pm.config.RDP.Guacd.Fallbacks,
				"ConnectTimeout":      pm.config.RDP.Guacd.ConnectTimeout,
				"HealthCheckInterval": pm.config.RDP.Guacd.HealthCheckInterval,
			}
			rdpFullConfig["Guacd"] = guacdConfig
			pm.logger.Infof("Configured Guacd address: %s", pm.config.RDP.Guacd.Address)
		} else {
			pm.logger.Infof("No Guacd configuration found, using defaults")
		}

		// 添加Connection配置
		if pm.config.RDP.Connection != nil {
			connectionConfig := map[string]interface{}{
				"DefaultWidth":  pm.config.RDP.Connection.DefaultWidth,
				"DefaultHeight": pm.config.RDP.Connection.DefaultHeight,
				"ColorDepth":    pm.config.RDP.Connection.ColorDepth,
				"DPI":           pm.config.RDP.Connection.DPI,
			}
			rdpFullConfig["Connection"] = connectionConfig
		} else {
			pm.logger.Infof("No RDP Connection configuration found, using defaults")
		}

		// 直接使用完整配置，不要嵌套
		rdpConfig = rdpFullConfig

		pm.logger.Infof("RDP plugin configuration loaded: MaxConnections=%d, Timeout=%s",
			pm.config.RDP.MaxConnections, pm.config.RDP.ConnectionTimeout)
	} else {
		pm.logger.Error("No RDP configuration found in config file - RDP plugin cannot function without proper configuration")
		return fmt.Errorf("RDP configuration missing in config file")
	}

	// 初始化插件
	if err := rdpPlugin.Initialize(rdpConfig); err != nil {
		return fmt.Errorf("failed to initialize RDP plugin: %w", err)
	}

	// 启动插件
	if err := rdpPlugin.Start(); err != nil {
		return fmt.Errorf("failed to start RDP plugin: %w", err)
	}

	// 注册插件
	pm.plugins[rdpPlugin.Name()] = rdpPlugin

	pm.logger.Infof("RDP plugin loaded and started: %s v%s", rdpPlugin.Name(), rdpPlugin.Version())
	return nil
}

// loadDbPlugin 加载DB插件
func (pm *PluginManager) loadDbPlugin() error {
	pm.logger.Info("Loading DB plugin...")

	// 创建DB插件实例
	dbPlugin := db.NewDbPlugin()

	// 准备DB插件配置
	dbConfig := make(map[string]interface{})
	dbConfig["max_connections"] = 50 // 默认值
	dbConfig["timeout"] = 30         // 默认值

	// 初始化插件
	if err := dbPlugin.Initialize(dbConfig); err != nil {
		return fmt.Errorf("failed to initialize DB plugin: %w", err)
	}

	// 启动插件
	if err := dbPlugin.Start(); err != nil {
		return fmt.Errorf("failed to start DB plugin: %w", err)
	}

	// 注册插件
	pm.plugins[dbPlugin.Name()] = dbPlugin

	pm.logger.Infof("DB plugin loaded and started: %s v%s", dbPlugin.Name(), dbPlugin.Version())
	return nil
}

// loadHTTPPlugin 加载HTTP插件
func (pm *PluginManager) loadHTTPPlugin() error {
    pm.logger.Info("Loading HTTP plugin...")

    plug := httpplugin.New()
    cfg := map[string]interface{}{
        "max_idle_conns":         pm.config.HTTP.MaxIdleConns,
        "max_idle_conns_per_host": pm.config.HTTP.MaxIdleConnsPerHost,
        "idle_conn_timeout":      pm.config.HTTP.IdleConnTimeout,
        "default_timeout":        pm.config.HTTP.DefaultTimeout,
        "allowed_hosts":          pm.config.HTTP.AllowedHosts,
        "blocked_hosts":          pm.config.HTTP.BlockedHosts,
        "proxy": map[string]any{
            "http_proxy":  pm.config.HTTP.Proxy.HTTPProxy,
            "https_proxy": pm.config.HTTP.Proxy.HTTPSProxy,
            "no_proxy":    pm.config.HTTP.Proxy.NoProxy,
        },
        "tls": map[string]any{
            "insecure_skip_verify": pm.config.HTTP.TLS.InsecureSkipVerify,
            "pinned_certs":         pm.config.HTTP.TLS.PinnedCerts,
        },
        "circuit_breaker": map[string]any{
            "failure_limit": pm.config.HTTP.CircuitBreaker.FailureLimit,
            "timeout":       pm.config.HTTP.CircuitBreaker.Timeout,
        },
        "parallelism": map[string]any{ "global": pm.config.HTTP.Parallelism.Global, "per_host": pm.config.HTTP.Parallelism.PerHost },
        "retry": map[string]any{
            "max":                    pm.config.HTTP.Retry.Max,
            "base_delay":             pm.config.HTTP.Retry.BaseDelay,
            "max_delay":              pm.config.HTTP.Retry.MaxDelay,
            "retry_on_5xx":           pm.config.HTTP.Retry.RetryOn5xx,
            "retry_on_network_error": pm.config.HTTP.Retry.RetryOnNetworkError,
            "retry_on_codes":         pm.config.HTTP.Retry.RetryOnCodes,
        },
    }
    if err := plug.Initialize(cfg); err != nil {
        return fmt.Errorf("failed to initialize HTTP plugin: %w", err)
    }
    if err := plug.Start(); err != nil {
        return fmt.Errorf("failed to start HTTP plugin: %w", err)
    }
    pm.plugins[plug.Name()] = plug
    pm.logger.Infof("HTTP plugin loaded and started: %s v%s", plug.Name(), plug.Version())
    return nil
}

func (pm *PluginManager) loadPortFwdPlugin() error {
    pm.logger.Info("Loading PortForward plugin...")
    plug := portfwd.New()
    if err := plug.Initialize(map[string]any{}); err != nil { return err }
    if err := plug.Start(); err != nil { return err }
    pm.plugins[plug.Name()] = plug
    pm.logger.Infof("PortForward plugin loaded and started: %s v%s", plug.Name(), plug.Version())
    return nil
}

func (pm *PluginManager) loadSNMPPlugin() error {
    pm.logger.Info("Loading SNMP plugin...")
    plug := snmpplugin.New()
    if err := plug.Initialize(map[string]any{}); err != nil { return err }
    if err := plug.Start(); err != nil { return err }
    pm.plugins[plug.Name()] = plug
    pm.logger.Infof("SNMP plugin loaded and started: %s v%s", plug.Name(), plug.Version())
    return nil
}

// UpdatePluginConfig 更新插件配置
func (pm *PluginManager) UpdatePluginConfig(name string, config map[string]interface{}) error {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	return plugin.UpdateConfig(config)
}

// GetSupportedProtocols 获取所有支持的协议
func (pm *PluginManager) GetSupportedProtocols() []string {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	protocolsMap := make(map[string]bool)
	for _, plugin := range pm.plugins {
		for _, protocol := range plugin.SupportedProtocols() {
			protocolsMap[protocol] = true
		}
	}

	protocols := make([]string, 0, len(protocolsMap))
	for protocol := range protocolsMap {
		protocols = append(protocols, protocol)
	}

	return protocols
}

// GetPluginForProtocol 根据协议获取插件
func (pm *PluginManager) GetPluginForProtocol(protocol string) (common.ProtocolPlugin, bool) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	for _, plugin := range pm.plugins {
		for _, supportedProtocol := range plugin.SupportedProtocols() {
			if supportedProtocol == protocol {
				// 检查插件是否运行，如果没有运行则尝试重启
				if !plugin.IsRunning() {
					pm.logger.Errorf("Plugin %s for protocol %s is not running, attempting to restart...", plugin.Name(), protocol)
					if err := plugin.Start(); err != nil {
						pm.logger.Errorf("Failed to restart plugin %s: %v", plugin.Name(), err)
						return nil, false
					}
					pm.logger.Infof("Plugin %s restarted successfully", plugin.Name())
				}
				return plugin, true
			}
		}
	}

	return nil, false
}
