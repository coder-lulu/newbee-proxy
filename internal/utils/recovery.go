package utils

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// PanicRecoveryMiddleware HTTP panic恢复中间件
func PanicRecoveryMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logx.Errorf("HTTP handler panic recovered: %v\nStack trace:\n%s",
					err, string(debug.Stack()))

				// 返回500错误
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		next(w, r)
	}
}

// SafeGoroutine 安全启动goroutine的包装函数
func SafeGoroutine(name string, fn func()) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				logx.Errorf("Goroutine '%s' panic recovered: %v\nStack trace:\n%s",
					name, err, string(debug.Stack()))
			}
		}()

		fn()
	}()
}

// SafeGoroutineWithContext 带context的安全goroutine
func SafeGoroutineWithContext(ctx context.Context, name string, fn func(context.Context)) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				logx.Errorf("Goroutine '%s' panic recovered: %v\nStack trace:\n%s",
					name, err, string(debug.Stack()))
			}
		}()

		fn(ctx)
	}()
}

// SafeExecute 安全执行函数，捕获panic
func SafeExecute(name string, fn func() error) error {
	defer func() {
		if err := recover(); err != nil {
			logx.Errorf("Function '%s' panic recovered: %v\nStack trace:\n%s",
				name, err, string(debug.Stack()))
		}
	}()

	return fn()
}

// HealthCheck 健康检查包装器
func SafeHealthCheck(name string, fn func() error) error {
	defer func() {
		if err := recover(); err != nil {
			logx.Errorf("Health check '%s' panic recovered: %v", name, err)
		}
	}()

	return fn()
}

// CircuitBreaker 简单的断路器实现
type CircuitBreaker struct {
	failureCount    int
	failureLimit    int
	lastFailureTime time.Time
	timeout         time.Duration
	state           string // "closed", "open", "half-open"
}

// NewCircuitBreaker 创建断路器
func NewCircuitBreaker(failureLimit int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		failureLimit: failureLimit,
		timeout:      timeout,
		state:        "closed",
	}
}

// Execute 通过断路器执行函数
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if cb.state == "open" {
		if time.Since(cb.lastFailureTime) > cb.timeout {
			cb.state = "half-open"
		} else {
			return fmt.Errorf("circuit breaker is open")
		}
	}

	err := fn()
	if err != nil {
		cb.failureCount++
		cb.lastFailureTime = time.Now()

		if cb.failureCount >= cb.failureLimit {
			cb.state = "open"
		}

		return err
	}

	// 成功执行，重置计数器
	cb.failureCount = 0
	cb.state = "closed"
	return nil
}

// RuntimeMetrics 运行时指标
type RuntimeMetrics struct {
	GoroutineCount int     `json:"goroutine_count"`
	MemoryUsage    uint64  `json:"memory_usage_bytes"`
	GCCount        uint32  `json:"gc_count"`
	CPUUsage       float64 `json:"cpu_usage_percent"`
}

// GetRuntimeMetrics 获取运行时指标
func GetRuntimeMetrics() *RuntimeMetrics {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &RuntimeMetrics{
		GoroutineCount: runtime.NumGoroutine(),
		MemoryUsage:    m.Alloc,
		GCCount:        m.NumGC,
		CPUUsage:       0.0, // 需要额外计算
	}
}

// MonitorResources 资源监控
func MonitorResources(interval time.Duration, callback func(*RuntimeMetrics)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			SafeExecute("resource_monitor", func() error {
				metrics := GetRuntimeMetrics()
				callback(metrics)
				return nil
			})
		}
	}
}
