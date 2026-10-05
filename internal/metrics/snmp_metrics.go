package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
    snmpReqs = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "worker", Subsystem: "snmp", Name: "requests_total", Help: "SNMP requests"}, []string{"op", "version"})
    snmpDur  = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "worker", Subsystem: "snmp", Name: "duration_seconds", Help: "SNMP request duration", Buckets: prometheus.ExponentialBuckets(0.01, 2, 12)}, []string{"op"})
    snmpErrs = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "worker", Subsystem: "snmp", Name: "errors_total", Help: "SNMP errors"}, []string{"op", "reason"})
)

func RegisterSNMPMetrics() { _ = prometheus.Register(snmpReqs); _ = prometheus.Register(snmpDur); _ = prometheus.Register(snmpErrs) }
func IncSNMPReq(op, version string)        { snmpReqs.WithLabelValues(op, version).Inc() }
func ObserveSNMPDur(op string, sec float64){ snmpDur.WithLabelValues(op).Observe(sec) }
func IncSNMPError(op, reason string)       { snmpErrs.WithLabelValues(op, reason).Inc() }

