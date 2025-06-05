package timeout

import (
	"context"
	"fmt"
	"time"
)

// TimeoutConfig 超时配置
type TimeoutConfig struct {
	// 基础超时设置
	DefaultTimeout time.Duration `json:"default_timeout"` // 默认超时时间
	MinTimeout     time.Duration `json:"min_timeout"`     // 最小超时时间
	MaxTimeout     time.Duration `json:"max_timeout"`     // 最大超时时间

	// 操作类型特定超时
	ConnectTimeout time.Duration `json:"connect_timeout"` // 连接超时
	ReadTimeout    time.Duration `json:"read_timeout"`    // 读取超时
	WriteTimeout   time.Duration `json:"write_timeout"`   // 写入超时
	QueryTimeout   time.Duration `json:"query_timeout"`   // 查询超时
	ExecTimeout    time.Duration `json:"exec_timeout"`    // 执行超时

	// WebSocket特定超时
	PingTimeout      time.Duration `json:"ping_timeout"`      // Ping超时
	PongTimeout      time.Duration `json:"pong_timeout"`      // Pong超时
	MessageTimeout   time.Duration `json:"message_timeout"`   // 消息处理超时
	HandshakeTimeout time.Duration `json:"handshake_timeout"` // 握手超时

	// 会话超时
	SessionTimeout time.Duration `json:"session_timeout"` // 会话超时
	IdleTimeout    time.Duration `json:"idle_timeout"`    // 空闲超时
}

// DefaultTimeoutConfig 返回默认超时配置
func DefaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		// 基础超时设置
		DefaultTimeout: 60 * time.Second,
		MinTimeout:     5 * time.Second,
		MaxTimeout:     10 * time.Minute,

		// 操作类型特定超时
		ConnectTimeout: 30 * time.Second,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		QueryTimeout:   60 * time.Second,
		ExecTimeout:    300 * time.Second,

		// WebSocket特定超时
		PingTimeout:      60 * time.Second,
		PongTimeout:      10 * time.Second,
		MessageTimeout:   30 * time.Second,
		HandshakeTimeout: 10 * time.Second,

		// 会话超时
		SessionTimeout: 2 * time.Hour,
		IdleTimeout:    30 * time.Minute,
	}
}

// TimeoutController 超时控制器
type TimeoutController struct {
	config *TimeoutConfig
}

// NewTimeoutController 创建超时控制器
func NewTimeoutController(config *TimeoutConfig) *TimeoutController {
	if config == nil {
		config = DefaultTimeoutConfig()
	}
	return &TimeoutController{
		config: config,
	}
}

// NormalizeTimeout 标准化超时时间
func (tc *TimeoutController) NormalizeTimeout(timeout time.Duration) time.Duration {
	if timeout == 0 {
		return tc.config.DefaultTimeout
	}

	if timeout < tc.config.MinTimeout {
		return tc.config.MinTimeout
	}

	if timeout > tc.config.MaxTimeout {
		return tc.config.MaxTimeout
	}

	return timeout
}

// NormalizeTimeoutWithDefault 使用指定默认值标准化超时时间
func (tc *TimeoutController) NormalizeTimeoutWithDefault(timeout, defaultTimeout time.Duration) time.Duration {
	if timeout == 0 {
		timeout = defaultTimeout
	}

	if timeout < tc.config.MinTimeout {
		return tc.config.MinTimeout
	}

	if timeout > tc.config.MaxTimeout {
		return tc.config.MaxTimeout
	}

	return timeout
}

// CreateTimeoutContext 创建带超时的Context
func (tc *TimeoutController) CreateTimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	normalizedTimeout := tc.NormalizeTimeout(timeout)
	return context.WithTimeout(parent, normalizedTimeout)
}

// CreateTimeoutContextWithDefault 使用指定默认值创建带超时的Context
func (tc *TimeoutController) CreateTimeoutContextWithDefault(parent context.Context, timeout, defaultTimeout time.Duration) (context.Context, context.CancelFunc) {
	normalizedTimeout := tc.NormalizeTimeoutWithDefault(timeout, defaultTimeout)
	return context.WithTimeout(parent, normalizedTimeout)
}

// GetConnectTimeout 获取连接超时时间
func (tc *TimeoutController) GetConnectTimeout() time.Duration {
	return tc.config.ConnectTimeout
}

// GetReadTimeout 获取读取超时时间
func (tc *TimeoutController) GetReadTimeout() time.Duration {
	return tc.config.ReadTimeout
}

// GetWriteTimeout 获取写入超时时间
func (tc *TimeoutController) GetWriteTimeout() time.Duration {
	return tc.config.WriteTimeout
}

// GetQueryTimeout 获取查询超时时间
func (tc *TimeoutController) GetQueryTimeout() time.Duration {
	return tc.config.QueryTimeout
}

// GetExecTimeout 获取执行超时时间
func (tc *TimeoutController) GetExecTimeout() time.Duration {
	return tc.config.ExecTimeout
}

// GetSessionTimeout 获取会话超时时间
func (tc *TimeoutController) GetSessionTimeout() time.Duration {
	return tc.config.SessionTimeout
}

// GetIdleTimeout 获取空闲超时时间
func (tc *TimeoutController) GetIdleTimeout() time.Duration {
	return tc.config.IdleTimeout
}

// GetWebSocketTimeouts 获取WebSocket相关超时时间
func (tc *TimeoutController) GetWebSocketTimeouts() WebSocketTimeouts {
	return WebSocketTimeouts{
		PingTimeout:      tc.config.PingTimeout,
		PongTimeout:      tc.config.PongTimeout,
		MessageTimeout:   tc.config.MessageTimeout,
		HandshakeTimeout: tc.config.HandshakeTimeout,
	}
}

// WebSocketTimeouts WebSocket超时配置
type WebSocketTimeouts struct {
	PingTimeout      time.Duration
	PongTimeout      time.Duration
	MessageTimeout   time.Duration
	HandshakeTimeout time.Duration
}

// ValidateTimeout 验证超时时间是否合理
func (tc *TimeoutController) ValidateTimeout(timeout time.Duration) error {
	if timeout < 0 {
		return fmt.Errorf("timeout cannot be negative: %v", timeout)
	}

	if timeout > 0 && timeout < tc.config.MinTimeout {
		return fmt.Errorf("timeout %v is below minimum %v", timeout, tc.config.MinTimeout)
	}

	if timeout > tc.config.MaxTimeout {
		return fmt.Errorf("timeout %v exceeds maximum %v", timeout, tc.config.MaxTimeout)
	}

	return nil
}

// UpdateConfig 更新超时配置
func (tc *TimeoutController) UpdateConfig(config *TimeoutConfig) {
	if config != nil {
		tc.config = config
	}
}

// GetConfig 获取当前配置
func (tc *TimeoutController) GetConfig() *TimeoutConfig {
	// 返回配置副本以防止外部修改
	configCopy := *tc.config
	return &configCopy
}

// TimeoutType 超时类型枚举
type TimeoutType string

const (
	TimeoutTypeConnect   TimeoutType = "connect"
	TimeoutTypeRead      TimeoutType = "read"
	TimeoutTypeWrite     TimeoutType = "write"
	TimeoutTypeQuery     TimeoutType = "query"
	TimeoutTypeExec      TimeoutType = "exec"
	TimeoutTypeSession   TimeoutType = "session"
	TimeoutTypeIdle      TimeoutType = "idle"
	TimeoutTypeWebSocket TimeoutType = "websocket"
)

// GetTimeoutByType 根据类型获取超时时间
func (tc *TimeoutController) GetTimeoutByType(timeoutType TimeoutType) time.Duration {
	switch timeoutType {
	case TimeoutTypeConnect:
		return tc.config.ConnectTimeout
	case TimeoutTypeRead:
		return tc.config.ReadTimeout
	case TimeoutTypeWrite:
		return tc.config.WriteTimeout
	case TimeoutTypeQuery:
		return tc.config.QueryTimeout
	case TimeoutTypeExec:
		return tc.config.ExecTimeout
	case TimeoutTypeSession:
		return tc.config.SessionTimeout
	case TimeoutTypeIdle:
		return tc.config.IdleTimeout
	case TimeoutTypeWebSocket:
		return tc.config.MessageTimeout
	default:
		return tc.config.DefaultTimeout
	}
}

// CreateTypedTimeoutContext 根据类型创建超时Context
func (tc *TimeoutController) CreateTypedTimeoutContext(parent context.Context, timeoutType TimeoutType) (context.Context, context.CancelFunc) {
	timeout := tc.GetTimeoutByType(timeoutType)
	return context.WithTimeout(parent, timeout)
}
