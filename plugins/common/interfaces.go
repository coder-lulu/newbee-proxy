package common

import (
	"context"
	"fmt"
	"io"
	"time"
)

// ProtocolPlugin 协议插件统一接口
type ProtocolPlugin interface {
	// 插件基本信息
	Name() string
	Version() string
	SupportedProtocols() []string
	Description() string

	// 生命周期管理
	Initialize(config map[string]interface{}) error
	Start() error
	Stop() error
	IsRunning() bool

	// 连接管理
	CreateConnection(ctx context.Context, target string, credentials *Credentials) (Connection, error)
	CloseConnection(connectionId string) error
	GetConnection(connectionId string) (Connection, bool)
	ListConnections() []string

	// 状态和监控
	GetStatus() *PluginStatus
	GetMetrics() *PluginMetrics

	// 配置热更新
	UpdateConfig(config map[string]interface{}) error
}

// Connection 连接接口
type Connection interface {
	// 连接信息
	ID() string
	Target() string
	Protocol() string
	Status() ConnectionStatus
	CreatedAt() time.Time
	LastActiveAt() time.Time

	// 数据传输
	Write(data []byte) (int, error)
	Read(data []byte) (int, error)

	// WebSocket桥接支持
	SetWebSocketWriter(writer io.Writer) error
	SetWebSocketReader(reader io.Reader) error

	// 连接控制
	Close() error
	IsConnected() bool

	// 会话管理
	GetSession() Session

	// 元数据
	GetMetadata() map[string]string
	SetMetadata(key, value string)
}

// Session 会话接口
type Session interface {
	// 会话信息
	ID() string
	ConnectionID() string
	UserID() string

	// 会话控制
	Start() error
	Stop() error

	// 数据流处理
	HandleData(data []byte) error
	SendData(data []byte) error

	// 会话状态
	IsActive() bool
	GetMetrics() *SessionMetrics
}

// Credentials 认证信息
type Credentials struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	KeyFile    string `json:"key_file"`
	AuthType   string `json:"auth_type"` // password, publickey, keyboard-interactive
	Timeout    int    `json:"timeout"`   // 连接超时(秒)
}

// ConnectionStatus 连接状态
type ConnectionStatus string

const (
	StatusConnecting   ConnectionStatus = "connecting"
	StatusConnected    ConnectionStatus = "connected"
	StatusDisconnected ConnectionStatus = "disconnected"
	StatusError        ConnectionStatus = "error"
	StatusTimeout      ConnectionStatus = "timeout"
)

// PluginStatus 插件状态
type PluginStatus struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Status      string                 `json:"status"` // running, stopped, error
	LoadedAt    time.Time              `json:"loaded_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	StoppedAt   *time.Time             `json:"stopped_at,omitempty"`
	Config      map[string]interface{} `json:"config"`
	ErrorMsg    string                 `json:"error_msg,omitempty"`
	Connections int                    `json:"connections"`
	Sessions    int                    `json:"sessions"`
}

// PluginMetrics 插件监控指标
type PluginMetrics struct {
	// 连接指标
	TotalConnections  int64         `json:"total_connections"`
	ActiveConnections int           `json:"active_connections"`
	FailedConnections int64         `json:"failed_connections"`
	ConnectionErrors  int64         `json:"connection_errors"`
	AvgConnectionTime time.Duration `json:"avg_connection_time"`

	// 会话指标
	TotalSessions      int64         `json:"total_sessions"`
	ActiveSessions     int           `json:"active_sessions"`
	AvgSessionDuration time.Duration `json:"avg_session_duration"`

	// 数据传输指标
	BytesSent        int64 `json:"bytes_sent"`
	BytesReceived    int64 `json:"bytes_received"`
	MessagesSent     int64 `json:"messages_sent"`
	MessagesReceived int64 `json:"messages_received"`

	// 性能指标
	CPUUsage       float64 `json:"cpu_usage"`
	MemoryUsage    int64   `json:"memory_usage"`
	GoroutineCount int     `json:"goroutine_count"`

	// 错误指标
	TotalErrors   int64 `json:"total_errors"`
	TimeoutErrors int64 `json:"timeout_errors"`
	AuthErrors    int64 `json:"auth_errors"`
	NetworkErrors int64 `json:"network_errors"`

	UpdatedAt time.Time `json:"updated_at"`
}

// SessionMetrics 会话监控指标
type SessionMetrics struct {
	SessionID     string        `json:"session_id"`
	ConnectionID  string        `json:"connection_id"`
	Duration      time.Duration `json:"duration"`
	BytesSent     int64         `json:"bytes_sent"`
	BytesReceived int64         `json:"bytes_received"`
	CommandsCount int           `json:"commands_count"`
	ErrorsCount   int           `json:"errors_count"`
	LastActivity  time.Time     `json:"last_activity"`
}

// ConnectionConfig 连接配置
type ConnectionConfig struct {
	Target            string            `json:"target"`
	Port              int               `json:"port"`
	Timeout           time.Duration     `json:"timeout"`
	KeepAlive         bool              `json:"keep_alive"`
	KeepAliveInterval time.Duration     `json:"keep_alive_interval"`
	MaxRetries        int               `json:"max_retries"`
	RetryInterval     time.Duration     `json:"retry_interval"`
	BufferSize        int               `json:"buffer_size"`
	Metadata          map[string]string `json:"metadata"`
}

// WebSocketBridge WebSocket桥接配置
type WebSocketBridge struct {
	Enabled     bool   `json:"enabled"`
	BufferSize  int    `json:"buffer_size"`
	Encoding    string `json:"encoding"` // utf8, base64
	Compression bool   `json:"compression"`
}

// PluginError 插件错误类型
type PluginError struct {
	Plugin    string                 `json:"plugin"`
	Code      string                 `json:"code"`
	Message   string                 `json:"message"`
	Detail    string                 `json:"detail"`
	Timestamp time.Time              `json:"timestamp"`
	Context   map[string]interface{} `json:"context"`
	Cause     error                  `json:"cause,omitempty"`
}

func (e *PluginError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("[%s:%s] %s: %s", e.Plugin, e.Code, e.Message, e.Detail)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Plugin, e.Code, e.Message)
}

func (e *PluginError) Unwrap() error {
	return e.Cause
}

func (e *PluginError) Is(target error) bool {
	if t, ok := target.(*PluginError); ok {
		return e.Code == t.Code && e.Plugin == t.Plugin
	}
	return false
}

// 标准错误码
const (
	// 连接错误
	ErrCodeConnectionFailed   = "CONNECTION_FAILED"
	ErrCodeConnectionTimeout  = "CONNECTION_TIMEOUT"
	ErrCodeConnectionClosed   = "CONNECTION_CLOSED"
	ErrCodeConnectionNotFound = "CONNECTION_NOT_FOUND"
	ErrCodeConnectionExists   = "CONNECTION_EXISTS"

	// 认证错误
	ErrCodeAuthFailed         = "AUTH_FAILED"
	ErrCodeAuthTimeout        = "AUTH_TIMEOUT"
	ErrCodeInvalidCredentials = "INVALID_CREDENTIALS"
	ErrCodePermissionDenied   = "PERMISSION_DENIED"

	// 插件错误
	ErrCodePluginNotFound       = "PLUGIN_NOT_FOUND"
	ErrCodePluginNotRunning     = "PLUGIN_NOT_RUNNING"
	ErrCodePluginAlreadyRunning = "PLUGIN_ALREADY_RUNNING"
	ErrCodePluginInitFailed     = "PLUGIN_INIT_FAILED"
	ErrCodePluginStartFailed    = "PLUGIN_START_FAILED"
	ErrCodePluginStopFailed     = "PLUGIN_STOP_FAILED"

	// 配置错误
	ErrCodeInvalidConfig      = "INVALID_CONFIG"
	ErrCodeMissingConfig      = "MISSING_CONFIG"
	ErrCodeConfigUpdateFailed = "CONFIG_UPDATE_FAILED"

	// 数据传输错误
	ErrCodeReadFailed     = "READ_FAILED"
	ErrCodeWriteFailed    = "WRITE_FAILED"
	ErrCodeDataCorrupted  = "DATA_CORRUPTED"
	ErrCodeBufferOverflow = "BUFFER_OVERFLOW"

	// 会话错误
	ErrCodeSessionNotFound = "SESSION_NOT_FOUND"
	ErrCodeSessionExpired  = "SESSION_EXPIRED"
	ErrCodeSessionClosed   = "SESSION_CLOSED"

	// 网络错误
	ErrCodeNetworkError        = "NETWORK_ERROR"
	ErrCodeDNSResolutionFailed = "DNS_RESOLUTION_FAILED"
	ErrCodePortNotAvailable    = "PORT_NOT_AVAILABLE"

	// 资源错误
	ErrCodeResourceExhausted = "RESOURCE_EXHAUSTED"
	ErrCodeMemoryError       = "MEMORY_ERROR"
	ErrCodeDiskError         = "DISK_ERROR"

	// WebSocket特定错误
	ErrCodeWSHandshakeFailed = "WS_HANDSHAKE_FAILED"
	ErrCodeWSProtocolError   = "WS_PROTOCOL_ERROR"
	ErrCodeWSMessageTooLarge = "WS_MESSAGE_TOO_LARGE"

	// SSH特定错误
	ErrCodeSSHHandshakeFailed   = "SSH_HANDSHAKE_FAILED"
	ErrCodeSSHChannelFailed     = "SSH_CHANNEL_FAILED"
	ErrCodeSSHKeyExchangeFailed = "SSH_KEY_EXCHANGE_FAILED"

	// RDP特定错误
	ErrCodeRDPHandshakeFailed     = "RDP_HANDSHAKE_FAILED"
	ErrCodeRDPScreenCaptureFailed = "RDP_SCREEN_CAPTURE_FAILED"
	ErrCodeRDPInputFailed         = "RDP_INPUT_FAILED"

	// Guacamole特定错误
	ErrCodeGuacProtocolError    = "GUAC_PROTOCOL_ERROR"
	ErrCodeGuacInstructionError = "GUAC_INSTRUCTION_ERROR"
	ErrCodeGuacConnectionFailed = "GUAC_CONNECTION_FAILED"

	// 兼容性错误码
	ErrCodeTimeout            = "TIMEOUT"
	ErrCodeUnsupportedFeature = "UNSUPPORTED_FEATURE"
)

// 错误处理工具函数

// NewPluginError 创建新的插件错误
func NewPluginError(plugin, code, message string) *PluginError {
	return &PluginError{
		Plugin:    plugin,
		Code:      code,
		Message:   message,
		Timestamp: time.Now(),
		Context:   make(map[string]interface{}),
	}
}

// NewPluginErrorWithDetail 创建包含详细信息的插件错误
func NewPluginErrorWithDetail(plugin, code, message, detail string) *PluginError {
	return &PluginError{
		Plugin:    plugin,
		Code:      code,
		Message:   message,
		Detail:    detail,
		Timestamp: time.Now(),
		Context:   make(map[string]interface{}),
	}
}

// NewPluginErrorWithCause 创建包含原因的插件错误
func NewPluginErrorWithCause(plugin, code, message string, cause error) *PluginError {
	return &PluginError{
		Plugin:    plugin,
		Code:      code,
		Message:   message,
		Timestamp: time.Now(),
		Context:   make(map[string]interface{}),
		Cause:     cause,
	}
}

// WithContext 为错误添加上下文信息
func (e *PluginError) WithContext(key string, value interface{}) *PluginError {
	e.Context[key] = value
	return e
}

// WithDetail 为错误添加详细信息
func (e *PluginError) WithDetail(detail string) *PluginError {
	e.Detail = detail
	return e
}

// IsConnectionError 判断是否为连接相关错误
func IsConnectionError(err error) bool {
	if pe, ok := err.(*PluginError); ok {
		switch pe.Code {
		case ErrCodeConnectionFailed, ErrCodeConnectionTimeout,
			ErrCodeConnectionClosed, ErrCodeConnectionNotFound,
			ErrCodeConnectionExists:
			return true
		}
	}
	return false
}

// IsAuthError 判断是否为认证相关错误
func IsAuthError(err error) bool {
	if pe, ok := err.(*PluginError); ok {
		switch pe.Code {
		case ErrCodeAuthFailed, ErrCodeAuthTimeout,
			ErrCodeInvalidCredentials, ErrCodePermissionDenied:
			return true
		}
	}
	return false
}

// IsTimeoutError 判断是否为超时错误
func IsTimeoutError(err error) bool {
	if pe, ok := err.(*PluginError); ok {
		switch pe.Code {
		case ErrCodeConnectionTimeout, ErrCodeAuthTimeout, ErrCodeTimeout:
			return true
		}
	}
	return false
}
