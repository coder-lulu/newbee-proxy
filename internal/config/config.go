package config

import (
	"github.com/zeromicro/go-zero/rest"
)

type Config struct {
    rest.RestConf

    // gRPC OPS 已弃用：保留字段仅为兼容旧配置，后续可移除
    // OpsRpc OpsRpcConf `json:",optional"`

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

    // Ops Center（中心服务）
    OpsCenter OpsCenterConf `json:",optional"`

    // gRPC 服务配置（控制面）
    Grpc GrpcConf `json:",optional"`

    // 本地存储（可选）
    Storage StorageConf `json:",optional"`
}

// 已移除 Agent 基础配置（不再需要任何 agent 元数据）

// OPS RPC配置
// type OpsRpcConf struct {
//     Endpoints     []string `json:",optional"`
//     Enabled       bool     `json:",optional"`
//     Timeout       int64    `json:",optional"`
//     RetryInterval int      `json:",optional"`
//     MaxRetries    int      `json:",optional"`
// }

// 资源限制配置
type ResourceLimits struct {
    MaxConcurrentSessions int `json:",optional"`
    MaxMemoryMB           int `json:",optional"`
    MaxCPUPercent         int `json:",optional"`
    SessionTimeoutSeconds int `json:",optional"`
    // 任务队列与并发（可选配置，默认队列1000、并发50）
    TaskQueueSize        int `json:",optional"`
    MaxConcurrentTasks   int `json:",optional"`
}

// 插件配置
type PluginConfigs struct {
    SSH    SSHPluginConf    `json:",optional"`
    Telnet TelnetPluginConf `json:",optional"`
    RDP    RDPPluginConf    `json:",optional"`
    IPMI   IPMIPluginConf   `json:",optional"`
    SNMP   SNMPPluginConf   `json:",optional"`
    HTTP   HTTPPluginConf   `json:",optional"`
    DB     DBPluginConf     `json:",optional"`
    PortForward PortForwardConf `json:",optional"`
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

// HTTP 插件配置
type HTTPPluginConf struct {
    // 连接池/超时
    MaxIdleConns        int    `json:",optional"`
    MaxIdleConnsPerHost int    `json:",optional"`
    IdleConnTimeout     string `json:",optional"` // e.g. 90s
    DefaultTimeout      string `json:",optional"` // e.g. 30s

    // 重试
    Retry struct {
        Max                 int    `json:",optional"`
        BaseDelay           string `json:",optional"`
        MaxDelay            string `json:",optional"`
        RetryOn5xx          bool   `json:",optional"`
        RetryOnNetworkError bool   `json:",optional"`
        RetryOnCodes        []int  `json:",optional"`
    } `json:",optional"`

    // 域名名单
    AllowedHosts []string `json:",optional"`
    BlockedHosts []string `json:",optional"`

    // 并发/速率（当前仅并发）
    Parallelism struct {
        Global  int `json:",optional"`
        PerHost int `json:",optional"`
    } `json:",optional"`

    // 代理
    Proxy struct {
        HTTPProxy  string `json:",optional"`
        HTTPSProxy string `json:",optional"`
        NoProxy    string `json:",optional"`
    } `json:",optional"`

    // TLS
    TLS struct {
        InsecureSkipVerify bool     `json:",optional"`
        PinnedCerts        []string `json:",optional"` // 证书指纹(SHA256十六进制)
    } `json:",optional"`

    // 断路器
    CircuitBreaker struct {
        FailureLimit int    `json:",optional"`
        Timeout      string `json:",optional"`
    } `json:",optional"`
}

// DB 插件配置（简化）
type DBPluginConf struct {
    MaxConnections int `json:",optional"`
    Timeout        int `json:",optional"` // 秒
}

// PortForward 插件默认配置（规则仍以 API 传入为准）
type PortForwardConf struct {
    Default PortFwdDefaultConf `json:",optional"`
}

type PortFwdDefaultConf struct {
    DialTimeout   string `json:",optional"`
    IdleTimeout   string `json:",optional"`
    KeepAliveSec  int    `json:",optional"`
    NoDelay       bool   `json:",optional"`
    MaxConns      int    `json:",optional"`
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

    // Web/API安全增强（可选，保持兼容）
    JWT              *JWTConf        `json:",optional"`
    OriginWhitelist  []string        `json:",optional"` // 允许的Origin白名单，留空表示不校验
    RateLimit        *RateLimitConf  `json:",optional"` // 速率限制（握手/HTTP层）
    // 认证跳过路径（默认全保护；此列表内路径跳过认证）
    SkipPaths        []string        `json:",optional"`

    // SSH 主机校验与凭据相关安全配置
    SSH *SSHConf `json:",optional"`
}

// SSHConf SSH 安全配置
type SSHConf struct {
    KnownHostsPath        string   `json:",optional"` // known_hosts 文件路径，留空时默认 ~/.ssh/known_hosts
    StrictHostKeyChecking bool     `json:",optional"` // 严格校验未知主机（true=拒绝未知）
    FirstUseTrust         bool     `json:",optional"` // 首次信任：未知主机首次连接时写入 known_hosts（Strict=false 时才生效）
    AllowedFingerprints   []string `json:",optional"` // 指纹白名单（SHA256），命中则直接放行
}

// JWTConf Web/WS 鉴权配置
type JWTConf struct {
    Enabled         bool   `json:",optional"` // 是否启用JWT校验
    Secret          string `json:",optional"` // HS256密钥（演示用；生产建议JWKS）
    AllowQueryToken bool   `json:",optional"` // 允许通过 query ?token= 传递（便于兼容）
    Enforce         bool   `json:",optional"` // 强制模式：无效即拒绝；false=观察模式：仅记录
}

// RateLimitConf 简易令牌桶配置
type RateLimitConf struct {
    Enabled            bool `json:",optional"`
    RequestsPerSecond  int  `json:",optional"` // 默认建议50
    Burst              int  `json:",optional"` // 预留（当前未使用），保留与RPS一致
}

// OpsCenterConf 中心服务配置
type OpsCenterConf struct {
    Enabled          bool              `json:",optional"`
    Endpoints        []string          `json:",optional"` // http://host:port （首选第一个）
    HeartbeatSeconds int               `json:",optional"` // 心跳间隔，默认30s
    ProxyID          string            `json:",optional"` // 覆盖Agent.ID作为上报ID（已弃用，使用WorkerID）
    WorkerID         string            `json:",optional"` // Worker唯一标识，优先使用此字段
    Region           string            `json:",optional"`
    Zone             string            `json:",optional"` // 可用区（优先使用，如为空则使用AZ）
    AZ               string            `json:",optional"` // 可用区（向后兼容）
    PSK              string            `json:",optional"` // 预共享密钥，用于注册/心跳鉴权
    Labels           map[string]string `json:",optional"`
    HealthCheckURL   string            `json:",optional"` // 健康检查URL，如：/health
    Tags             []string          `json:",optional"` // Worker标签列表
}

// GrpcConf gRPC 服务配置
type GrpcConf struct {
    // 监听地址，如 ":9700" 或 "0.0.0.0:9700"
    ListenOn string `json:",optional"`
    // 是否启用 gRPC 服务
    Enabled  bool   `json:",optional"`
}

// StorageConf 本地存储（SQLite）配置
type StorageConf struct {
    EnableSQLite  bool   `json:",optional"` // 是否启用本地 SQLite 存储（默认 true）
    DBPath        string `json:",optional"` // 数据库文件路径（默认 data/proxy.db）
    MaxSizeMB     int    `json:",optional"` // 最大大小（MB），用于清理策略，0 表示不限
    RetentionDays int    `json:",optional"` // 保留天数，0 表示不限
}
