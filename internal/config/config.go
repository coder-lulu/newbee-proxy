package config

import (
	"github.com/zeromicro/go-zero/rest"
)

type Config struct {
	rest.RestConf

	// Agent基础配置
	Agent AgentConf `json:",optional"`

	// OPS服务连接
	OpsRpc OpsRpcConf `json:",optional"`

	// 资源限制
	Limits ResourceLimits `json:",optional"`

	// 插件配置
	Plugins PluginConfigs `json:",optional"`

	// 心跳配置
	Heartbeat HeartbeatConf `json:",optional"`

	// 网络配置
	Network NetworkConf `json:",optional"`

	// 安全配置
	Security SecurityConf `json:",optional"`
}

// Agent基础配置
type AgentConf struct {
	ID           string   `json:",optional"`
	Region       string   `json:",optional"`
	Version      string   `json:",optional"`
	Capabilities []string `json:",optional"`
	AuthToken    string   `json:",optional"`
}

// OPS RPC配置
type OpsRpcConf struct {
	Endpoints     []string `json:",optional"`
	Enabled       bool     `json:",optional"`
	Timeout       int64    `json:",optional"`
	RetryInterval int      `json:",optional"`
	MaxRetries    int      `json:",optional"`
}

// 资源限制配置
type ResourceLimits struct {
	MaxConcurrentSessions int `json:",optional"`
	MaxMemoryMB           int `json:",optional"`
	MaxCPUPercent         int `json:",optional"`
	SessionTimeoutSeconds int `json:",optional"`
}

// 插件配置
type PluginConfigs struct {
	SSH    SSHPluginConf    `json:",optional"`
	Telnet TelnetPluginConf `json:",optional"`
	RDP    RDPPluginConf    `json:",optional"`
	IPMI   IPMIPluginConf   `json:",optional"`
	SNMP   SNMPPluginConf   `json:",optional"`
}

// SSH插件配置
type SSHPluginConf struct {
	MaxConnections    int `json:",optional"`
	KeepAliveInterval int `json:",optional"`
	ConnectionTimeout int `json:",optional"`
}

// Telnet插件配置
type TelnetPluginConf struct {
	MaxConnections    int `json:",optional"`
	ConnectionTimeout int `json:",optional"`
}

// RDP插件配置
type RDPPluginConf struct {
	MaxConnections    int                `json:",optional"`
	ConnectionTimeout int                `json:",optional"`
	Guacd             *GuacdConf         `json:",optional"`
	Connection        *RDPConnectionConf `json:",optional"`
}

// Guacamole守护进程配置
type GuacdConf struct {
	Address             string   `json:",optional"`
	Fallbacks           []string `json:",optional"`
	ConnectTimeout      string   `json:",optional"`
	HealthCheckInterval string   `json:",optional"`
}

// RDP连接配置
type RDPConnectionConf struct {
	DefaultWidth  int `json:",optional"`
	DefaultHeight int `json:",optional"`
	ColorDepth    int `json:",optional"`
	DPI           int `json:",optional"`
}

// IPMI插件配置
type IPMIPluginConf struct {
	ConnectionTimeout int `json:",optional"`
	CommandTimeout    int `json:",optional"`
}

// SNMP插件配置
type SNMPPluginConf struct {
	Version   string `json:",optional"`
	Community string `json:",optional"`
	Timeout   int    `json:",optional"`
}

// 心跳配置
type HeartbeatConf struct {
	Interval   int `json:",optional"` // 心跳间隔(秒)
	Timeout    int `json:",optional"` // 心跳超时(秒)
	RetryCount int `json:",optional"` // 重试次数
}

// 网络配置
type NetworkConf struct {
	LocalIP         string   `json:",optional"`
	PublicIP        string   `json:",optional"`
	NetworkSegments []string `json:",optional"`
}

// 安全配置
type SecurityConf struct {
	EnableTLS  bool   `json:",optional"`
	CertFile   string `json:",optional"`
	KeyFile    string `json:",optional"`
	CAFile     string `json:",optional"`
	SkipVerify bool   `json:",optional"`
}
