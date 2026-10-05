package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
)

var (
    dbExecDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Namespace: "worker", Subsystem: "db", Name: "exec_duration_seconds", Help: "DB execution duration seconds",
        Buckets:   prometheus.ExponentialBuckets(0.01, 2, 14), // 10ms..
    }, []string{"db_type", "sql_type"})

    dbExecErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "db", Name: "exec_errors_total", Help: "DB execution errors by db_type and code",
    }, []string{"db_type", "code"})
)

func init() {
    // 注册在 RegisterPrometheus 中统一处理，避免重复
}

// RegisterDBMetrics 注册 DB 指标（由 RegisterPrometheus 间接调用）
func RegisterDBMetrics() {
    _ = prometheus.Register(dbExecDuration)
    _ = prometheus.Register(dbExecErrors)
}

// 对外 API
func ObserveDBExecSeconds(dbType, sqlType string, seconds float64) {
    if dbType == "" { dbType = "unknown" }
    if sqlType == "" { sqlType = "unknown" }
    dbExecDuration.WithLabelValues(dbType, sqlType).Observe(seconds)
}

func IncDBExecError(dbType, code string) {
    if dbType == "" { dbType = "unknown" }
    if code == "" { code = "unknown" }
    dbExecErrors.WithLabelValues(dbType, code).Inc()
}

