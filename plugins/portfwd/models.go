package portfwd

import "time"

// ForwardRule 端口转发规则（最小实现：TCP 本地转发）
type ForwardRule struct {
    ID              string    `json:"id"`
    Listen          string    `json:"listen"`           // 监听地址，如 ":18080" 或 "0.0.0.0:18080"
    Target          string    `json:"target"`           // 目标地址，如 "127.0.0.1:8080"
    Protocol        string    `json:"protocol"`         // 固定 "tcp"
    DialTimeout     string    `json:"dial_timeout,omitempty"`
    IdleTimeout     string    `json:"idle_timeout,omitempty"`
    KeepAliveSec    int       `json:"keepalive_seconds,omitempty"`
    NoDelay         bool      `json:"nodelay,omitempty"`
    MaxConns        int       `json:"max_conns,omitempty"` // 0 表示不限
    CreatedAt       time.Time `json:"created_at"`

    // 运行态（只读）
    Running     bool `json:"running"`
    ActiveConns int  `json:"active_conns"`
}

