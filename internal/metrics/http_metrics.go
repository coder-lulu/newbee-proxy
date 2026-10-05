package metrics

import (
    "fmt"
    "github.com/prometheus/client_golang/prometheus"
)

var (
    httpReqs = prometheus.NewCounterVec(prometheus.CounterOpts{
        Namespace: "worker",
        Subsystem: "http",
        Name:      "requests_total",
        Help:      "Total HTTP requests",
    }, []string{"method", "code", "domain"})

    httpDur = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Namespace: "worker",
        Subsystem: "http",
        Name:      "request_duration_seconds",
        Help:      "HTTP request duration in seconds",
        Buckets:   prometheus.ExponentialBuckets(0.01, 2, 12),
    }, []string{"method", "domain"})

    httpErrs = prometheus.NewCounterVec(prometheus.CounterOpts{
        Namespace: "worker",
        Subsystem: "http",
        Name:      "errors_total",
        Help:      "HTTP errors by reason",
    }, []string{"reason"})

    httpInflight = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: "worker",
        Subsystem: "http",
        Name:      "inflight",
        Help:      "Number of in-flight HTTP requests",
    })

    httpRetries = prometheus.NewCounterVec(prometheus.CounterOpts{
        Namespace: "worker",
        Subsystem: "http",
        Name:      "retries_total",
        Help:      "Total HTTP retries by reason",
    }, []string{"reason"})
)

func RegisterHTTPMetrics() {
    _ = prometheus.Register(httpReqs)
    _ = prometheus.Register(httpDur)
    _ = prometheus.Register(httpErrs)
    _ = prometheus.Register(httpInflight)
    _ = prometheus.Register(httpRetries)
}

func ObserveHTTPRequest(method, domain string, code int, seconds float64) {
    httpReqs.WithLabelValues(method, toCode(code), domain).Inc()
    httpDur.WithLabelValues(method, domain).Observe(seconds)
}

func IncHTTPError(reason string) { httpErrs.WithLabelValues(reason).Inc() }
func IncHTTPRetry(reason string) { httpRetries.WithLabelValues(reason).Inc() }

func toCode(code int) string {
    // 复用 prometheus 的常见做法：把状态码转为字符串标签
    return fmt.Sprintf("%d", code)
}

func IncHTTPInflight() { httpInflight.Inc() }
func DecHTTPInflight() { httpInflight.Dec() }
