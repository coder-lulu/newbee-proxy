package handlers

import (
    "context"
    "crypto/tls"
    "encoding/json"
    "fmt"
    "net"
    "net/http"
    "os"
    "strconv"
    "time"

    "golang.org/x/net/icmp"
    "golang.org/x/net/ipv4"

    "github.com/coder-lulu/newbee-proxy/internal/svc"
)

type DiagHandler struct{ svc *svc.ServiceContext }

func NewDiagHandler(s *svc.ServiceContext) *DiagHandler { return &DiagHandler{svc: s} }

// GET /api/diag/tcp?host=1.2.3.4&port=22&timeout_ms=3000
func (h *DiagHandler) TCP(w http.ResponseWriter, r *http.Request) {
    q := r.URL.Query()
    host := q.Get("host")
    port := q.Get("port")
    if host == "" || port == "" { http.Error(w, "missing host/port", 400); return }
    timeout := parseMs(q.Get("timeout_ms"), 3000)
    addr := net.JoinHostPort(host, port)
    start := time.Now()
    conn, err := net.DialTimeout("tcp", addr, time.Duration(timeout)*time.Millisecond)
    dur := time.Since(start)
    if err == nil { _ = conn.Close() }
    out := map[string]any{"ok": err == nil, "rtt_ms": dur.Milliseconds(), "error": errString(err)}
    _ = json.NewEncoder(w).Encode(out)
}

// GET /api/diag/dns?name=example.com&server=8.8.8.8:53&timeout_ms=2000
func (h *DiagHandler) DNS(w http.ResponseWriter, r *http.Request) {
    q := r.URL.Query()
    name := q.Get("name")
    if name == "" { http.Error(w, "missing name", 400); return }
    timeout := parseMs(q.Get("timeout_ms"), 2000)
    server := q.Get("server")
    resolver := net.Resolver{}
    if server != "" {
        d := &net.Dialer{Timeout: time.Duration(timeout)*time.Millisecond}
        resolver = net.Resolver{ PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) { return d.DialContext(ctx, "udp", server) } }
    }
    ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeout)*time.Millisecond)
    defer cancel()
    start := time.Now()
    addrs, err := resolver.LookupIPAddr(ctx, name)
    dur := time.Since(start)
    ips := make([]string, 0, len(addrs))
    for _, a := range addrs { ips = append(ips, a.IP.String()) }
    out := map[string]any{"ok": err == nil, "rtt_ms": dur.Milliseconds(), "count": len(ips), "ips": ips, "error": errString(err)}
    _ = json.NewEncoder(w).Encode(out)
}

// GET /api/diag/tls?host=example.com&port=443
func (h *DiagHandler) TLS(w http.ResponseWriter, r *http.Request) {
    q := r.URL.Query()
    host := q.Get("host")
    if host == "" { http.Error(w, "missing host", 400); return }
    port := q.Get("port")
    if port == "" { port = "443" }
    addr := net.JoinHostPort(host, port)
    cfg := &tls.Config{ServerName: host}
    start := time.Now()
    conn, err := tls.Dial("tcp", addr, cfg)
    dur := time.Since(start)
    if err != nil { _ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()}); return }
    defer conn.Close()
    state := conn.ConnectionState()
    ok := len(state.PeerCertificates) > 0
    var subj, issuer string
    var notAfter time.Time
    if ok {
        cert := state.PeerCertificates[0]
        subj = cert.Subject.String()
        issuer = cert.Issuer.String()
        notAfter = cert.NotAfter
    }
    days := int(time.Until(notAfter).Hours() / 24)
    out := map[string]any{
        "ok": ok,
        "handshake_ms": dur.Milliseconds(),
        "subject": subj,
        "issuer": issuer,
        "not_after": notAfter.Unix(),
        "days_to_expiry": days,
        "negotiated_protocol": state.NegotiatedProtocol,
        "version": tlsVersionString(state.Version),
        "cipher_suite": tls.CipherSuiteName(state.CipherSuite),
    }
    _ = json.NewEncoder(w).Encode(out)
}

// GET /api/diag/icmp?host=1.2.3.4&count=3&timeout_ms=3000
func (h *DiagHandler) ICMP(w http.ResponseWriter, r *http.Request) {
    q := r.URL.Query()
    host := q.Get("host")
    if host == "" { http.Error(w, "missing host", 400); return }
    count := parseInt(q.Get("count"), 3)
    timeout := parseMs(q.Get("timeout_ms"), 3000)
    res := icmpPing(host, count, time.Duration(timeout)*time.Millisecond)
    _ = json.NewEncoder(w).Encode(res)
}

// GET /api/diag/snmp?target=1.2.3.4&version=v2c&community=public&timeout_ms=2000
// 注：SNMP 为独立插件 API（/api/snmp/*），不在 diag 内聚合。

// ----- helpers -----
func parseMs(s string, def int) int { if n, err := strconv.Atoi(s); err == nil && n > 0 { return n }; return def }
func parseInt(s string, def int) int { if n, err := strconv.Atoi(s); err == nil { return n }; return def }
func errString(err error) string { if err != nil { return err.Error() }; return "" }

func tlsVersionString(v uint16) string {
    switch v {
    case tls.VersionTLS10: return "TLS1.0"
    case tls.VersionTLS11: return "TLS1.1"
    case tls.VersionTLS12: return "TLS1.2"
    case tls.VersionTLS13: return "TLS1.3"
    default: return fmt.Sprintf("0x%x", v)
    }
}

type icmpStats struct {
    Sent int `json:"sent"`
    Received int `json:"received"`
    Loss float64 `json:"loss_percent"`
    MinMs int64 `json:"min_ms"`
    AvgMs int64 `json:"avg_ms"`
    MaxMs int64 `json:"max_ms"`
    PrivilegedRequired bool `json:"privileged_required"`
    Error string `json:"error,omitempty"`
}

func icmpPing(host string, count int, timeout time.Duration) icmpStats {
    stats := icmpStats{Sent: count}
    // Resolve to IPv4
    ip, err := net.ResolveIPAddr("ip4", host)
    if err != nil { stats.Error = err.Error(); return stats }
    c, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
    if err != nil {
        stats.Error = err.Error()
        stats.PrivilegedRequired = true
        return stats
    }
    defer c.Close()
    var min, max time.Duration
    var sum time.Duration
    pid := os.Getpid() & 0xffff
    for i := 0; i < count; i++ {
        m := icmp.Message{Type: ipv4.ICMPTypeEcho, Code: 0, Body: &icmp.Echo{ID: pid, Seq: i + 1, Data: []byte("NB")}}
        b, _ := m.Marshal(nil)
        start := time.Now()
        _, err := c.WriteTo(b, &net.IPAddr{IP: ip.IP})
        if err != nil { continue }
        _ = c.SetReadDeadline(time.Now().Add(timeout))
        rb := make([]byte, 1500)
        n, _, rerr := c.ReadFrom(rb)
        if rerr != nil { continue }
        rm, _ := icmp.ParseMessage(1, rb[:n])
        if rm.Type == ipv4.ICMPTypeEchoReply {
            rtt := time.Since(start)
            stats.Received++
            if stats.Received == 1 || rtt < min { min = rtt }
            if rtt > max { max = rtt }
            sum += rtt
        }
        time.Sleep(200 * time.Millisecond)
    }
    if stats.Received > 0 {
        stats.MinMs = min.Milliseconds()
        stats.MaxMs = max.Milliseconds()
        stats.AvgMs = (sum / time.Duration(stats.Received)).Milliseconds()
    }
    stats.Loss = float64(stats.Sent-stats.Received) * 100 / float64(stats.Sent)
    return stats
}
