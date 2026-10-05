package portfwd

import (
    "context"
    "errors"
    "fmt"
    "io"
    "net"
    "sync"
    "sync/atomic"
    "time"

    "github.com/coder-lulu/newbee-proxy/plugins/common"
    "github.com/coder-lulu/newbee-proxy/internal/metrics"
)

// Plugin 实现简单的 TCP 端口转发
type Plugin struct {
    mu     sync.RWMutex
    rules  map[string]*ruleRuntime
    status *common.PluginStatus
    metr   *common.PluginMetrics
}

type ruleRuntime struct {
    rule       *ForwardRule
    listener   net.Listener
    stopCh     chan struct{}
    wg         sync.WaitGroup
    activeConns int32
    maxConns    int
}

func New() *Plugin {
    return &Plugin{
        rules: make(map[string]*ruleRuntime),
        status: &common.PluginStatus{ Name: "portfwd", Version: "0.1.0", Status: "running", LoadedAt: time.Now(), Config: map[string]any{}},
        metr:   &common.PluginMetrics{},
    }
}

// --- ProtocolPlugin 基本信息 ---
func (p *Plugin) Name() string { return "portfwd" }
func (p *Plugin) Version() string { return "0.1.0" }
func (p *Plugin) SupportedProtocols() []string { return []string{"tcp-forward"} }
func (p *Plugin) Description() string { return "TCP port forward plugin" }
func (p *Plugin) Initialize(cfg map[string]any) error { p.status.Config = cfg; return nil }
func (p *Plugin) Start() error  { return nil }
func (p *Plugin) Stop() error   { p.mu.Lock(); defer p.mu.Unlock(); for _, rt := range p.rules { _ = rt.close() }; return nil }
func (p *Plugin) IsRunning() bool { return true }
func (p *Plugin) GetStatus() *common.PluginStatus { s := *p.status; return &s }
func (p *Plugin) GetMetrics() *common.PluginMetrics { m := *p.metr; return &m }
func (p *Plugin) UpdateConfig(map[string]any) error { return nil }

// 无连接语义
func (p *Plugin) CreateConnection(context.Context, string, *common.Credentials) (common.Connection, error) { return nil, common.NewPluginError("portfwd", common.ErrCodeUnsupportedFeature, "connectionless plugin") }
func (p *Plugin) CloseConnection(string) error { return nil }
func (p *Plugin) GetConnection(string) (common.Connection, bool) { return nil, false }
func (p *Plugin) ListConnections() []string { return nil }

// --- 规则管理 API ---
func (p *Plugin) CreateRule(r *ForwardRule) error {
    if r == nil || r.ID == "" || r.Listen == "" || r.Target == "" { return fmt.Errorf("invalid rule") }
    r.Protocol = "tcp"
    r.CreatedAt = time.Now()
    p.mu.Lock(); defer p.mu.Unlock()
    if _, ok := p.rules[r.ID]; ok { return fmt.Errorf("rule exists") }
    rt := &ruleRuntime{ rule: r, stopCh: make(chan struct{}), maxConns: r.MaxConns }
    p.rules[r.ID] = rt
    metrics.SetPFActiveRules(len(p.rules))
    return nil
}

func (p *Plugin) StartRule(id string) error {
    p.mu.Lock(); rt, ok := p.rules[id]; p.mu.Unlock(); if !ok { return fmt.Errorf("rule not found") }
    if rt.listener != nil { return fmt.Errorf("rule already running") }
    ln, err := net.Listen("tcp", rt.rule.Listen)
    if err != nil { return err }
    rt.listener = ln
    rt.rule.Running = true
    rt.wg.Add(1)
    go p.acceptLoop(rt)
    return nil
}

func (p *Plugin) StopRule(id string) error {
    p.mu.Lock(); rt, ok := p.rules[id]; p.mu.Unlock(); if !ok { return fmt.Errorf("rule not found") }
    return rt.close()
}

func (p *Plugin) DeleteRule(id string) error {
    _ = p.StopRule(id)
    p.mu.Lock(); delete(p.rules, id); p.mu.Unlock()
    p.mu.RLock(); metrics.SetPFActiveRules(len(p.rules)); p.mu.RUnlock()
    return nil
}

func (p *Plugin) ListRules() []*ForwardRule {
    p.mu.RLock(); defer p.mu.RUnlock()
    out := make([]*ForwardRule, 0, len(p.rules))
    for _, rt := range p.rules { r := *rt.rule; r.ActiveConns = int(atomic.LoadInt32(&rt.activeConns)); out = append(out, &r) }
    return out
}

func (p *Plugin) acceptLoop(rt *ruleRuntime) {
    defer rt.wg.Done()
    for {
        conn, err := rt.listener.Accept()
        if err != nil {
            if errors.Is(err, net.ErrClosed) { return }
            metrics.IncPFAcceptErr()
            continue
        }
        // 最大连接数限制
        if rt.maxConns > 0 && int(atomic.LoadInt32(&rt.activeConns)) >= rt.maxConns {
            _ = conn.Close()
            continue
        }
        atomic.AddInt32(&rt.activeConns, 1)
        metrics.SetPFActiveConns(int(atomic.LoadInt32(&rt.activeConns)))
        rt.wg.Add(1)
        go func(c net.Conn){ defer rt.wg.Done(); p.handleConn(rt, c) }(conn)
    }
}

func (p *Plugin) handleConn(rt *ruleRuntime, c net.Conn) {
    defer func(){ _ = c.Close(); atomic.AddInt32(&rt.activeConns, -1); metrics.SetPFActiveConns(int(atomic.LoadInt32(&rt.activeConns))) }()
    dtimeout := 5 * time.Second
    if rt.rule.DialTimeout != "" { if d, err := time.ParseDuration(rt.rule.DialTimeout); err == nil { dtimeout = d } }
    upstream, err := net.DialTimeout("tcp", rt.rule.Target, dtimeout)
    if err != nil { return }
    defer upstream.Close()

    // 可选 nodelay/keepalive
    if tc, ok := c.(*net.TCPConn); ok {
        _ = tc.SetNoDelay(rt.rule.NoDelay)
        if rt.rule.KeepAliveSec > 0 { _ = tc.SetKeepAlive(true); _ = tc.SetKeepAlivePeriod(time.Duration(rt.rule.KeepAliveSec) * time.Second) }
    }
    if tu, ok := upstream.(*net.TCPConn); ok {
        _ = tu.SetNoDelay(rt.rule.NoDelay)
        if rt.rule.KeepAliveSec > 0 { _ = tu.SetKeepAlive(true); _ = tu.SetKeepAlivePeriod(time.Duration(rt.rule.KeepAliveSec) * time.Second) }
    }

    // Idle 超时：周期刷新 deadline，避免长时间空闲挂起
    var stopDeadlines chan struct{}
    if rt.rule.IdleTimeout != "" {
        if idle, err := time.ParseDuration(rt.rule.IdleTimeout); err == nil && idle > 0 {
            // 先设置一次初始 deadline
            _ = c.SetDeadline(time.Now().Add(idle))
            _ = upstream.SetDeadline(time.Now().Add(idle))
            stopDeadlines = make(chan struct{})
            go refreshDeadlines(c, upstream, idle, stopDeadlines)
            defer close(stopDeadlines)
        }
    }

    // 双向复制
    done := make(chan struct{}, 2)
    go splice(upstream, c, done)
    go splice(c, upstream, done)
    <-done; <-done
}

// refreshDeadlines 周期性刷新双方连接的 deadline，直到 stop 关闭
func refreshDeadlines(a, b net.Conn, idle time.Duration, stop <-chan struct{}) {
    // 刷新频率：idle 的一半，最少 1s，最多 60s
    tick := idle / 2
    if tick < time.Second { tick = time.Second }
    if tick > 60*time.Second { tick = 60 * time.Second }
    t := time.NewTicker(tick)
    defer t.Stop()
    for {
        select {
        case <-stop:
            return
        case <-t.C:
            dl := time.Now().Add(idle)
            _ = a.SetDeadline(dl)
            _ = b.SetDeadline(dl)
        }
    }
}

func splice(dst, src net.Conn, done chan<- struct{}) {
    defer func(){ done <- struct{}{} }()
    _ , _ = io.Copy(dst, src)
    // 半关闭写入端，避免另一方向被阻塞
    if tc, ok := dst.(*net.TCPConn); ok { _ = tc.CloseWrite() }
}

func (rt *ruleRuntime) close() error {
    if rt.listener != nil { _ = rt.listener.Close() }
    close(rt.stopCh)
    rt.wg.Wait()
    rt.rule.Running = false
    return nil
}
