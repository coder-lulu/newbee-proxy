package svc

import (
    "encoding/json"
    "net"
    "net/http"
    "strings"
    "time"

    jwt "github.com/golang-jwt/jwt/v4"
    "github.com/zeromicro/go-zero/core/logx"
)

// AuditManager 轻量审计管理器（当前仅本地日志；后续可上报到 Ops Center）
type AuditManager struct {
    logger logx.Logger
}

func NewAuditManager(logger logx.Logger) *AuditManager {
    return &AuditManager{logger: logger}
}

type AuditEvent struct {
    Time       int64                  `json:"time"`
    Event      string                 `json:"event"`
    Protocol   string                 `json:"protocol,omitempty"`
    ClientIP   string                 `json:"client_ip,omitempty"`
    Target     string                 `json:"target,omitempty"`
    SessionID  string                 `json:"session_id,omitempty"`
    ConnID     string                 `json:"conn_id,omitempty"`
    TenantID   string                 `json:"tenant_id,omitempty"`
    UserID     string                 `json:"user_id,omitempty"`
    CIID       string                 `json:"ci_id,omitempty"`
    Extra      map[string]interface{} `json:"extra,omitempty"`
    Error      string                 `json:"error,omitempty"`
}

func (am *AuditManager) log(evt *AuditEvent) {
    b, _ := json.Marshal(evt)
    am.logger.Infof("AUDIT %s", string(b))
}

func (am *AuditManager) LogWSConnect(r *http.Request, protocol, target, sessionID, connID string, claims jwt.MapClaims) {
    tenant, user, ci := claimStr(claims, "tenantId"), claimStr(claims, "userId"), claimStr(claims, "ciId")
    am.log(&AuditEvent{
        Time:      time.Now().Unix(),
        Event:     "ws_session_open",
        Protocol:  protocol,
        ClientIP:  clientIP(r),
        Target:    maskTarget(target),
        SessionID: sessionID,
        ConnID:    connID,
        TenantID:  tenant,
        UserID:    user,
        CIID:      ci,
    })
}

func (am *AuditManager) LogWSDisconnect(r *http.Request, protocol, target, sessionID, connID string, claims jwt.MapClaims) {
    tenant, user, ci := claimStr(claims, "tenantId"), claimStr(claims, "userId"), claimStr(claims, "ciId")
    am.log(&AuditEvent{
        Time:      time.Now().Unix(),
        Event:     "ws_session_close",
        Protocol:  protocol,
        ClientIP:  clientIP(r),
        Target:    maskTarget(target),
        SessionID: sessionID,
        ConnID:    connID,
        TenantID:  tenant,
        UserID:    user,
        CIID:      ci,
    })
}

func (am *AuditManager) LogWSError(r *http.Request, protocol, target, sessionID, connID string, claims jwt.MapClaims, err error) {
    tenant, user, ci := claimStr(claims, "tenantId"), claimStr(claims, "userId"), claimStr(claims, "ciId")
    am.log(&AuditEvent{
        Time:      time.Now().Unix(),
        Event:     "ws_session_error",
        Protocol:  protocol,
        ClientIP:  clientIP(r),
        Target:    maskTarget(target),
        SessionID: sessionID,
        ConnID:    connID,
        TenantID:  tenant,
        UserID:    user,
        CIID:      ci,
        Error:     safeErr(err),
    })
}

// Utils
func clientIP(r *http.Request) string {
    host, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        return r.RemoteAddr
    }
    return host
}

func maskTarget(target string) string {
    // 屏蔽密码等敏感内容（简单保护）
    if strings.Contains(target, "password=") {
        return strings.ReplaceAll(target, "password=", "password=***")
    }
    return target
}

func safeErr(err error) string {
    if err == nil {
        return ""
    }
    s := err.Error()
    s = strings.ReplaceAll(s, "password=", "password=***")
    return s
}

func claimStr(c jwt.MapClaims, k string) string {
    if c == nil {
        return ""
    }
    if v, ok := c[k]; ok {
        if s, ok2 := v.(string); ok2 {
            return s
        }
    }
    return ""
}

