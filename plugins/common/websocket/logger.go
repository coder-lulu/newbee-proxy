package websocket

import (
	"fmt"
	"log"
	"time"
)

// Logger 简单日志接口
type Logger interface {
	Info(msg string)
	Infof(format string, args ...interface{})
	Warn(msg string)
	Warnf(format string, args ...interface{})
	Error(msg string)
	Errorf(format string, args ...interface{})
}

// SimpleLogger 简单日志实现
type SimpleLogger struct {
	prefix string
}

// NewSimpleLogger 创建简单日志器
func NewSimpleLogger(prefix string) Logger {
	return &SimpleLogger{
		prefix: prefix,
	}
}

// Info 记录信息日志
func (l *SimpleLogger) Info(msg string) {
	log.Printf("[INFO] [%s] %s %s", l.prefix, time.Now().Format("2006-01-02 15:04:05"), msg)
}

// Infof 记录格式化信息日志
func (l *SimpleLogger) Infof(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	l.Info(msg)
}

// Warn 记录警告日志
func (l *SimpleLogger) Warn(msg string) {
	log.Printf("[WARN] [%s] %s %s", l.prefix, time.Now().Format("2006-01-02 15:04:05"), msg)
}

// Warnf 记录格式化警告日志
func (l *SimpleLogger) Warnf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	l.Warn(msg)
}

// Error 记录错误日志
func (l *SimpleLogger) Error(msg string) {
	log.Printf("[ERROR] [%s] %s %s", l.prefix, time.Now().Format("2006-01-02 15:04:05"), msg)
}

// Errorf 记录格式化错误日志
func (l *SimpleLogger) Errorf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	l.Error(msg)
}

// DefaultLogger 默认日志器
var DefaultLogger = NewSimpleLogger("websocket")
