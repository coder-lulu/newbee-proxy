package metrics

import (
	"testing"
	"time"
)

func TestNewMetricsCollector(t *testing.T) {
	collector := NewMetricsCollector()

	if collector == nil {
		t.Error("Expected collector to be initialized")
	}

	if collector.connections == nil {
		t.Error("Expected connections metrics to be initialized")
	}

	if collector.requests == nil {
		t.Error("Expected requests metrics to be initialized")
	}

	if collector.errors == nil {
		t.Error("Expected errors metrics to be initialized")
	}

	if collector.requests.RequestsByType == nil {
		t.Error("Expected RequestsByType map to be initialized")
	}

	if collector.errors.ErrorsByType == nil {
		t.Error("Expected ErrorsByType map to be initialized")
	}
}

func TestRecordConnectionStart(t *testing.T) {
	collector := NewMetricsCollector()

	collector.RecordConnectionStart("websocket")

	metrics := collector.GetConnectionMetrics()
	if metrics.ActiveConnections != 1 {
		t.Errorf("Expected 1 active connection, got %d", metrics.ActiveConnections)
	}

	if metrics.TotalConnections != 1 {
		t.Errorf("Expected 1 total connection, got %d", metrics.TotalConnections)
	}

	if metrics.PeakConnections != 1 {
		t.Errorf("Expected 1 peak connection, got %d", metrics.PeakConnections)
	}

	// 测试多个连接
	collector.RecordConnectionStart("ssh")
	metrics = collector.GetConnectionMetrics()
	if metrics.ActiveConnections != 2 {
		t.Errorf("Expected 2 active connections, got %d", metrics.ActiveConnections)
	}

	if metrics.PeakConnections != 2 {
		t.Errorf("Expected 2 peak connections, got %d", metrics.PeakConnections)
	}
}

func TestRecordConnectionEnd(t *testing.T) {
	collector := NewMetricsCollector()

	// 先开始一个连接
	collector.RecordConnectionStart("websocket")

	// 结束连接
	duration := 5 * time.Second
	collector.RecordConnectionEnd("websocket", duration, true)

	metrics := collector.GetConnectionMetrics()
	if metrics.ActiveConnections != 0 {
		t.Errorf("Expected 0 active connections, got %d", metrics.ActiveConnections)
	}

	if metrics.AverageConnectionTime != duration {
		t.Errorf("Expected average connection time %v, got %v", duration, metrics.AverageConnectionTime)
	}

	if metrics.MaxConnectionTime != duration {
		t.Errorf("Expected max connection time %v, got %v", duration, metrics.MaxConnectionTime)
	}

	if metrics.MinConnectionTime != duration {
		t.Errorf("Expected min connection time %v, got %v", duration, metrics.MinConnectionTime)
	}

	// 测试失败的连接
	collector.RecordConnectionStart("ssh")
	collector.RecordConnectionEnd("ssh", 2*time.Second, false)

	metrics = collector.GetConnectionMetrics()
	if metrics.FailedConnections != 1 {
		t.Errorf("Expected 1 failed connection, got %d", metrics.FailedConnections)
	}
}

func TestRecordRequest(t *testing.T) {
	collector := NewMetricsCollector()

	duration := 100 * time.Millisecond
	collector.RecordRequest("query", duration, true)

	metrics := collector.GetRequestMetrics()
	if metrics.TotalRequests != 1 {
		t.Errorf("Expected 1 total request, got %d", metrics.TotalRequests)
	}

	if metrics.SuccessfulRequests != 1 {
		t.Errorf("Expected 1 successful request, got %d", metrics.SuccessfulRequests)
	}

	if metrics.SuccessRate != 1.0 {
		t.Errorf("Expected success rate 1.0, got %f", metrics.SuccessRate)
	}

	if metrics.AverageResponseTime != duration {
		t.Errorf("Expected average response time %v, got %v", duration, metrics.AverageResponseTime)
	}

	if count, exists := metrics.RequestsByType["query"]; !exists || count != 1 {
		t.Errorf("Expected 1 query request, got %d", count)
	}

	// 测试失败的请求
	collector.RecordRequest("insert", 50*time.Millisecond, false)

	metrics = collector.GetRequestMetrics()
	if metrics.TotalRequests != 2 {
		t.Errorf("Expected 2 total requests, got %d", metrics.TotalRequests)
	}

	if metrics.FailedRequests != 1 {
		t.Errorf("Expected 1 failed request, got %d", metrics.FailedRequests)
	}

	if metrics.SuccessRate != 0.5 {
		t.Errorf("Expected success rate 0.5, got %f", metrics.SuccessRate)
	}
}

func TestRecordError(t *testing.T) {
	collector := NewMetricsCollector()

	// 先记录一个请求
	collector.RecordRequest("query", 100*time.Millisecond, true)

	// 记录错误
	collector.RecordError("sql_error", "Connection timeout", "db_plugin", "error")

	errorMetrics := collector.GetErrorMetrics()
	if errorMetrics.TotalErrors != 1 {
		t.Errorf("Expected 1 total error, got %d", errorMetrics.TotalErrors)
	}

	if count, exists := errorMetrics.ErrorsByType["sql_error"]; !exists || count != 1 {
		t.Errorf("Expected 1 sql_error, got %d", count)
	}

	if errorMetrics.ErrorRate != 1.0 {
		t.Errorf("Expected error rate 1.0, got %f", errorMetrics.ErrorRate)
	}

	if len(errorMetrics.RecentErrors) != 1 {
		t.Errorf("Expected 1 recent error, got %d", len(errorMetrics.RecentErrors))
	}

	recentError := errorMetrics.RecentErrors[0]
	if recentError.Type != "sql_error" {
		t.Errorf("Expected error type 'sql_error', got '%s'", recentError.Type)
	}

	if recentError.Message != "Connection timeout" {
		t.Errorf("Expected error message 'Connection timeout', got '%s'", recentError.Message)
	}

	if recentError.Source != "db_plugin" {
		t.Errorf("Expected error source 'db_plugin', got '%s'", recentError.Source)
	}

	if recentError.Severity != "error" {
		t.Errorf("Expected error severity 'error', got '%s'", recentError.Severity)
	}
}

func TestRecentErrorsLimit(t *testing.T) {
	collector := NewMetricsCollector()

	// 记录超过100个错误
	for i := 0; i < 150; i++ {
		collector.RecordError("test_error", "test message", "test_source", "info")
	}

	errorMetrics := collector.GetErrorMetrics()
	if len(errorMetrics.RecentErrors) > 100 {
		t.Errorf("Expected max 100 recent errors, got %d", len(errorMetrics.RecentErrors))
	}

	if errorMetrics.TotalErrors != 150 {
		t.Errorf("Expected 150 total errors, got %d", errorMetrics.TotalErrors)
	}
}

func TestGetAllMetrics(t *testing.T) {
	collector := NewMetricsCollector()

	// 记录一些数据
	collector.RecordConnectionStart("websocket")
	collector.RecordRequest("query", 100*time.Millisecond, true)
	collector.RecordError("test_error", "test message", "test_source", "warning")

	allMetrics := collector.GetAllMetrics()

	if allMetrics == nil {
		t.Error("Expected all metrics to be returned")
	}

	if _, exists := allMetrics["connections"]; !exists {
		t.Error("Expected connections metrics in all metrics")
	}

	if _, exists := allMetrics["requests"]; !exists {
		t.Error("Expected requests metrics in all metrics")
	}

	if _, exists := allMetrics["errors"]; !exists {
		t.Error("Expected errors metrics in all metrics")
	}

	if _, exists := allMetrics["timestamp"]; !exists {
		t.Error("Expected timestamp in all metrics")
	}
}

func TestConnectionUtilization(t *testing.T) {
	collector := NewMetricsCollector()

	// 创建一些连接以建立峰值
	for i := 0; i < 10; i++ {
		collector.RecordConnectionStart("websocket")
	}

	// 结束一些连接
	for i := 0; i < 5; i++ {
		collector.RecordConnectionEnd("websocket", time.Second, true)
	}

	metrics := collector.GetConnectionMetrics()

	// 当前5个活跃连接，峰值10个
	expectedUtilization := 5.0 / 10.0
	if metrics.ConnectionUtilization != expectedUtilization {
		t.Errorf("Expected utilization %f, got %f", expectedUtilization, metrics.ConnectionUtilization)
	}
}

func TestAverageCalculations(t *testing.T) {
	collector := NewMetricsCollector()

	// 测试连接时间平均值计算
	collector.RecordConnectionStart("test")
	collector.RecordConnectionEnd("test", 10*time.Second, true)

	collector.RecordConnectionStart("test")
	collector.RecordConnectionEnd("test", 20*time.Second, true)

	metrics := collector.GetConnectionMetrics()
	expectedAvg := (10*time.Second + 20*time.Second) / 2
	if metrics.AverageConnectionTime != expectedAvg {
		t.Errorf("Expected average connection time %v, got %v", expectedAvg, metrics.AverageConnectionTime)
	}

	// 测试响应时间平均值计算
	collector.RecordRequest("test", 100*time.Millisecond, true)
	collector.RecordRequest("test", 200*time.Millisecond, true)

	requestMetrics := collector.GetRequestMetrics()
	expectedResponseAvg := (100*time.Millisecond + 200*time.Millisecond) / 2
	if requestMetrics.AverageResponseTime != expectedResponseAvg {
		t.Errorf("Expected average response time %v, got %v", expectedResponseAvg, requestMetrics.AverageResponseTime)
	}
}
