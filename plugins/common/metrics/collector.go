package metrics

import (
	"sync"
	"time"
)

// MetricsCollector 统一指标收集器
type MetricsCollector struct {
	// 连接指标
	connections *ConnectionMetrics

	// 请求指标
	requests *RequestMetrics

	// 错误指标
	errors *ErrorMetrics

	// 性能指标
	performance *PerformanceMetrics

	// 同步
	mutex sync.RWMutex
}

// ConnectionMetrics 连接指标
type ConnectionMetrics struct {
	// 当前连接统计
	ActiveConnections int64 `json:"active_connections"`
	IdleConnections   int64 `json:"idle_connections"`
	TotalConnections  int64 `json:"total_connections"`
	FailedConnections int64 `json:"failed_connections"`

	// 连接生命周期
	AverageConnectionTime time.Duration `json:"average_connection_time"`
	MaxConnectionTime     time.Duration `json:"max_connection_time"`
	MinConnectionTime     time.Duration `json:"min_connection_time"`

	// 连接使用率
	ConnectionUtilization float64 `json:"connection_utilization"`
	PeakConnections       int64   `json:"peak_connections"`

	// 更新时间
	LastUpdated time.Time `json:"last_updated"`
}

// RequestMetrics 请求指标
type RequestMetrics struct {
	// 请求计数
	TotalRequests      int64 `json:"total_requests"`
	SuccessfulRequests int64 `json:"successful_requests"`
	FailedRequests     int64 `json:"failed_requests"`

	// 请求类型分布
	RequestsByType map[string]int64 `json:"requests_by_type"`

	// 响应时间
	AverageResponseTime time.Duration `json:"average_response_time"`
	MaxResponseTime     time.Duration `json:"max_response_time"`
	MinResponseTime     time.Duration `json:"min_response_time"`

	// 吞吐量（每秒请求数）
	RequestsPerSecond float64 `json:"requests_per_second"`

	// 成功率
	SuccessRate float64 `json:"success_rate"`

	// 更新时间
	LastUpdated time.Time `json:"last_updated"`
}

// ErrorMetrics 错误指标
type ErrorMetrics struct {
	// 错误计数
	TotalErrors int64 `json:"total_errors"`

	// 错误类型分布
	ErrorsByType map[string]int64 `json:"errors_by_type"`

	// 错误率
	ErrorRate float64 `json:"error_rate"`

	// 最近错误
	RecentErrors []ErrorRecord `json:"recent_errors"`

	// 更新时间
	LastUpdated time.Time `json:"last_updated"`
}

// ErrorRecord 错误记录
type ErrorRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
	Severity  string    `json:"severity"`
}

// PerformanceMetrics 性能指标
type PerformanceMetrics struct {
	// CPU使用率（模拟）
	CPUUsage float64 `json:"cpu_usage"`

	// 内存使用
	MemoryUsage   int64   `json:"memory_usage"`
	MemoryPercent float64 `json:"memory_percent"`

	// 网络IO
	NetworkBytesIn  int64 `json:"network_bytes_in"`
	NetworkBytesOut int64 `json:"network_bytes_out"`

	// 数据库查询性能
	SlowQueries      int64         `json:"slow_queries"`
	AverageQueryTime time.Duration `json:"average_query_time"`

	// 更新时间
	LastUpdated time.Time `json:"last_updated"`
}

// NewMetricsCollector 创建指标收集器
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		connections: &ConnectionMetrics{
			LastUpdated: time.Now(),
		},
		requests: &RequestMetrics{
			RequestsByType: make(map[string]int64),
			LastUpdated:    time.Now(),
		},
		errors: &ErrorMetrics{
			ErrorsByType: make(map[string]int64),
			RecentErrors: make([]ErrorRecord, 0, 100), // 保留最近100个错误
			LastUpdated:  time.Now(),
		},
		performance: &PerformanceMetrics{
			LastUpdated: time.Now(),
		},
	}
}

// RecordConnectionStart 记录连接开始
func (mc *MetricsCollector) RecordConnectionStart(connType string) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	mc.connections.ActiveConnections++
	mc.connections.TotalConnections++

	if mc.connections.ActiveConnections > mc.connections.PeakConnections {
		mc.connections.PeakConnections = mc.connections.ActiveConnections
	}

	mc.connections.LastUpdated = time.Now()
}

// RecordConnectionEnd 记录连接结束
func (mc *MetricsCollector) RecordConnectionEnd(connType string, duration time.Duration, success bool) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	mc.connections.ActiveConnections--

	if !success {
		mc.connections.FailedConnections++
	}

	// 更新连接时间统计
	if mc.connections.TotalConnections == 1 {
		mc.connections.AverageConnectionTime = duration
		mc.connections.MaxConnectionTime = duration
		mc.connections.MinConnectionTime = duration
	} else {
		// 简单的移动平均
		mc.connections.AverageConnectionTime =
			(mc.connections.AverageConnectionTime + duration) / 2

		if duration > mc.connections.MaxConnectionTime {
			mc.connections.MaxConnectionTime = duration
		}
		if duration < mc.connections.MinConnectionTime {
			mc.connections.MinConnectionTime = duration
		}
	}

	mc.connections.LastUpdated = time.Now()
}

// RecordRequest 记录请求
func (mc *MetricsCollector) RecordRequest(requestType string, duration time.Duration, success bool) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	mc.requests.TotalRequests++

	if success {
		mc.requests.SuccessfulRequests++
	} else {
		mc.requests.FailedRequests++
	}

	// 按类型统计
	mc.requests.RequestsByType[requestType]++

	// 更新响应时间统计
	if mc.requests.TotalRequests == 1 {
		mc.requests.AverageResponseTime = duration
		mc.requests.MaxResponseTime = duration
		mc.requests.MinResponseTime = duration
	} else {
		// 简单的移动平均
		mc.requests.AverageResponseTime =
			(mc.requests.AverageResponseTime + duration) / 2

		if duration > mc.requests.MaxResponseTime {
			mc.requests.MaxResponseTime = duration
		}
		if duration < mc.requests.MinResponseTime {
			mc.requests.MinResponseTime = duration
		}
	}

	// 计算成功率
	if mc.requests.TotalRequests > 0 {
		mc.requests.SuccessRate = float64(mc.requests.SuccessfulRequests) / float64(mc.requests.TotalRequests)
	}

	mc.requests.LastUpdated = time.Now()
}

// RecordError 记录错误
func (mc *MetricsCollector) RecordError(errorType, message, source, severity string) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	mc.errors.TotalErrors++
	mc.errors.ErrorsByType[errorType]++

	// 添加到最近错误列表
	errorRecord := ErrorRecord{
		Timestamp: time.Now(),
		Type:      errorType,
		Message:   message,
		Source:    source,
		Severity:  severity,
	}

	mc.errors.RecentErrors = append(mc.errors.RecentErrors, errorRecord)

	// 保持最近错误数量在100以内
	if len(mc.errors.RecentErrors) > 100 {
		mc.errors.RecentErrors = mc.errors.RecentErrors[1:]
	}

	// 计算错误率
	totalRequests := mc.requests.TotalRequests
	if totalRequests > 0 {
		mc.errors.ErrorRate = float64(mc.errors.TotalErrors) / float64(totalRequests)
	}

	mc.errors.LastUpdated = time.Now()
}

// GetConnectionMetrics 获取连接指标
func (mc *MetricsCollector) GetConnectionMetrics() ConnectionMetrics {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	// 计算连接使用率
	totalSlots := mc.connections.PeakConnections
	if totalSlots == 0 {
		totalSlots = 100 // 默认假设100个槽位
	}

	utilization := float64(mc.connections.ActiveConnections) / float64(totalSlots)

	metrics := *mc.connections
	metrics.ConnectionUtilization = utilization

	return metrics
}

// GetRequestMetrics 获取请求指标
func (mc *MetricsCollector) GetRequestMetrics() RequestMetrics {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	return *mc.requests
}

// GetErrorMetrics 获取错误指标
func (mc *MetricsCollector) GetErrorMetrics() ErrorMetrics {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	return *mc.errors
}

// GetAllMetrics 获取所有指标
func (mc *MetricsCollector) GetAllMetrics() map[string]interface{} {
	return map[string]interface{}{
		"connections": mc.GetConnectionMetrics(),
		"requests":    mc.GetRequestMetrics(),
		"errors":      mc.GetErrorMetrics(),
		"timestamp":   time.Now(),
	}
}
