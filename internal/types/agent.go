package types

import (
	"time"
)

// AgentStatus Agent状态信息
type AgentStatus struct {
    Status          string        `json:"status"`
    ActiveSessions  int           `json:"active_sessions"`
    StartTime       time.Time     `json:"start_time"`
    Uptime          time.Duration `json:"uptime"`
}

// SessionInfo 会话信息
type SessionInfo struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`   // ssh, telnet, rdp, etc.
	Status     string            `json:"status"` // active, inactive, error
	Host       string            `json:"host"`
	Port       int               `json:"port"`
	User       string            `json:"user"`
	CreatedAt  time.Time         `json:"created_at"`
	LastActive time.Time         `json:"last_active"`
	ExpiresAt  time.Time         `json:"expires_at"`
	Metadata   map[string]string `json:"metadata"`
}

// TaskRequest 任务请求
type TaskRequest struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"` // command, script, file_transfer, etc.
	Parameters  map[string]interface{} `json:"parameters"`
	Timeout     int                    `json:"timeout"`  // seconds
	Priority    int                    `json:"priority"` // 1-10
	Metadata    map[string]string      `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	ScheduledAt time.Time              `json:"scheduled_at"`
}

// TaskResult 任务结果
type TaskResult struct {
	TaskID    string            `json:"task_id"`
	Status    string            `json:"status"` // success, failed, timeout, cancelled
	Output    string            `json:"output"`
	Error     string            `json:"error"`
	ExitCode  int               `json:"exit_code"`
	StartTime time.Time         `json:"start_time"`
	EndTime   time.Time         `json:"end_time"`
	Duration  time.Duration     `json:"duration"`
	Metadata  map[string]string `json:"metadata"`
}

// PluginInfo 插件信息
type PluginInfo struct {
	Name     string                 `json:"name"`
	Version  string                 `json:"version"`
	Type     string                 `json:"type"`   // protocol, hardware, monitoring
	Status   string                 `json:"status"` // loaded, unloaded, error
	LoadedAt time.Time              `json:"loaded_at"`
	Config   map[string]interface{} `json:"config"`
	Metadata map[string]string      `json:"metadata"`
}

// MetricsData 监控指标数据
type MetricsData struct {
	Timestamp      time.Time         `json:"timestamp"`
	CPUUsage       float64           `json:"cpu_usage"`
	MemoryUsage    float64           `json:"memory_usage"`
	NetworkIn      int64             `json:"network_in"`
	NetworkOut     int64             `json:"network_out"`
	ActiveSessions int               `json:"active_sessions"`
	CompletedTasks int               `json:"completed_tasks"`
	FailedTasks    int               `json:"failed_tasks"`
	LoadedPlugins  int               `json:"loaded_plugins"`
	Metadata       map[string]string `json:"metadata"`
}

// TaskType 任务类型
type TaskType string

const (
    TaskTypeCommand      TaskType = "command"
    TaskTypeFileTransfer TaskType = "file_transfer"
    TaskTypeScript       TaskType = "script"
    TaskTypeHTTP         TaskType = "http_request"
    TaskTypeTunnel       TaskType = "tunnel"
    TaskTypeProbe        TaskType = "probe"
    TaskTypeHealthCheck  TaskType = "health_check"
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusTimeout   TaskStatus = "timeout"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// Task 任务定义
type Task struct {
	ID          string                 `json:"id"`
	Type        TaskType               `json:"type"`
	Status      TaskStatus             `json:"status"`
	Target      string                 `json:"target"`
	Credentials map[string]interface{} `json:"credentials"`
	Parameters  map[string]interface{} `json:"parameters"`
	Result      map[string]interface{} `json:"result"`
	Error       string                 `json:"error"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at"`
	TimeoutAt   *time.Time             `json:"timeout_at"`
}

// ConnectionInfo 连接信息
type ConnectionInfo struct {
	ID         string            `json:"id"`
	Protocol   string            `json:"protocol"`
	Target     string            `json:"target"`
	Status     string            `json:"status"`
	CreatedAt  time.Time         `json:"created_at"`
	LastActive time.Time         `json:"last_active"`
	SessionID  string            `json:"session_id"`
	Metadata   map[string]string `json:"metadata"`
}
