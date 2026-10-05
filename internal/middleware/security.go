package middleware

import (
    "context"
    "net"
    "net/http"
    "strings"
    "sync"
    "time"

    "github.com/coder-lulu/newbee-proxy/internal/config"
    "github.com/coder-lulu/newbee-proxy/internal/svc"

    jwt "github.com/golang-jwt/jwt/v4"
    "github.com/zeromicro/go-zero/core/logx"
    "github.com/zeromicro/go-zero/rest"
)

// ctxKey 用于在请求上下文中携带已验证的JWT Claims
type ctxKey string

const claimsCtxKey ctxKey = "security.jwt.claims"

var (
    // 默认受保护的前缀（WS 握手与相关通道）
    defaultProtectedPrefixes = []string{
        "/api/rdp/websocket",
        "/api/vnc/websocket",
        "/api/telnet/websocket",
        "/api/ssh/websocket",
        "/ws/ssh",
        "/ws/telnet",
        "/api/db/websocket",
        // 默认将任务/DB/流式任务纳入保护（HTTP 模式）
        "/api/task/",
        "/api/db/",
        "/api/http/",
        "/api/portfwd/",
        "/api/snmp/",
        "/api/diag/",
        "/api/stream/task",
    }

    limiterMu sync.Mutex
    limiters  = make(map[string]*svc.RateLimiter)
)

// RegisterSecurity 将安全相关中间件注册到 server
func RegisterSecurity(server *rest.Server, svcCtx *svc.ServiceContext) {
    // Draining/Online/Offline 状态守卫（统一入口限制新建会话/任务）
    server.Use(drainingGuardMiddleware(svcCtx))

    // Origin 白名单中间件
    server.Use(originWhitelistMiddleware(svcCtx.Config.Security))

    // 速率限制中间件（HTTP/WS 握手维度）
    server.Use(rateLimitMiddleware(svcCtx))

    // 统一鉴权：JWT 或 PSK（仅对受保护路径生效）
    server.Use(jwtOrPskAuthMiddleware(svcCtx))
}

// drainingGuardMiddleware 在 draining/offline 状态下对大多数入口返回 503，带 Retry-After，
// 但允许健康检查/指标/pprof 等只读接口通过。
func drainingGuardMiddleware(svcCtx *svc.ServiceContext) rest.Middleware {
    // 允许在维护期仍可访问的路径
    alwaysAllowed := []string{
        "/health",
        "/status",
        "/metrics",
        "/metrics.json",
    }
    allowedPrefixes := []string{
        "/debug/pprof/",
    }
    isAllowed := func(p string) bool {
        for _, a := range alwaysAllowed { if p == a { return true } }
        for _, pre := range allowedPrefixes { if strings.HasPrefix(p, pre) { return true } }
        return false
    }
    return func(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            if svcCtx == nil || svcCtx.IsAcceptingNew() || isAllowed(r.URL.Path) {
                next(w, r)
                return
            }
            // draining/offline：拒绝新建会话/任务
            w.Header().Set("Retry-After", "30")
            http.Error(w, "Service Unavailable: draining", http.StatusServiceUnavailable)
        }
    }
}

// originWhitelistMiddleware 校验 Origin 是否在白名单内（无配置则放行）
func originWhitelistMiddleware(sec config.SecurityConf) rest.Middleware {
    wl := map[string]struct{}{}
    for _, o := range sec.OriginWhitelist {
        if o == "" {
            continue
        }
        wl[strings.TrimSpace(o)] = struct{}{}
    }
    return func(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            origin := r.Header.Get("Origin")
            if origin == "" || len(wl) == 0 {
                next(w, r)
                return
            }
            if _, ok := wl[origin]; ok {
                next(w, r)
                return
            }
            // 观察模式：若 JWT 配置为非强制，则仅记录；否则拒绝
            enforce := false
            if sec.JWT != nil {
                enforce = sec.JWT.Enforce
            }
            if !enforce {
                logx.WithContext(r.Context()).Infof("Origin not in whitelist (observe): %s", origin)
                next(w, r)
                return
            }
            http.Error(w, "Forbidden: origin not allowed", http.StatusForbidden)
        }
    }
}

// rateLimitMiddleware 简易速率限制（按租户/用户/来源IP 维度）
func rateLimitMiddleware(svcCtx *svc.ServiceContext) rest.Middleware {
    conf := svcCtx.Config.Security.RateLimit
    if conf == nil || !conf.Enabled {
        return func(next http.HandlerFunc) http.HandlerFunc { return next }
    }
    rps := conf.RequestsPerSecond
    if rps <= 0 {
        rps = 50
    }
    return func(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            key := composeLimitKey(r)
            limiter := getOrCreateLimiter(key, rps)
            if !limiter.Allow() {
                http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
                return
            }
            next(w, r)
        }
    }
}

func getOrCreateLimiter(key string, rps int) *svc.RateLimiter {
    limiterMu.Lock()
    defer limiterMu.Unlock()
    if rl, ok := limiters[key]; ok {
        return rl
    }
    rl := svc.NewRateLimiter(rps)
    limiters[key] = rl
    return rl
}

func composeLimitKey(r *http.Request) string {
    // 优先从已验证的 claims 获取租户/用户标识；否则回退到来源IP
    if v := r.Context().Value(claimsCtxKey); v != nil {
        if claims, ok := v.(jwt.MapClaims); ok {
            if tid, ok2 := claims["tenantId"].(string); ok2 && tid != "" {
                return "tenant:" + tid
            }
            if uid, ok2 := claims["userId"].(string); ok2 && uid != "" {
                return "user:" + uid
            }
        }
    }
    host, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil || host == "" {
        return "ip:unknown"
    }
    return "ip:" + host
}

// jwtAuthMiddleware 针对受保护路径执行 JWT 校验
func jwtOrPskAuthMiddleware(svcCtx *svc.ServiceContext) rest.Middleware {
    sec := svcCtx.Config.Security
    jwtEnabled := sec.JWT != nil && sec.JWT.Enabled && sec.JWT.Secret != ""
    // 是否强制鉴权：优先使用 JWT.Enforce；若未启用 JWT 且配置了 PSK，则默认强制 PSK
    enforce := false
    if sec.JWT != nil && sec.JWT.Enabled {
        enforce = sec.JWT.Enforce
    } else if svcCtx.Config.OpsCenter.PSK != "" {
        enforce = true
    }

    // 安全默认：默认所有路径受保护，仅允许少数观测路径；提供 SkipPaths 进行显式跳过
    protected := defaultProtectedPrefixes
    // 若配置了 SkipPaths，则把这些前缀从保护中移除（剩余均需要认证）
    skip := map[string]struct{}{}
    for _, p := range sec.SkipPaths { if p != "" { skip[p] = struct{}{} } }

    var secret []byte
    allowQuery := false
    if jwtEnabled {
        secret = []byte(sec.JWT.Secret)
        allowQuery = sec.JWT.AllowQueryToken
    }
    psk := svcCtx.Config.OpsCenter.PSK

    return func(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            // 是否跳过：匹配 SkipPaths
            if isPrefixedByAny(r.URL.Path, skip) {
                next(w, r)
                return
            }
            // 非观测路径默认受保护
            if !isProtectedPath(r.URL.Path, protected) {
                next(w, r)
                return
            }

            // 1) JWT 校验（若启用）
            if jwtEnabled {
                if tok := extractToken(r, allowQuery); tok != "" {
                    if parsed, err := jwt.Parse(tok, func(t *jwt.Token) (interface{}, error) {
                        if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
                            return nil, jwt.ErrSignatureInvalid
                        }
                        return secret, nil
                    }); err == nil && parsed.Valid {
                        if claims, ok := parsed.Claims.(jwt.MapClaims); ok {
                            if validateExp(claims) {
                                // 注入 claims 并放行
                                r = r.WithContext(context.WithValue(r.Context(), claimsCtxKey, claims))
                                next(w, r)
                                return
                            }
                            // exp 无效：继续走 PSK 回退
                            logx.WithContext(r.Context()).Info("JWT expired (try PSK)")
                        } else {
                            logx.WithContext(r.Context()).Info("JWT claims type mismatch (try PSK)")
                        }
                    } else if err != nil {
                        logx.WithContext(r.Context()).Infof("JWT parse invalid (try PSK): %v", err)
                    }
                }
            }

            // 2) PSK 回退
            if psk != "" && checkPSKHeaderOrQuery(r, psk) {
                next(w, r)
                return
            }

            if enforce {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            logx.WithContext(r.Context()).Info("Auth missing/invalid (observe)")
            next(w, r)
        }
    }
}

func checkPSKHeaderOrQuery(r *http.Request, want string) bool {
    if want == "" { return false }
    if got := r.Header.Get("X-PSK"); got != "" && got == want { return true }
    if got := r.Header.Get("X-OPS-PSK"); got != "" && got == want { return true }
    if got := r.URL.Query().Get("psk"); got != "" && got == want { return true }
    return false
}

func isProtectedPath(path string, protected []string) bool {
    for _, p := range protected {
        if strings.HasPrefix(path, p) {
            return true
        }
    }
    return false
}

// isPrefixedByAny 判断 path 是否以 skip 集合中任一前缀开头
func isPrefixedByAny(path string, skip map[string]struct{}) bool {
    if len(skip) == 0 { return false }
    for p := range skip {
        if strings.HasPrefix(path, p) { return true }
    }
    return false
}

func extractToken(r *http.Request, allowQuery bool) string {
    auth := r.Header.Get("Authorization")
    if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
        return strings.TrimSpace(auth[7:])
    }
    if allowQuery {
        if t := r.URL.Query().Get("token"); t != "" {
            return t
        }
    }
    return ""
}

func validateExp(claims jwt.MapClaims) bool {
    v, ok := claims["exp"]
    if !ok {
        return true // 无exp视为不校验
    }
    switch tv := v.(type) {
    case float64:
        // 容忍60s偏差
        return time.Now().Unix() <= int64(tv)+60
    case jsonNumber:
        // 兼容某些解析器写入的 json.Number
        if i, err := tv.Int64(); err == nil {
            return time.Now().Unix() <= i+60
        }
        return false
    default:
        return false
    }
}

// jsonNumber 适配本地避免直接引入 encoding/json 依赖符号
type jsonNumber interface{ Int64() (int64, error) }
