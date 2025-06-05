package db

import (
	"fmt"
	"strings"
)

// 数据库错误类型
type DbErrorType int

const (
	ErrTypeConnection DbErrorType = iota
	ErrTypeAuth
	ErrTypeSQL
	ErrTypeTimeout
	ErrTypeNetwork
	ErrTypeConfig
	ErrTypePermission
	ErrTypeResource
	ErrTypeConstraint
	ErrTypeTransaction
	ErrTypeUnknown
)

// DbError 数据库错误
type DbError struct {
	Type      DbErrorType `json:"type"`
	Code      string      `json:"code"`
	Message   string      `json:"message"`
	Details   string      `json:"details"`
	SQL       string      `json:"sql,omitempty"`
	Retryable bool        `json:"retryable"`
	Fatal     bool        `json:"fatal"`
	Cause     error       `json:"-"`
}

// Error 实现error接口
func (e *DbError) Error() string {
	if e.SQL != "" {
		return fmt.Sprintf("%s: %s (SQL: %s)", e.Code, e.Message, e.SQL)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 实现错误解包
func (e *DbError) Unwrap() error {
	return e.Cause
}

// IsRetryable 判断错误是否可重试
func (e *DbError) IsRetryable() bool {
	return e.Retryable
}

// IsFatal 判断错误是否致命
func (e *DbError) IsFatal() bool {
	return e.Fatal
}

// 错误构造函数

// NewConnectionError 创建连接错误
func NewConnectionError(message string, cause error) *DbError {
	return &DbError{
		Type:      ErrTypeConnection,
		Code:      "DB_CONNECTION_ERROR",
		Message:   message,
		Retryable: true,
		Fatal:     false,
		Cause:     cause,
	}
}

// NewAuthError 创建认证错误
func NewAuthError(message string, cause error) *DbError {
	return &DbError{
		Type:      ErrTypeAuth,
		Code:      "DB_AUTH_ERROR",
		Message:   message,
		Retryable: false,
		Fatal:     true,
		Cause:     cause,
	}
}

// NewSQLError 创建SQL错误
func NewSQLError(message, sql string, cause error) *DbError {
	return &DbError{
		Type:      ErrTypeSQL,
		Code:      "DB_SQL_ERROR",
		Message:   message,
		SQL:       sql,
		Retryable: false,
		Fatal:     false,
		Cause:     cause,
	}
}

// NewTimeoutError 创建超时错误
func NewTimeoutError(message string, cause error) *DbError {
	return &DbError{
		Type:      ErrTypeTimeout,
		Code:      "DB_TIMEOUT_ERROR",
		Message:   message,
		Retryable: true,
		Fatal:     false,
		Cause:     cause,
	}
}

// NewNetworkError 创建网络错误
func NewNetworkError(message string, cause error) *DbError {
	return &DbError{
		Type:      ErrTypeNetwork,
		Code:      "DB_NETWORK_ERROR",
		Message:   message,
		Retryable: true,
		Fatal:     false,
		Cause:     cause,
	}
}

// NewConfigError 创建配置错误
func NewConfigError(message string, cause error) *DbError {
	return &DbError{
		Type:      ErrTypeConfig,
		Code:      "DB_CONFIG_ERROR",
		Message:   message,
		Retryable: false,
		Fatal:     true,
		Cause:     cause,
	}
}

// NewPermissionError 创建权限错误
func NewPermissionError(message string, cause error) *DbError {
	return &DbError{
		Type:      ErrTypePermission,
		Code:      "DB_PERMISSION_ERROR",
		Message:   message,
		Retryable: false,
		Fatal:     false,
		Cause:     cause,
	}
}

// NewResourceError 创建资源错误
func NewResourceError(message string, cause error) *DbError {
	return &DbError{
		Type:      ErrTypeResource,
		Code:      "DB_RESOURCE_ERROR",
		Message:   message,
		Retryable: true,
		Fatal:     false,
		Cause:     cause,
	}
}

// NewConstraintError 创建约束错误
func NewConstraintError(message, sql string, cause error) *DbError {
	return &DbError{
		Type:      ErrTypeConstraint,
		Code:      "DB_CONSTRAINT_ERROR",
		Message:   message,
		SQL:       sql,
		Retryable: false,
		Fatal:     false,
		Cause:     cause,
	}
}

// NewTransactionError 创建事务错误
func NewTransactionError(message string, cause error) *DbError {
	return &DbError{
		Type:      ErrTypeTransaction,
		Code:      "DB_TRANSACTION_ERROR",
		Message:   message,
		Retryable: true,
		Fatal:     false,
		Cause:     cause,
	}
}

// WrapError 包装错误为数据库错误
func WrapError(err error, sql ...string) *DbError {
	if err == nil {
		return nil
	}

	// 如果已经是数据库错误，直接返回
	if dbErr, ok := err.(*DbError); ok {
		return dbErr
	}

	message := err.Error()
	lowerMsg := strings.ToLower(message)

	var sqlStmt string
	if len(sql) > 0 {
		sqlStmt = sql[0]
	}

	// 根据错误信息判断错误类型
	switch {
	case strings.Contains(lowerMsg, "connection") && (strings.Contains(lowerMsg, "refused") || strings.Contains(lowerMsg, "timeout")):
		return NewConnectionError(message, err)
	case strings.Contains(lowerMsg, "authentication") || strings.Contains(lowerMsg, "access denied") || strings.Contains(lowerMsg, "login failed"):
		return NewAuthError(message, err)
	case strings.Contains(lowerMsg, "timeout") || strings.Contains(lowerMsg, "deadline"):
		return NewTimeoutError(message, err)
	case strings.Contains(lowerMsg, "network") || strings.Contains(lowerMsg, "tcp") || strings.Contains(lowerMsg, "connection reset"):
		return NewNetworkError(message, err)
	case strings.Contains(lowerMsg, "permission") || strings.Contains(lowerMsg, "denied"):
		return NewPermissionError(message, err)
	case strings.Contains(lowerMsg, "constraint") || strings.Contains(lowerMsg, "duplicate") || strings.Contains(lowerMsg, "foreign key"):
		return NewConstraintError(message, sqlStmt, err)
	case strings.Contains(lowerMsg, "transaction") || strings.Contains(lowerMsg, "deadlock"):
		return NewTransactionError(message, err)
	case sqlStmt != "":
		return NewSQLError(message, sqlStmt, err)
	default:
		return &DbError{
			Type:      ErrTypeUnknown,
			Code:      "DB_UNKNOWN_ERROR",
			Message:   message,
			Retryable: false,
			Fatal:     false,
			Cause:     err,
		}
	}
}

// IsConnectionError 判断是否为连接错误
func IsConnectionError(err error) bool {
	if dbErr, ok := err.(*DbError); ok {
		return dbErr.Type == ErrTypeConnection
	}
	return false
}

// IsAuthError 判断是否为认证错误
func IsAuthError(err error) bool {
	if dbErr, ok := err.(*DbError); ok {
		return dbErr.Type == ErrTypeAuth
	}
	return false
}

// IsSQLError 判断是否为SQL错误
func IsSQLError(err error) bool {
	if dbErr, ok := err.(*DbError); ok {
		return dbErr.Type == ErrTypeSQL
	}
	return false
}

// IsTimeoutError 判断是否为超时错误
func IsTimeoutError(err error) bool {
	if dbErr, ok := err.(*DbError); ok {
		return dbErr.Type == ErrTypeTimeout
	}
	return false
}

// IsRetryableError 判断错误是否可重试
func IsRetryableError(err error) bool {
	if dbErr, ok := err.(*DbError); ok {
		return dbErr.IsRetryable()
	}
	return false
}

// IsFatalError 判断错误是否致命
func IsFatalError(err error) bool {
	if dbErr, ok := err.(*DbError); ok {
		return dbErr.IsFatal()
	}
	return false
}

// ErrorSummary 错误摘要
type ErrorSummary struct {
	TotalErrors     int            `json:"total_errors"`
	ErrorsByType    map[string]int `json:"errors_by_type"`
	RetryableErrors int            `json:"retryable_errors"`
	FatalErrors     int            `json:"fatal_errors"`
	RecentErrors    []*DbError     `json:"recent_errors"`
}

// ErrorCollector 错误收集器
type ErrorCollector struct {
	errors   []*DbError
	maxSize  int
	counters map[DbErrorType]int
}

// NewErrorCollector 创建错误收集器
func NewErrorCollector(maxSize int) *ErrorCollector {
	return &ErrorCollector{
		errors:   make([]*DbError, 0, maxSize),
		maxSize:  maxSize,
		counters: make(map[DbErrorType]int),
	}
}

// Add 添加错误
func (ec *ErrorCollector) Add(err *DbError) {
	if err == nil {
		return
	}

	// 更新计数器
	ec.counters[err.Type]++

	// 添加到错误列表
	if len(ec.errors) >= ec.maxSize {
		// 移除最老的错误
		copy(ec.errors, ec.errors[1:])
		ec.errors = ec.errors[:len(ec.errors)-1]
	}
	ec.errors = append(ec.errors, err)
}

// GetSummary 获取错误摘要
func (ec *ErrorCollector) GetSummary() *ErrorSummary {
	errorsByType := make(map[string]int)
	retryableCount := 0
	fatalCount := 0
	totalCount := 0

	for errType, count := range ec.counters {
		totalCount += count
		switch errType {
		case ErrTypeConnection:
			errorsByType["connection"] = count
		case ErrTypeAuth:
			errorsByType["auth"] = count
			fatalCount += count
		case ErrTypeSQL:
			errorsByType["sql"] = count
		case ErrTypeTimeout:
			errorsByType["timeout"] = count
			retryableCount += count
		case ErrTypeNetwork:
			errorsByType["network"] = count
			retryableCount += count
		case ErrTypeConfig:
			errorsByType["config"] = count
			fatalCount += count
		case ErrTypePermission:
			errorsByType["permission"] = count
		case ErrTypeResource:
			errorsByType["resource"] = count
			retryableCount += count
		case ErrTypeConstraint:
			errorsByType["constraint"] = count
		case ErrTypeTransaction:
			errorsByType["transaction"] = count
			retryableCount += count
		default:
			errorsByType["unknown"] = count
		}
	}

	// 复制最近的错误
	recentErrors := make([]*DbError, len(ec.errors))
	copy(recentErrors, ec.errors)

	return &ErrorSummary{
		TotalErrors:     totalCount,
		ErrorsByType:    errorsByType,
		RetryableErrors: retryableCount,
		FatalErrors:     fatalCount,
		RecentErrors:    recentErrors,
	}
}

// Clear 清空错误
func (ec *ErrorCollector) Clear() {
	ec.errors = ec.errors[:0]
	ec.counters = make(map[DbErrorType]int)
}
