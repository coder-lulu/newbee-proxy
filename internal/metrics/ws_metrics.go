package metrics

import (
    "sync/atomic"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/collectors"
)

// 简易 WS 指标：活跃连接、收发字节、丢弃计数
var (
    wsActiveSSH    int64
    wsActiveTelnet int64
    wsSentBytes    int64
    wsRecvBytes    int64
    wsSendDrops    int64

    // Prometheus 指标
    wsActiveGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
        Namespace: "worker",
        Subsystem: "ws",
        Name:      "active_connections",
        Help:      "Number of active WebSocket connections",
    }, []string{"protocol"})

    wsBytesCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
        Namespace: "worker",
        Subsystem: "ws",
        Name:      "bytes_total",
        Help:      "Total bytes sent/received over WebSocket",
    }, []string{"direction"})

    wsDropCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker",
        Subsystem: "ws",
        Name:      "send_queue_drop_total",
        Help:      "Total number of dropped messages due to bounded send queue",
    })

    // 每协议 RTT 直方图（通过 ping/pong 估算）
    wsRTTHist = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Namespace: "worker",
        Subsystem: "ws",
        Name:      "ping_rtt_seconds",
        Help:      "Observed WebSocket ping round-trip time in seconds",
        Buckets:   prometheus.ExponentialBuckets(0.01, 2, 12), // 10ms .. ~40s
    }, []string{"protocol"})

    // 错误计数（read/write/other）
    wsErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
        Namespace: "worker",
        Subsystem: "ws",
        Name:      "errors_total",
        Help:      "Total WebSocket errors by protocol and kind",
    }, []string{"protocol", "kind"})
)

// RegisterPrometheus 仅在进程启动时调用一次
func RegisterPrometheus() {
    // Register 忽略重复注册错误
    _ = prometheus.Register(wsActiveGauge)
    _ = prometheus.Register(wsBytesCounter)
    _ = prometheus.Register(wsDropCounter)
    _ = prometheus.Register(wsRTTHist)
    _ = prometheus.Register(wsErrorCounter)
    RegisterRuntimeMetrics()
    RegisterTaskMetrics()
    RegisterSNMPMetrics()
    RegisterDBMetrics()
    // 注册 Go runtime/process 指标
    _ = prometheus.Register(collectors.NewGoCollector())
    _ = prometheus.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

func IncWSConnections(proto string) {
    switch proto {
    case "ssh":
        atomic.AddInt64(&wsActiveSSH, 1)
        wsActiveGauge.WithLabelValues("ssh").Inc()
    case "telnet":
        atomic.AddInt64(&wsActiveTelnet, 1)
        wsActiveGauge.WithLabelValues("telnet").Inc()
    default:
        wsActiveGauge.WithLabelValues(proto).Inc()
    }
}

func DecWSConnections(proto string) {
    switch proto {
    case "ssh":
        atomic.AddInt64(&wsActiveSSH, -1)
        wsActiveGauge.WithLabelValues("ssh").Dec()
    case "telnet":
        atomic.AddInt64(&wsActiveTelnet, -1)
        wsActiveGauge.WithLabelValues("telnet").Dec()
    default:
        wsActiveGauge.WithLabelValues(proto).Dec()
    }
}

func AddWSSentBytes(_ string, n int) { // proto 预留
    atomic.AddInt64(&wsSentBytes, int64(n))
    wsBytesCounter.WithLabelValues("sent").Add(float64(n))
}

func AddWSRecvBytes(_ string, n int) {
    atomic.AddInt64(&wsRecvBytes, int64(n))
    wsBytesCounter.WithLabelValues("recv").Add(float64(n))
}

func IncWSSendQueueDrop(_ string) {
    atomic.AddInt64(&wsSendDrops, 1)
    wsDropCounter.Inc()
}

// 导出当前快照（可用于 JSON 快照）
func GetWSMetrics() map[string]int64 {
    return map[string]int64{
        "ws_active_ssh":    atomic.LoadInt64(&wsActiveSSH),
        "ws_active_telnet": atomic.LoadInt64(&wsActiveTelnet),
        "ws_sent_bytes":    atomic.LoadInt64(&wsSentBytes),
        "ws_recv_bytes":    atomic.LoadInt64(&wsRecvBytes),
        "ws_sendq_drops":   atomic.LoadInt64(&wsSendDrops),
    }
}

// RTT 与错误指标更新 API
func ObserveWSPingRTT(proto string, seconds float64) { wsRTTHist.WithLabelValues(proto).Observe(seconds) }
func IncWSError(proto, kind string)                   { wsErrorCounter.WithLabelValues(proto, kind).Inc() }
