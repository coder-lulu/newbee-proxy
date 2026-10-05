package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
    pfActiveRules = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: "worker", Subsystem: "portfwd", Name: "active_rules", Help: "Active port forward rules",
    })
    pfActiveConns = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: "worker", Subsystem: "portfwd", Name: "active_conns", Help: "Active port forward connections",
    })
    pfAcceptErrs = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "portfwd", Name: "accept_errors_total", Help: "Accept errors",
    })
)

func RegisterPortFwdMetrics() { _ = prometheus.Register(pfActiveRules); _ = prometheus.Register(pfActiveConns); _ = prometheus.Register(pfAcceptErrs) }
func SetPFActiveRules(n int)  { pfActiveRules.Set(float64(n)) }
func SetPFActiveConns(n int)  { pfActiveConns.Set(float64(n)) }
func IncPFAcceptErr()         { pfAcceptErrs.Inc() }

