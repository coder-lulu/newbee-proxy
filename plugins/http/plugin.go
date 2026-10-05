package httpplugin

import (
    "bytes"
    "context"
    "crypto/tls"
    "crypto/sha256"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "net/http"
    "net/url"
    "strings"
    "sync"
    "time"

    "github.com/coder-lulu/newbee-proxy/plugins/common"
    "github.com/coder-lulu/newbee-proxy/internal/metrics"
    utils "github.com/coder-lulu/newbee-proxy/internal/utils"
)

// 简化版 HTTP 插件：实现 ProtocolPlugin 基础能力 + DoRequest 高阶方法
type Plugin struct {
    mu       sync.RWMutex
    running  bool
    client   *http.Client
    tr       *http.Transport
    status   *common.PluginStatus
    metrics  *common.PluginMetrics

    // 配置
    defaultTimeout time.Duration
    maxSnippet     int

    // 控制
    allowedHosts map[string]struct{}
    blockedHosts map[string]struct{}
    // 简易断路器（按域名）
    breakers map[string]*utils.CircuitBreaker

    // 并发限流
    globalSem chan struct{}
    hostSem   map[string]chan struct{}
}

func New() *Plugin {
    return &Plugin{
        status: &common.PluginStatus{
            Name:     "http",
            Version:  "1.0.0",
            Status:   "stopped",
            LoadedAt: time.Now(),
            Config:   map[string]interface{}{},
        },
        metrics: &common.PluginMetrics{},
        defaultTimeout: 30 * time.Second,
        maxSnippet:     4096,
        breakers:       map[string]*utils.CircuitBreaker{},
    }
}

// ProtocolPlugin 实现
func (p *Plugin) Name() string                     { return "http" }
func (p *Plugin) Version() string                  { return "1.0.0" }
func (p *Plugin) SupportedProtocols() []string     { return []string{"http", "https"} }
func (p *Plugin) Description() string              { return "HTTP client plugin for general API requests" }
func (p *Plugin) IsRunning() bool                  { p.mu.RLock(); defer p.mu.RUnlock(); return p.running }
func (p *Plugin) GetStatus() *common.PluginStatus  { p.mu.RLock(); defer p.mu.RUnlock(); s := *p.status; return &s }
func (p *Plugin) GetMetrics() *common.PluginMetrics { p.mu.RLock(); defer p.mu.RUnlock(); m := *p.metrics; return &m }

func (p *Plugin) Initialize(cfg map[string]interface{}) error {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.status.Config = cfg

    // Transport（连接池与 TLS ）
    // 从 cfg 解析若干参数
    maxIdle := getInt(cfg, "max_idle_conns", 200)
    maxPerHost := getInt(cfg, "max_idle_conns_per_host", 20)
    idleTO := getDuration(cfg, "idle_conn_timeout", 90*time.Second)
    defTO := getDuration(cfg, "default_timeout", 30*time.Second)
    insecure := false
    if tlsm, ok := cfg["tls"].(map[string]any); ok {
        insecure, _ = tlsm["insecure_skip_verify"].(bool)
    }
    p.tr = &http.Transport{
        TLSClientConfig:     &tls.Config{InsecureSkipVerify: insecure},
        MaxIdleConns:        maxIdle,
        MaxIdleConnsPerHost: maxPerHost,
        IdleConnTimeout:     idleTO,
    }
    // 代理（简单环境变量支持）
    if pm, ok := cfg["proxy"].(map[string]any); ok {
        if s, _ := pm["http_proxy"].(string); s != "" { _ = os.Setenv("HTTP_PROXY", s) }
        if s, _ := pm["https_proxy"].(string); s != "" { _ = os.Setenv("HTTPS_PROXY", s) }
        if s, _ := pm["no_proxy"].(string); s != "" { _ = os.Setenv("NO_PROXY", s) }
    }
    p.client = &http.Client{Transport: p.tr, Timeout: defTO}
    p.defaultTimeout = defTO
    // 域名白/黑名单
    p.allowedHosts = toSet(getStringSlice(cfg, "allowed_hosts"))
    p.blockedHosts = toSet(getStringSlice(cfg, "blocked_hosts"))
    // 断路器参数
    if cbm, ok := cfg["circuit_breaker"].(map[string]any); ok {
        // 仅存储参数，实例化在首次请求时懒创建
        _ = cbm
    }
    // 并发限流初始化
    if pm, ok := cfg["parallelism"].(map[string]any); ok {
        g := getInt(pm, "global", 0)
        if g > 0 { p.globalSem = make(chan struct{}, g) }
        h := getInt(pm, "per_host", 0)
        if h > 0 { p.hostSem = make(map[string]chan struct{}) }
    }
    p.status.Status = "initialized"
    return nil
}

func (p *Plugin) Start() error {
    p.mu.Lock()
    defer p.mu.Unlock()
    if p.running {
        return fmt.Errorf("http plugin already running")
    }
    now := time.Now(); p.status.StartedAt = &now
    p.running = true
    p.status.Status = "running"
    return nil
}

func (p *Plugin) Stop() error {
    p.mu.Lock(); defer p.mu.Unlock()
    if !p.running { return fmt.Errorf("http plugin not running") }
    p.running = false
    now := time.Now(); p.status.StoppedAt = &now
    p.status.Status = "stopped"
    // Transport 不强制 CloseIdleConnections，这里保持简单
    return nil
}

// 连接相关：HTTP 为无状态，按接口要求实现空实现
func (p *Plugin) CreateConnection(ctx context.Context, target string, credentials *common.Credentials) (common.Connection, error) {
    return nil, common.NewPluginError("http", common.ErrCodeUnsupportedFeature, "connectionless plugin")
}
func (p *Plugin) CloseConnection(_ string) error                       { return nil }
func (p *Plugin) GetConnection(_ string) (common.Connection, bool)     { return nil, false }
func (p *Plugin) ListConnections() []string                            { return nil }
func (p *Plugin) UpdateConfig(cfg map[string]interface{}) error        { p.mu.Lock(); p.status.Config = cfg; p.mu.Unlock(); return nil }

// DoRequest 高阶方法：执行通用 HTTP 请求
func (p *Plugin) DoRequest(ctx context.Context, r *HTTPRequest) (*HTTPResponse, error) {
    if !p.IsRunning() {
        return nil, common.NewPluginError("http", common.ErrCodePluginNotRunning, "plugin not running")
    }
    // 构建 URL（处理 path_params 与 query）
    u := r.URL
    for k, v := range r.PathParams { u = strings.ReplaceAll(u, ":"+k, url.PathEscape(v)) }
    reqURL, perr := url.Parse(u)
    if perr != nil { return nil, perr }
    q := reqURL.Query()
    for k, v := range r.Query { q.Set(k, v) }
    reqURL.RawQuery = q.Encode()

    domain := reqURL.Hostname()
    if p.isBlocked(domain) { return nil, fmt.Errorf("domain blocked: %s", domain) }
    if !p.isAllowed(domain) { return nil, fmt.Errorf("domain not allowed: %s", domain) }

    // 构造 body
    var body io.Reader
    if r.Body != nil {
        switch strings.ToLower(r.BodyType) {
        case "json", "":
            b, jerr := json.Marshal(r.Body)
            if jerr != nil { return nil, jerr }
            body = bytes.NewReader(b)
        case "raw":
            if bs, ok := r.Body.(string); ok { body = strings.NewReader(bs) } else if b, ok := r.Body.([]byte); ok { body = bytes.NewReader(b) } else { return nil, fmt.Errorf("unsupported raw body type") }
        default:
            return nil, fmt.Errorf("unsupported body_type: %s", r.BodyType)
        }
    }

    // 请求对象
    method := strings.ToUpper(r.Method)
    if method == "" { method = http.MethodGet }
    req, nerr := http.NewRequestWithContext(ctx, method, reqURL.String(), body)
    if nerr != nil { return nil, nerr }
    for k, v := range r.Headers { req.Header.Set(k, v) }
    if r.Body != nil && strings.ToLower(r.BodyType) == "json" { req.Header.Set("Content-Type", "application/json") }
    // 鉴权
    if r.Auth != nil {
        switch strings.ToLower(r.Auth.Type) {
        case "basic":
            req.SetBasicAuth(r.Auth.Username, r.Auth.Password)
        case "bearer":
            req.Header.Set("Authorization", "Bearer "+r.Auth.Token)
        case "apikey":
            if strings.ToLower(r.Auth.APIKeyIn) == "query" { q := req.URL.Query(); q.Set(r.Auth.APIKeyName, r.Auth.APIKey); req.URL.RawQuery = q.Encode() } else { req.Header.Set(r.Auth.APIKeyName, r.Auth.APIKey) }
        }
    }

    // 执行
    timeout := r.ParsedTimeout(p.defaultTimeout)
    ctx2, cancel := context.WithTimeout(req.Context(), timeout)
    defer cancel()
    req = req.WithContext(ctx2)

    // 并发限流 acquire
    rel, ok := p.acquire(domain)
    if ok { defer rel() }
    metrics.IncHTTPInflight()
    start := time.Now()

    // 读取重试策略（默认 GET/HEAD/PUT/DELETE 可重试；POST 仅在显式配置时生效）
    maxRetries := 0
    baseDelay := 200 * time.Millisecond
    maxDelay := 2 * time.Second
    retryOn5xx := true
    retryOnNet := true
    retryOnCodes := map[int]struct{}{429: {}}
    if r.Retry != nil {
        maxRetries = getRetryInt(r.Retry, "max", 0)
        baseDelay = getRetryDur(r.Retry, "base_delay", baseDelay)
        maxDelay = getRetryDur(r.Retry, "max_delay", maxDelay)
        retryOn5xx = getRetryBool(r.Retry, "retry_on_5xx", retryOn5xx)
        retryOnNet = getRetryBool(r.Retry, "retry_on_network_error", retryOnNet)
        retryOnCodes = toCodeSet(getRetryCodes(r.Retry, "retry_on_codes"))
    }

    attempt := 0
    var resp *http.Response
    var err error
    backoff := baseDelay
    for {
        // 对非幂等方法，在未开启 Retry 时不进行重试
        if attempt > 0 && strings.ToUpper(r.Method) == http.MethodPost && maxRetries == 0 {
            break
        }
        // 断路器（按域名）
        br := p.getBreaker(domain)
        if br != nil {
            cerr := br.Execute(func() error {
                var derr error
                resp, derr = p.client.Do(req)
                return derr
            })
            if cerr != nil {
                if attempt < maxRetries && retryOnNet {
                    metrics.IncHTTPRetry("circuit_open")
                    time.Sleep(backoff)
                    attempt++
                    backoff *= 2
                    if backoff > maxDelay { backoff = maxDelay }
                    continue
                }
                metrics.DecHTTPInflight()
                metrics.IncHTTPError("circuit_open")
                return nil, cerr
            }
        } else {
            resp, err = p.client.Do(req)
        }
        if err != nil {
            if attempt < maxRetries && retryOnNet {
                metrics.IncHTTPRetry("network_error")
                time.Sleep(backoff)
                attempt++
                backoff *= 2
                if backoff > maxDelay { backoff = maxDelay }
                continue
            }
            metrics.DecHTTPInflight()
            metrics.IncHTTPError("network_error")
            return nil, err
        }
        // 状态码重试判断
        if attempt < maxRetries {
            if resp.StatusCode >= 500 && retryOn5xx {
                metrics.IncHTTPRetry("5xx")
                _ = resp.Body.Close()
                time.Sleep(backoff)
                attempt++
                backoff *= 2
                if backoff > maxDelay { backoff = maxDelay }
                continue
            }
            if _, ok := retryOnCodes[resp.StatusCode]; ok {
                metrics.IncHTTPRetry("code")
                _ = resp.Body.Close()
                time.Sleep(backoff)
                attempt++
                backoff *= 2
                if backoff > maxDelay { backoff = maxDelay }
                continue
            }
        }
        break
    }
    defer resp.Body.Close()
    metrics.DecHTTPInflight()

    out := &HTTPResponse{ Status: resp.StatusCode, Headers: map[string]string{}, DurationMs: time.Since(start).Milliseconds() }
    for k, vals := range resp.Header { if len(vals) > 0 { out.Headers[strings.ToLower(k)] = vals[0] } }

    // 期望状态校验
    if r.Expect != nil && len(r.Expect.StatusIn) > 0 {
        ok := false
        for _, code := range r.Expect.StatusIn { if code == resp.StatusCode { ok = true; break } }
        if !ok { return out, fmt.Errorf("unexpected status: %d", resp.StatusCode) }
    }

    // 读取/保存 Body
    if r.SaveToFile {
        // 流式落盘：task_results/http/YYYY-MM-DD/<ts>-<rand>.bin
        day := time.Now().Format("2006-01-02")
        baseDir := filepath.Join("task_results", "http", day)
        _ = os.MkdirAll(baseDir, 0755)
        fname := fmt.Sprintf("%d.bin", time.Now().UnixNano())
        fpath := filepath.Join(baseDir, fname)
        f, ferr := os.Create(fpath)
        if ferr != nil { return nil, ferr }
        defer f.Close()
        // 同时计算 sha256
        h := sha256.New()
        n, _ := io.Copy(io.MultiWriter(f, h), resp.Body)
        out.BodyFilePath = fpath
        out.SizeBytes = n
        out.BodySHA256 = fmt.Sprintf("%x", h.Sum(nil))
        return out, nil
    }
    // 截断为片段
    b, _ := io.ReadAll(io.LimitReader(resp.Body, int64(p.maxSnippet)))
    out.BodySnippet = string(b)
    out.SizeBytes = int64(len(b))
    return out, nil
}

// ---- helpers ----
func getInt(m map[string]any, k string, def int) int {
    if v, ok := m[k]; ok {
        switch tv := v.(type) {
        case int:
            return tv
        case float64:
            return int(tv)
        }
    }
    return def
}

func getDuration(m map[string]any, k string, def time.Duration) time.Duration {
    if v, ok := m[k]; ok {
        if s, ok2 := v.(string); ok2 {
            if d, err := time.ParseDuration(s); err == nil {
                return d
            }
        }
    }
    return def
}

func getStringSlice(m map[string]any, k string) []string {
    if v, ok := m[k]; ok {
        switch tv := v.(type) {
        case []string:
            return tv
        case []any:
            out := make([]string, 0, len(tv))
            for _, it := range tv { if s, ok := it.(string); ok { out = append(out, s) } }
            return out
        }
    }
    return nil
}

func toSet(arr []string) map[string]struct{} {
    if len(arr) == 0 { return nil }
    s := make(map[string]struct{}, len(arr))
    for _, v := range arr { if v != "" { s[v] = struct{}{} } }
    return s
}

func (p *Plugin) isBlocked(host string) bool {
    if len(p.blockedHosts) == 0 || host == "" { return false }
    _, ok := p.blockedHosts[host]
    return ok
}

func (p *Plugin) isAllowed(host string) bool {
    if len(p.allowedHosts) == 0 || host == "" { return true }
    _, ok := p.allowedHosts[host]
    return ok
}

// getBreaker 返回域名断路器（懒加载）
func (p *Plugin) getBreaker(domain string) *utils.CircuitBreaker {
    if domain == "" { return nil }
    p.mu.Lock()
    defer p.mu.Unlock()
    if b, ok := p.breakers[domain]; ok { return b }
    // 默认关闭：仅当配置显式启用时再创建（FailureLimit>0 且 Timeout>0）
    cbm, _ := p.status.Config["circuit_breaker"].(map[string]any)
    fl := getInt(cbm, "failure_limit", 0)
    to := getDuration(cbm, "timeout", 0)
    if fl <= 0 || to <= 0 { return nil }
    b := utils.NewCircuitBreaker(fl, to)
    p.breakers[domain] = b
    return b
}

// verifyPinnedCerts 校验证书指纹（预留，当前仅示意）
func verifyPinnedCerts(state tls.ConnectionState, pins []string) bool {
    if len(pins) == 0 { return true }
    if len(state.PeerCertificates) == 0 { return false }
    cert := state.PeerCertificates[0]
    sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
    fp := fmt.Sprintf("%x", sum[:])
    for _, p := range pins { if strings.EqualFold(p, fp) { return true } }
    return false
}

// acquire 获取并发配额（全局与 per-host）。返回释放函数与是否限流启用。
func (p *Plugin) acquire(host string) (func(), bool) {
    var g, h bool
    if p.globalSem != nil { p.globalSem <- struct{}{}; g = true }
    var hs chan struct{}
    if p.hostSem != nil && host != "" {
        p.mu.Lock()
        if s, ok := p.hostSem[host]; ok {
            hs = s
        } else {
            // 默认容量取并发配置的 per_host 值
            cap := getInt(p.status.Config["parallelism"].(map[string]any), "per_host", 0)
            if cap > 0 { s = make(chan struct{}, cap) }
            p.hostSem[host] = s
            hs = s
        }
        p.mu.Unlock()
        if hs != nil { hs <- struct{}{}; h = true }
    }
    if !g && !h { return func(){}, false }
    return func(){ if h { <-hs }; if g { <-p.globalSem } }, true
}

// --- retry helpers ---
func getRetryInt(r *HTTPRetry, k string, def int) int {
    switch k {
    case "max":
        if r.Max > 0 { return r.Max }
    }
    return def
}
func getRetryDur(r *HTTPRetry, k string, def time.Duration) time.Duration {
    var s string
    switch k {
    case "base_delay": s = r.BaseDelay
    case "max_delay": s = r.MaxDelay
    }
    if s == "" { return def }
    if d, err := time.ParseDuration(s); err == nil { return d }
    return def
}
func getRetryBool(r *HTTPRetry, k string, def bool) bool {
    switch k {
    case "retry_on_5xx": return orBool(r.RetryOn5xx, def)
    case "retry_on_network_error": return orBool(r.RetryOnNetworkError, def)
    }
    return def
}
func getRetryCodes(r *HTTPRetry, k string) []int {
    if k == "retry_on_codes" { return r.RetryOnCodes }
    return nil
}
func toCodeSet(arr []int) map[int]struct{} {
    m := make(map[int]struct{}, len(arr))
    for _, c := range arr { m[c] = struct{}{} }
    return m
}
func orBool(v, def bool) bool { if v { return true }; return def }
