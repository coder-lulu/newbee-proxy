package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// LogLevel 日志级别
type LogLevel string

const (
	LogLevelDebug LogLevel = "DEBUG"
	LogLevelInfo  LogLevel = "INFO"
	LogLevelWarn  LogLevel = "WARN"
	LogLevelError LogLevel = "ERROR"
	LogLevelFatal LogLevel = "FATAL"
)

// LogContext 日志上下文
type LogContext struct {
	TraceID    string            `json:"trace_id,omitempty"`
	TaskID     string            `json:"task_id,omitempty"`
	SessionID  string            `json:"session_id,omitempty"`
	Protocol   string            `json:"protocol,omitempty"`
	Target     string            `json:"target,omitempty"`
	Component  string            `json:"component,omitempty"`
	Operation  string            `json:"operation,omitempty"`
	Duration   time.Duration     `json:"duration,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Error      error             `json:"error,omitempty"`
	StackTrace string            `json:"stack_trace,omitempty"`
}

// StructuredLogger 结构化日志器
type StructuredLogger struct {
	logger logx.Logger
	ctx    *LogContext
}

// NewStructuredLogger 创建结构化日志器
func NewStructuredLogger(component string) *StructuredLogger {
	return &StructuredLogger{
		logger: logx.WithContext(context.Background()),
		ctx: &LogContext{
			Component: component,
			Metadata:  make(map[string]string),
		},
	}
}

// WithContext 设置日志上下文
func (sl *StructuredLogger) WithContext(ctx *LogContext) *StructuredLogger {
	newLogger := &StructuredLogger{
		logger: sl.logger,
		ctx:    &LogContext{},
	}

	// 复制现有上下文
	*newLogger.ctx = *sl.ctx

	// 合并新上下文
	if ctx.TraceID != "" {
		newLogger.ctx.TraceID = ctx.TraceID
	}
	if ctx.TaskID != "" {
		newLogger.ctx.TaskID = ctx.TaskID
	}
	if ctx.SessionID != "" {
		newLogger.ctx.SessionID = ctx.SessionID
	}
	if ctx.Protocol != "" {
		newLogger.ctx.Protocol = ctx.Protocol
	}
	if ctx.Target != "" {
		newLogger.ctx.Target = ctx.Target
	}
	if ctx.Operation != "" {
		newLogger.ctx.Operation = ctx.Operation
	}
	if ctx.Duration != 0 {
		newLogger.ctx.Duration = ctx.Duration
	}
	if ctx.Error != nil {
		newLogger.ctx.Error = ctx.Error
	}

	// 合并metadata
	for k, v := range ctx.Metadata {
		newLogger.ctx.Metadata[k] = v
	}

	return newLogger
}

// WithTaskID 设置任务ID
func (sl *StructuredLogger) WithTaskID(taskID string) *StructuredLogger {
	return sl.WithContext(&LogContext{TaskID: taskID})
}

// WithSession 设置会话信息
func (sl *StructuredLogger) WithSession(sessionID, protocol, target string) *StructuredLogger {
	return sl.WithContext(&LogContext{
		SessionID: sessionID,
		Protocol:  protocol,
		Target:    target,
	})
}

// WithOperation 设置操作信息
func (sl *StructuredLogger) WithOperation(operation string) *StructuredLogger {
	return sl.WithContext(&LogContext{Operation: operation})
}

// WithError 设置错误信息
func (sl *StructuredLogger) WithError(err error) *StructuredLogger {
	ctx := &LogContext{Error: err}

	// 如果是panic，添加堆栈信息
	if err != nil {
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		ctx.StackTrace = string(buf[:n])
	}

	return sl.WithContext(ctx)
}

// WithDuration 设置执行时间
func (sl *StructuredLogger) WithDuration(duration time.Duration) *StructuredLogger {
	return sl.WithContext(&LogContext{Duration: duration})
}

// WithMetadata 设置元数据
func (sl *StructuredLogger) WithMetadata(key, value string) *StructuredLogger {
	return sl.WithContext(&LogContext{
		Metadata: map[string]string{key: value},
	})
}

// formatMessage 格式化日志消息
func (sl *StructuredLogger) formatMessage(level LogLevel, message string) string {
	logEntry := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"level":     level,
		"message":   message,
		"component": sl.ctx.Component,
	}

	// 添加上下文信息
	if sl.ctx.TraceID != "" {
		logEntry["trace_id"] = sl.ctx.TraceID
	}
	if sl.ctx.TaskID != "" {
		logEntry["task_id"] = sl.ctx.TaskID
	}
	if sl.ctx.SessionID != "" {
		logEntry["session_id"] = sl.ctx.SessionID
	}
	if sl.ctx.Protocol != "" {
		logEntry["protocol"] = sl.ctx.Protocol
	}
	if sl.ctx.Target != "" {
		logEntry["target"] = sl.ctx.Target
	}
	if sl.ctx.Operation != "" {
		logEntry["operation"] = sl.ctx.Operation
	}
	if sl.ctx.Duration != 0 {
		logEntry["duration_ms"] = sl.ctx.Duration.Milliseconds()
	}
	if sl.ctx.Error != nil {
		logEntry["error"] = sl.ctx.Error.Error()
	}
	if sl.ctx.StackTrace != "" {
		logEntry["stack_trace"] = sl.ctx.StackTrace
	}
	if len(sl.ctx.Metadata) > 0 {
		logEntry["metadata"] = sl.ctx.Metadata
	}

	// 序列化为JSON
	jsonBytes, _ := json.Marshal(logEntry)
	return string(jsonBytes)
}

// Debug 调试日志
func (sl *StructuredLogger) Debug(message string) {
	formatted := sl.formatMessage(LogLevelDebug, message)
	sl.logger.Infof("[DEBUG] %s", formatted)
}

// Info 信息日志
func (sl *StructuredLogger) Info(message string) {
	formatted := sl.formatMessage(LogLevelInfo, message)
	sl.logger.Infof("[INFO] %s", formatted)
}

// Warn 警告日志
func (sl *StructuredLogger) Warn(message string) {
	formatted := sl.formatMessage(LogLevelWarn, message)
	sl.logger.Infof("[WARN] %s", formatted)
}

// Error 错误日志
func (sl *StructuredLogger) Error(message string) {
	formatted := sl.formatMessage(LogLevelError, message)
	sl.logger.Errorf("[ERROR] %s", formatted)
}

// Fatal 致命错误日志
func (sl *StructuredLogger) Fatal(message string) {
	formatted := sl.formatMessage(LogLevelFatal, message)
	sl.logger.Errorf("[FATAL] %s", formatted)
}

// 操作日志的便捷方法

// LogConnectionStart 记录连接开始
func (sl *StructuredLogger) LogConnectionStart(target, protocol string) {
	sl.WithSession("", protocol, target).
		WithOperation("connection_start").
		Info("开始建立连接")
}

// LogConnectionSuccess 记录连接成功
func (sl *StructuredLogger) LogConnectionSuccess(sessionID, target, protocol string, duration time.Duration) {
	sl.WithSession(sessionID, protocol, target).
		WithOperation("connection_success").
		WithDuration(duration).
		Info("连接建立成功")
}

// LogConnectionFailed 记录连接失败
func (sl *StructuredLogger) LogConnectionFailed(target, protocol string, duration time.Duration, err error) {
	sl.WithSession("", protocol, target).
		WithOperation("connection_failed").
		WithDuration(duration).
		WithError(err).
		Error("连接建立失败")
}

// LogTaskStart 记录任务开始
func (sl *StructuredLogger) LogTaskStart(taskID, taskType, target string) {
	sl.WithTaskID(taskID).
		WithTarget(target).
		WithOperation("task_start").
		WithMetadata("task_type", taskType).
		Info("任务开始执行")
}

// LogTaskSuccess 记录任务成功
func (sl *StructuredLogger) LogTaskSuccess(taskID string, duration time.Duration, metadata map[string]string) {
	logger := sl.WithTaskID(taskID).
		WithOperation("task_success").
		WithDuration(duration)

	for k, v := range metadata {
		logger = logger.WithMetadata(k, v)
	}

	logger.Info("任务执行成功")
}

// LogTaskFailed 记录任务失败
func (sl *StructuredLogger) LogTaskFailed(taskID string, duration time.Duration, err error) {
	sl.WithTaskID(taskID).
		WithOperation("task_failed").
		WithDuration(duration).
		WithError(err).
		Error("任务执行失败")
}

// LogAuthStart 记录认证开始
func (sl *StructuredLogger) LogAuthStart(sessionID, protocol, username string) {
	sl.WithSession(sessionID, protocol, "").
		WithOperation("auth_start").
		WithMetadata("username", username).
		Info("开始身份认证")
}

// LogAuthSuccess 记录认证成功
func (sl *StructuredLogger) LogAuthSuccess(sessionID, protocol, username string, duration time.Duration) {
	sl.WithSession(sessionID, protocol, "").
		WithOperation("auth_success").
		WithDuration(duration).
		WithMetadata("username", username).
		Info("身份认证成功")
}

// LogAuthFailed 记录认证失败
func (sl *StructuredLogger) LogAuthFailed(sessionID, protocol, username string, duration time.Duration, err error) {
	sl.WithSession(sessionID, protocol, "").
		WithOperation("auth_failed").
		WithDuration(duration).
		WithMetadata("username", username).
		WithError(err).
		Error("身份认证失败")
}

// LogCommandExecute 记录命令执行
func (sl *StructuredLogger) LogCommandExecute(taskID, command string) {
	sl.WithTaskID(taskID).
		WithOperation("command_execute").
		WithMetadata("command", command).
		Info("执行命令")
}

// LogScriptExecute 记录脚本执行
func (sl *StructuredLogger) LogScriptExecute(taskID, scriptType string, lineCount int) {
	sl.WithTaskID(taskID).
		WithOperation("script_execute").
		WithMetadata("script_type", scriptType).
		WithMetadata("line_count", fmt.Sprintf("%d", lineCount)).
		Info("执行脚本")
}

// LogTimeout 记录超时事件
func (sl *StructuredLogger) LogTimeout(operation string, expectedDuration, actualDuration time.Duration) {
	sl.WithOperation(operation+"_timeout").
		WithDuration(actualDuration).
		WithMetadata("expected_duration_ms", fmt.Sprintf("%d", expectedDuration.Milliseconds())).
		WithMetadata("actual_duration_ms", fmt.Sprintf("%d", actualDuration.Milliseconds())).
		Warn("操作超时")
}

// LogMemoryCleanup 记录内存清理
func (sl *StructuredLogger) LogMemoryCleanup(before, after int, duration time.Duration) {
	sl.WithOperation("memory_cleanup").
		WithDuration(duration).
		WithMetadata("before_count", fmt.Sprintf("%d", before)).
		WithMetadata("after_count", fmt.Sprintf("%d", after)).
		WithMetadata("cleaned_count", fmt.Sprintf("%d", before-after)).
		Info("内存清理完成")
}

// LogResourceUsage 记录资源使用情况
func (sl *StructuredLogger) LogResourceUsage(metrics *RuntimeMetrics) {
	sl.WithOperation("resource_usage").
		WithMetadata("goroutine_count", fmt.Sprintf("%d", metrics.GoroutineCount)).
		WithMetadata("memory_usage_mb", fmt.Sprintf("%.2f", float64(metrics.MemoryUsage)/1024/1024)).
		WithMetadata("gc_count", fmt.Sprintf("%d", metrics.GCCount)).
		Info("资源使用统计")
}

// WithTarget 设置目标信息（内部辅助方法）
func (sl *StructuredLogger) WithTarget(target string) *StructuredLogger {
	return sl.WithContext(&LogContext{Target: target})
}
