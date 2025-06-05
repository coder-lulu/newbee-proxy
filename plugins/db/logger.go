package db

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogLevel 日志级别
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelFatal
)

// String 返回日志级别字符串
func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	case LogLevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// LogEntry 日志条目
type LogEntry struct {
	Timestamp    time.Time              `json:"timestamp"`
	Level        LogLevel               `json:"level"`
	Component    string                 `json:"component"`
	ConnectionID string                 `json:"connection_id,omitempty"`
	SessionID    string                 `json:"session_id,omitempty"`
	Operation    string                 `json:"operation"`
	Message      string                 `json:"message"`
	SQL          string                 `json:"sql,omitempty"`
	Duration     time.Duration          `json:"duration,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	Operator     string                 `json:"operator,omitempty"`
}

// DbLogger 数据库日志记录器
type DbLogger struct {
	level     LogLevel
	outputs   []LogOutput
	mutex     sync.RWMutex
	errorColl *ErrorCollector
}

// LogOutput 日志输出接口
type LogOutput interface {
	Write(entry *LogEntry) error
	Close() error
}

// ConsoleOutput 控制台输出
type ConsoleOutput struct {
	logger *log.Logger
}

// NewConsoleOutput 创建控制台输出
func NewConsoleOutput() *ConsoleOutput {
	return &ConsoleOutput{
		logger: log.New(os.Stdout, "[DB] ", log.LstdFlags),
	}
}

// Write 写入日志条目
func (c *ConsoleOutput) Write(entry *LogEntry) error {
	logMsg := fmt.Sprintf("[%s] %s - %s",
		entry.Level.String(),
		entry.Operation,
		entry.Message)

	if entry.Error != "" {
		logMsg += fmt.Sprintf(" | Error: %s", entry.Error)
	}

	if entry.Duration > 0 {
		logMsg += fmt.Sprintf(" | Duration: %v", entry.Duration)
	}

	if entry.SQL != "" {
		logMsg += fmt.Sprintf(" | SQL: %s", entry.SQL)
	}

	c.logger.Println(logMsg)
	return nil
}

// Close 关闭输出
func (c *ConsoleOutput) Close() error {
	return nil
}

// FileOutput 文件输出
type FileOutput struct {
	filePath string
	file     *os.File
	encoder  *json.Encoder
	mutex    sync.Mutex
}

// NewFileOutput 创建文件输出
func NewFileOutput(filePath string) (*FileOutput, error) {
	// 确保目录存在
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	return &FileOutput{
		filePath: filePath,
		file:     file,
		encoder:  json.NewEncoder(file),
	}, nil
}

// Write 写入日志条目
func (f *FileOutput) Write(entry *LogEntry) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	return f.encoder.Encode(entry)
}

// Close 关闭输出
func (f *FileOutput) Close() error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if f.file != nil {
		return f.file.Close()
	}
	return nil
}

// NewDbLogger 创建数据库日志记录器
func NewDbLogger(level LogLevel) *DbLogger {
	return &DbLogger{
		level:     level,
		outputs:   make([]LogOutput, 0),
		errorColl: NewErrorCollector(100),
	}
}

// AddOutput 添加日志输出
func (l *DbLogger) AddOutput(output LogOutput) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.outputs = append(l.outputs, output)
}

// SetLevel 设置日志级别
func (l *DbLogger) SetLevel(level LogLevel) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.level = level
}

// shouldLog 判断是否应该记录日志
func (l *DbLogger) shouldLog(level LogLevel) bool {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	return level >= l.level
}

// log 记录日志
func (l *DbLogger) log(level LogLevel, component, operation, message string, options ...LogOption) {
	if !l.shouldLog(level) {
		return
	}

	entry := &LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Component: component,
		Operation: operation,
		Message:   message,
		Metadata:  make(map[string]interface{}),
	}

	// 应用选项
	for _, opt := range options {
		opt(entry)
	}

	// 如果是错误日志，添加到错误收集器
	if level >= LogLevelError && entry.Error != "" {
		dbErr := WrapError(fmt.Errorf(entry.Error), entry.SQL)
		l.errorColl.Add(dbErr)
	}

	// 写入所有输出
	l.mutex.RLock()
	outputs := make([]LogOutput, len(l.outputs))
	copy(outputs, l.outputs)
	l.mutex.RUnlock()

	for _, output := range outputs {
		if err := output.Write(entry); err != nil {
			// 写入失败时只能打印到标准错误输出
			fmt.Fprintf(os.Stderr, "Failed to write log: %v\n", err)
		}
	}
}

// LogOption 日志选项
type LogOption func(*LogEntry)

// WithConnectionID 设置连接ID
func WithConnectionID(id string) LogOption {
	return func(entry *LogEntry) {
		entry.ConnectionID = id
	}
}

// WithSessionID 设置会话ID
func WithSessionID(id string) LogOption {
	return func(entry *LogEntry) {
		entry.SessionID = id
	}
}

// WithSQL 设置SQL语句
func WithSQL(sql string) LogOption {
	return func(entry *LogEntry) {
		entry.SQL = sql
	}
}

// WithDuration 设置执行时长
func WithDuration(duration time.Duration) LogOption {
	return func(entry *LogEntry) {
		entry.Duration = duration
	}
}

// WithError 设置错误信息
func WithError(err error) LogOption {
	return func(entry *LogEntry) {
		if err != nil {
			entry.Error = err.Error()
		}
	}
}

// WithMetadata 设置元数据
func WithMetadata(key string, value interface{}) LogOption {
	return func(entry *LogEntry) {
		if entry.Metadata == nil {
			entry.Metadata = make(map[string]interface{})
		}
		entry.Metadata[key] = value
	}
}

// WithOperator 设置操作员
func WithOperator(operator string) LogOption {
	return func(entry *LogEntry) {
		entry.Operator = operator
	}
}

// Debug 记录调试日志
func (l *DbLogger) Debug(component, operation, message string, options ...LogOption) {
	l.log(LogLevelDebug, component, operation, message, options...)
}

// Info 记录信息日志
func (l *DbLogger) Info(component, operation, message string, options ...LogOption) {
	l.log(LogLevelInfo, component, operation, message, options...)
}

// Warn 记录警告日志
func (l *DbLogger) Warn(component, operation, message string, options ...LogOption) {
	l.log(LogLevelWarn, component, operation, message, options...)
}

// Error 记录错误日志
func (l *DbLogger) Error(component, operation, message string, options ...LogOption) {
	l.log(LogLevelError, component, operation, message, options...)
}

// Fatal 记录致命错误日志
func (l *DbLogger) Fatal(component, operation, message string, options ...LogOption) {
	l.log(LogLevelFatal, component, operation, message, options...)
}

// LogConnection 记录连接操作
func (l *DbLogger) LogConnection(operation string, connectionID string, dbInfo *DbInfo, duration time.Duration, err error) {
	options := []LogOption{
		WithConnectionID(connectionID),
		WithDuration(duration),
	}

	if err != nil {
		options = append(options, WithError(err))
	}

	if dbInfo != nil {
		options = append(options,
			WithMetadata("db_type", dbInfo.Type),
			WithMetadata("host", dbInfo.Host),
			WithMetadata("port", dbInfo.Port),
			WithMetadata("database", dbInfo.Database),
		)
	}

	if err != nil {
		l.Error("connection", operation, "Database connection operation failed", options...)
	} else {
		l.Info("connection", operation, "Database connection operation completed", options...)
	}
}

// LogSQL 记录SQL执行
func (l *DbLogger) LogSQL(operation string, connectionID, sql string, duration time.Duration, rowsAffected int64, err error, operator string) {
	options := []LogOption{
		WithConnectionID(connectionID),
		WithSQL(sql),
		WithDuration(duration),
		WithMetadata("rows_affected", rowsAffected),
	}

	if operator != "" {
		options = append(options, WithOperator(operator))
	}

	if err != nil {
		options = append(options, WithError(err))
		l.Error("sql", operation, "SQL execution failed", options...)
	} else {
		l.Info("sql", operation, "SQL executed successfully", options...)
	}
}

// LogTransaction 记录事务操作
func (l *DbLogger) LogTransaction(operation string, connectionID string, duration time.Duration, err error) {
	options := []LogOption{
		WithConnectionID(connectionID),
		WithDuration(duration),
	}

	if err != nil {
		options = append(options, WithError(err))
		l.Error("transaction", operation, "Transaction operation failed", options...)
	} else {
		l.Info("transaction", operation, "Transaction operation completed", options...)
	}
}

// LogWebSocket 记录WebSocket操作
func (l *DbLogger) LogWebSocket(operation string, sessionID, connectionID string, duration time.Duration, err error) {
	options := []LogOption{
		WithSessionID(sessionID),
		WithConnectionID(connectionID),
		WithDuration(duration),
	}

	if err != nil {
		options = append(options, WithError(err))
		l.Error("websocket", operation, "WebSocket operation failed", options...)
	} else {
		l.Info("websocket", operation, "WebSocket operation completed", options...)
	}
}

// GetErrorSummary 获取错误摘要
func (l *DbLogger) GetErrorSummary() *ErrorSummary {
	return l.errorColl.GetSummary()
}

// ClearErrors 清空错误
func (l *DbLogger) ClearErrors() {
	l.errorColl.Clear()
}

// Close 关闭日志记录器
func (l *DbLogger) Close() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	var lastErr error
	for _, output := range l.outputs {
		if err := output.Close(); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// 全局日志记录器实例
var (
	globalLogger *DbLogger
	loggerOnce   sync.Once
)

// GetLogger 获取全局日志记录器
func GetLogger() *DbLogger {
	loggerOnce.Do(func() {
		globalLogger = NewDbLogger(LogLevelInfo)
		globalLogger.AddOutput(NewConsoleOutput())
	})
	return globalLogger
}

// 便捷函数
func LogConnection(operation string, connectionID string, dbInfo *DbInfo, duration time.Duration, err error) {
	GetLogger().LogConnection(operation, connectionID, dbInfo, duration, err)
}

func LogSQL(operation string, connectionID, sql string, duration time.Duration, rowsAffected int64, err error, operator string) {
	GetLogger().LogSQL(operation, connectionID, sql, duration, rowsAffected, err, operator)
}

func LogTransaction(operation string, connectionID string, duration time.Duration, err error) {
	GetLogger().LogTransaction(operation, connectionID, duration, err)
}

func LogWebSocket(operation string, sessionID, connectionID string, duration time.Duration, err error) {
	GetLogger().LogWebSocket(operation, sessionID, connectionID, duration, err)
}

// LogContext 日志上下文
type LogContext struct {
	logger       *DbLogger
	connectionID string
	sessionID    string
	operator     string
	metadata     map[string]interface{}
}

// NewLogContext 创建日志上下文
func NewLogContext(logger *DbLogger) *LogContext {
	if logger == nil {
		logger = GetLogger()
	}
	return &LogContext{
		logger:   logger,
		metadata: make(map[string]interface{}),
	}
}

// WithConnection 设置连接ID
func (lc *LogContext) WithConnection(connectionID string) *LogContext {
	lc.connectionID = connectionID
	return lc
}

// WithSession 设置会话ID
func (lc *LogContext) WithSession(sessionID string) *LogContext {
	lc.sessionID = sessionID
	return lc
}

// WithOperator 设置操作员
func (lc *LogContext) WithOperator(operator string) *LogContext {
	lc.operator = operator
	return lc
}

// WithMetadata 设置元数据
func (lc *LogContext) WithMetadata(key string, value interface{}) *LogContext {
	lc.metadata[key] = value
	return lc
}

// buildOptions 构建日志选项
func (lc *LogContext) buildOptions(extraOptions ...LogOption) []LogOption {
	options := make([]LogOption, 0)

	if lc.connectionID != "" {
		options = append(options, WithConnectionID(lc.connectionID))
	}

	if lc.sessionID != "" {
		options = append(options, WithSessionID(lc.sessionID))
	}

	if lc.operator != "" {
		options = append(options, WithOperator(lc.operator))
	}

	for key, value := range lc.metadata {
		options = append(options, WithMetadata(key, value))
	}

	options = append(options, extraOptions...)
	return options
}

// Info 记录信息日志
func (lc *LogContext) Info(component, operation, message string, options ...LogOption) {
	allOptions := lc.buildOptions(options...)
	lc.logger.Info(component, operation, message, allOptions...)
}

// Error 记录错误日志
func (lc *LogContext) Error(component, operation, message string, options ...LogOption) {
	allOptions := lc.buildOptions(options...)
	lc.logger.Error(component, operation, message, allOptions...)
}
