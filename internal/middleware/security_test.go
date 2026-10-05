package middleware

import (
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/coder-lulu/newbee-proxy/internal/config"
    jwt "github.com/golang-jwt/jwt/v4"
)

func TestIsProtectedPath(t *testing.T) {
    cases := []struct{
        path string
        want bool
    }{
        {"/api/rdp/websocket", true},
        {"/api/vnc/websocket?x=1", true},
        {"/ws/ssh", true},
        {"/ws/telnet", true},
        {"/health", false},
        {"/status", false},
    }
    for _, c := range cases {
        if got := isProtectedPath(c.path, defaultProtectedPrefixes); got != c.want {
            t.Fatalf("isProtectedPath(%s)=%v, want %v", c.path, got, c.want)
        }
    }
}

func TestExtractToken(t *testing.T) {
    // header
    req := httptest.NewRequest("GET", "/api/rdp/websocket", nil)
    req.Header.Set("Authorization", "Bearer abc.def.ghi")
    if tok := extractToken(req, true); tok != "abc.def.ghi" {
        t.Fatalf("extractToken(header)=%s, want token", tok)
    }
    // query
    req2 := httptest.NewRequest("GET", "/api/rdp/websocket?token=qqq", nil)
    if tok := extractToken(req2, true); tok != "qqq" {
        t.Fatalf("extractToken(query)=%s, want qqq", tok)
    }
    // no token
    req3 := httptest.NewRequest("GET", "/api/rdp/websocket", nil)
    if tok := extractToken(req3, false); tok != "" {
        t.Fatalf("extractToken(none)=%s, want empty", tok)
    }
}

func TestValidateExp(t *testing.T) {
    now := time.Now()
    claimsValid := jwt.MapClaims{
        "exp": float64(now.Add(2 * time.Minute).Unix()),
    }
    if !validateExp(claimsValid) {
        t.Fatalf("validateExp(valid) = false, want true")
    }
    claimsExpired := jwt.MapClaims{
        "exp": float64(now.Add(-2 * time.Minute).Unix()),
    }
    if validateExp(claimsExpired) {
        t.Fatalf("validateExp(expired) = true, want false")
    }
}

func TestComposeLimitKey(t *testing.T) {
    // without claims → ip key
    req := httptest.NewRequest("GET", "/api/ssh/websocket", nil)
    req.RemoteAddr = "192.168.1.10:55555"
    key := composeLimitKey(req)
    if key != "ip:192.168.1.10" {
        t.Fatalf("composeLimitKey(ip)=%s, want ip:192.168.1.10", key)
    }
}

func TestProtectedOverride(t *testing.T) {
    custom := []string{"/foo"}
    if !isProtectedPath("/foo/bar", custom) {
        t.Fatalf("custom protected not applied")
    }
    if isProtectedPath("/api/ssh/websocket", custom) {
        t.Fatalf("default should not be used when custom provided")
    }
}

func TestOriginWhitelistMiddleware(t *testing.T) {
    // Build middleware with whitelist
    sec := config.SecurityConf{}
    sec.OriginWhitelist = []string{"https://allowed.example.com"}
    // Enforce via JWT.Enforce
    sec.JWT = &config.JWTConf{Enabled: true, Secret: "dev", Enforce: true}
    mw := originWhitelistMiddleware(sec)
    // Next handler sets a marker header
    next := func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-Passed", "1") }
    h := mw(next)

    // Disallowed origin should be forbidden when enforce
    req := httptest.NewRequest("GET", "/api/ssh/websocket", nil)
    req.Header.Set("Origin", "https://bad.example.com")
    w := httptest.NewRecorder()
    h(w, req)
    if w.Result().StatusCode != http.StatusForbidden {
        t.Fatalf("want 403 for disallowed origin, got %d", w.Result().StatusCode)
    }

    // Allowed origin should pass
    req2 := httptest.NewRequest("GET", "/api/ssh/websocket", nil)
    req2.Header.Set("Origin", "https://allowed.example.com")
    w2 := httptest.NewRecorder()
    h(w2, req2)
    if w2.Header().Get("X-Passed") != "1" {
        t.Fatalf("allowed origin did not reach next handler")
    }
}
