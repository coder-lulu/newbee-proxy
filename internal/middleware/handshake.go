package middleware

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "net/http"
    "sort"
    "strings"

    "github.com/coder-lulu/newbee-proxy/internal/config"

    jwt "github.com/golang-jwt/jwt/v4"
    "github.com/zeromicro/go-zero/core/logx"
)

// VerifyWSRequest validates JWT claims and optional handshake signature.
// - expectedProtocol: one of ssh|telnet|rdp|vnc
// - conf: security configuration (reads JWT secret and enforce flag)
// Returns ok and a reason for logging when not ok.
func VerifyWSRequest(r *http.Request, expectedProtocol string, conf config.SecurityConf, psk string) (bool, string) {
    // 允许 JWT 或 PSK 其一通过；若 Enforce=true 则都未通过时拒绝；否则观察模式
    jwtEnabled := conf.JWT != nil && conf.JWT.Enabled && conf.JWT.Secret != ""
    jwtPassed := false
    pskPassed := false

    // 1) JWT 路径
    if jwtEnabled {
        if claims, ok := GetJWTClaims(r.Context()); ok {
            // Validate protocol claim alignment
            if p, okp := claimString(claims, "protocol"); okp {
                if expectedProtocol != "" && p != expectedProtocol {
                    if conf.JWT.Enforce {
                        return false, "protocol-mismatch"
                    }
                    logx.WithContext(r.Context()).Infof("protocol mismatch: claim=%s expected=%s", p, expectedProtocol)
                }
            }

            // Optional signature check: if client provided `sig` query param, verify it
            if sig := r.URL.Query().Get("sig"); sig != "" {
                params := map[string]string{}
                for k, vals := range r.URL.Query() {
                    if k == "sig" || strings.EqualFold(k, "token") { continue }
                    if len(vals) == 0 { continue }
                    params[k] = vals[0]
                }
                sessionID, _ := claimString(claims, "sessionId")
                ciId, _ := claimString(claims, "ciId")
                proto, _ := claimString(claims, "protocol")
                expect := computeSig(sessionID, proto, ciId, params, []byte(conf.JWT.Secret))
                if !hmac.Equal([]byte(sig), []byte(expect)) {
                    if conf.JWT.Enforce {
                        return false, "sig-invalid"
                    }
                    logx.WithContext(r.Context()).Infof("handshake signature invalid: claimSession=%s claimCi=%s", sessionID, ciId)
                }
            }
            jwtPassed = true
        }
    }

    // 2) PSK 路径（作为 JWT 的备选或在 JWT 关闭时使用）
    if !jwtPassed && psk != "" {
        // 约定：优先取 Header: X-PSK，其次 query: psk
        got := r.Header.Get("X-PSK")
        if got == "" {
            got = r.URL.Query().Get("psk")
        }
        if subtleHmacEqual([]byte(got), []byte(psk)) {
            pskPassed = true
        }
    }

    if jwtPassed || pskPassed {
        return true, "ok"
    }
    // 两种方式都未通过
    if conf.JWT != nil && conf.JWT.Enforce {
        return false, "jwt-required"
    }
    // 观察模式：放行但记录
    logx.WithContext(r.Context()).Info("WS auth bypassed (observe mode, neither JWT nor PSK provided)")
    return true, "observe-no-auth"
}

func computeSig(sessionId, protocol, ciId string, params map[string]string, secret []byte) string {
    // canonicalize params by key ASC
    keys := make([]string, 0, len(params))
    for k := range params { keys = append(keys, k) }
    sort.Strings(keys)
    b := strings.Builder{}
    for i, k := range keys {
        if k == "sig" { continue }
        if i > 0 { b.WriteString("&") }
        b.WriteString(k)
        b.WriteString("=")
        b.WriteString(params[k])
    }
    payload := sessionId + "|" + protocol + "|" + ciId + "|" + b.String()
    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(payload))
    return hex.EncodeToString(mac.Sum(nil))
}

// subtleHmacEqual 常量时间比较
func subtleHmacEqual(a, b []byte) bool {
    if len(a) != len(b) { return false }
    var v byte
    for i := range a { v |= a[i] ^ b[i] }
    return v == 0
}

func claimString(claims jwt.MapClaims, key string) (string, bool) {
    if v, ok := claims[key]; ok {
        if s, ok := v.(string); ok {
            return s, true
        }
    }
    return "", false
}
