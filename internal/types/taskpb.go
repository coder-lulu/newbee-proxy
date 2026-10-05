package types

// 彻底移除 gRPC/protobuf 后，为保持现有内部代码结构，补充最小可用的任务类型定义。

// Credentials 执行凭据
type Credentials struct {
    Username   string `json:"username,omitempty"`
    Password   string `json:"password,omitempty"`
    PrivateKey string `json:"private_key,omitempty"`
    AuthMethod string `json:"auth_method,omitempty"`
}

// TaskAssignment 任务下发结构（TaskType/TaskStatus 复用 agent.go 的定义）
type TaskAssignment struct {
    TaskId         string            `json:"task_id"`
    CommandId      string            `json:"command_id,omitempty"`
    TaskType       TaskType          `json:"task_type"`
    Target         string            `json:"target"`
    TargetPort     int32             `json:"target_port,omitempty"`
    Credentials    *Credentials      `json:"credentials,omitempty"`
    Payload        string            `json:"payload,omitempty"`
    TimeoutSeconds int32             `json:"timeout_seconds,omitempty"`
    Priority       int32             `json:"priority,omitempty"`
    Options        map[string]string `json:"options,omitempty"`
}

// PBTaskResult 任务执行结果（专用于内部执行器/上报路径，避免与 agent.go 的 TaskResult 重名）
type PBTaskResult struct {
    TaskId          string     `json:"task_id"`
    CommandId       string     `json:"command_id,omitempty"`
    Status          TaskStatus `json:"status"`
    ResultData      string     `json:"result_data,omitempty"`
    ErrorMessage    string     `json:"error_message,omitempty"`
    ExecutionTimeMs int64      `json:"execution_time_ms,omitempty"`
}

// 兼容历史常量名（别名），便于不改动业务代码
const (
    // TaskType 别名
    TaskType_COMMAND_EXECUTE TaskType = TaskTypeCommand
    TaskType_SCRIPT_EXECUTE  TaskType = TaskTypeScript
    TaskType_FILE_TRANSFER   TaskType = TaskTypeFileTransfer
    // TaskStatus 别名
    TaskStatus_TASK_PENDING   TaskStatus = TaskStatusPending
    TaskStatus_TASK_RUNNING   TaskStatus = TaskStatusRunning
    TaskStatus_TASK_COMPLETED TaskStatus = TaskStatusCompleted
    TaskStatus_TASK_FAILED    TaskStatus = TaskStatusFailed
    TaskStatus_TASK_TIMEOUT   TaskStatus = TaskStatusTimeout
    TaskStatus_TASK_CANCELLED TaskStatus = TaskStatusCancelled
)
