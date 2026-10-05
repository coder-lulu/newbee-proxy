package types

import (
	"context"
	"time"
)

// CommandExecutor 命令执行器接口
type CommandExecutor interface {
	// 执行单个命令
	ExecuteCommand(ctx context.Context, req *CommandRequest) (*CommandResult, error)
	// 执行脚本
	ExecuteScript(ctx context.Context, req *ScriptRequest) (*ScriptResult, error)
	// 获取支持的协议
	SupportedProtocols() []string
	// 健康检查
	HealthCheck() error
}

// CommandRequest 命令执行请求
type CommandRequest struct {
	TaskID       string            `json:"task_id"`
	Target       string            `json:"target"`        // 目标主机地址
	Port         int32             `json:"port"`          // 端口
	Protocol     string            `json:"protocol"`      // ssh/telnet等
	Username     string            `json:"username"`      // 用户名
	Password     string            `json:"password"`      // 密码
	PrivateKey   string            `json:"private_key"`   // 私钥(PEM格式)
	Command      string            `json:"command"`       // 要执行的命令
	WorkingDir   string            `json:"working_dir"`   // 工作目录
	Timeout      int32             `json:"timeout"`       // 超时时间(秒)
    UseSudo      bool              `json:"use_sudo"`          // 是否使用sudo
    SudoPassword string            `json:"sudo_password"`     // sudo密码
    // 使用 sudo -E 保留环境变量（需目标机 sudoers 允许），默认 false。
    SudoPreserveEnv bool           `json:"sudo_preserve_env"`
    // 需要 TTY 的 sudo 场景，可请求分配 PTY
    RequestPty bool   `json:"request_pty"`
    PtyTerm    string `json:"pty_term"` // 默认 xterm-256color
    PtyRows    int    `json:"pty_rows"` // 默认 40
    PtyCols    int    `json:"pty_cols"` // 默认 120
    Environment  map[string]string `json:"environment"`   // 环境变量
}

// CommandResult 命令执行结果
type CommandResult struct {
	TaskID        string            `json:"task_id"`
	Success       bool              `json:"success"`
	ExitCode      int               `json:"exit_code"`
	Stdout        string            `json:"stdout"`         // 标准输出
	Stderr        string            `json:"stderr"`         // 标准错误
	ErrorMessage  string            `json:"error_message"`  // 执行错误信息
	ExecutionTime time.Duration     `json:"execution_time"` // 执行耗时
	StartTime     time.Time         `json:"start_time"`
	EndTime       time.Time         `json:"end_time"`
	Metadata      map[string]string `json:"metadata"` // 额外元数据
}

// ScriptRequest 脚本执行请求
type ScriptRequest struct {
	TaskID         string            `json:"task_id"`
	Target         string            `json:"target"`           // 目标主机地址
	Port           int32             `json:"port"`             // 端口
	Protocol       string            `json:"protocol"`         // ssh/telnet等
	Username       string            `json:"username"`         // 用户名
	Password       string            `json:"password"`         // 密码
	PrivateKey     string            `json:"private_key"`      // 私钥(PEM格式)
	ScriptContent  string            `json:"script_content"`   // 脚本内容
	ScriptType     string            `json:"script_type"`      // shell/bash/python/powershell等
	WorkingDir     string            `json:"working_dir"`      // 工作目录
	Timeout        int32             `json:"timeout"`          // 超时时间(秒)
    UseSudo        bool              `json:"use_sudo"`           // 是否使用sudo
    SudoPassword   string            `json:"sudo_password"`      // sudo密码
    // 使用 sudo -E 保留环境变量（需目标机 sudoers 允许），默认 false。
    SudoPreserveEnv bool             `json:"sudo_preserve_env"`
    // 需要 TTY 的 sudo 场景，可请求分配 PTY
    RequestPty bool   `json:"request_pty"`
    PtyTerm    string `json:"pty_term"` // 默认 xterm-256color
    PtyRows    int    `json:"pty_rows"` // 默认 40
    PtyCols    int    `json:"pty_cols"` // 默认 120
    Environment    map[string]string `json:"environment"`      // 环境变量
	RemoteFileMode string            `json:"remote_file_mode"` // 文件权限，如"755"
	RemoteFilePath string            `json:"remote_file_path"` // 指定远程脚本路径(可选)
	CleanupAfter   bool              `json:"cleanup_after"`    // 执行后是否清理脚本文件
}

// ScriptResult 脚本执行结果
type ScriptResult struct {
	TaskID         string            `json:"task_id"`
	Success        bool              `json:"success"`
	ExitCode       int               `json:"exit_code"`
	Stdout         string            `json:"stdout"`         // 标准输出
	Stderr         string            `json:"stderr"`         // 标准错误
	ErrorMessage   string            `json:"error_message"`  // 执行错误信息
	ExecutionTime  time.Duration     `json:"execution_time"` // 执行耗时
	StartTime      time.Time         `json:"start_time"`
	EndTime        time.Time         `json:"end_time"`
	RemoteFilePath string            `json:"remote_file_path"` // 上传的脚本文件路径
	UploadTime     time.Duration     `json:"upload_time"`      // 文件上传耗时
	Metadata       map[string]string `json:"metadata"`         // 额外元数据
}

// ExecutionStatus 执行状态
type ExecutionStatus string

const (
	StatusPending   ExecutionStatus = "pending"
	StatusRunning   ExecutionStatus = "running"
	StatusCompleted ExecutionStatus = "completed"
	StatusFailed    ExecutionStatus = "failed"
	StatusTimeout   ExecutionStatus = "timeout"
	StatusCancelled ExecutionStatus = "cancelled"
)

// FileTransferRequest 文件传输请求
type FileTransferRequest struct {
	TaskID     string `json:"task_id"`
	Target     string `json:"target"`      // 目标主机地址
	Port       int32  `json:"port"`        // 端口
	Protocol   string `json:"protocol"`    // ssh/sftp等
	Username   string `json:"username"`    // 用户名
	Password   string `json:"password"`    // 密码
	PrivateKey string `json:"private_key"` // 私钥(PEM格式)
	SourcePath string `json:"source_path"` // 源文件路径
	TargetPath string `json:"target_path"` // 目标文件路径
	Direction  string `json:"direction"`   // upload/download
	Timeout    int32  `json:"timeout"`     // 超时时间(秒)
}

// FileTransferResult 文件传输结果
type FileTransferResult struct {
	TaskID        string            `json:"task_id"`
	Success       bool              `json:"success"`
	ErrorMessage  string            `json:"error_message"`
	TransferTime  time.Duration     `json:"transfer_time"`
	BytesTotal    int64             `json:"bytes_total"`
	BytesTransfer int64             `json:"bytes_transfer"`
	StartTime     time.Time         `json:"start_time"`
	EndTime       time.Time         `json:"end_time"`
	Metadata      map[string]string `json:"metadata"`
}

// AuthMethod 认证方式
type AuthMethod string

const (
	AuthPassword  AuthMethod = "password"
	AuthPublicKey AuthMethod = "publickey"
	AuthNone      AuthMethod = "none"
)
