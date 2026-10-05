package middleware

import (
    "net/http/httptest"
    "testing"
    "net/url"
    "context"

    jwt "github.com/golang-jwt/jwt/v4"
    "github.com/coder-lulu/newbee-proxy/internal/config"
)

func TestVerifyWSRequest_SignatureAndProtocol(t *testing.T) {
    // prepare request with query params
    r := httptest.NewRequest("GET", "/api/ssh/websocket", nil)
    q := url.Values{}
    q.Set("port", "2222")
    r.URL.RawQuery = q.Encode()

    // inject claims into context
    claims := jwt.MapClaims{"sessionId":"s1","protocol":"ssh","ciId":"ci-001"}
    r = r.WithContext(context.WithValue(r.Context(), claimsCtxKey, claims))

    conf := config.SecurityConf{JWT: &config.JWTConf{Enabled: true, Secret: "dev-secret", Enforce: true}}

    // compute and attach sig
    sig := computeSig("s1", "ssh", "ci-001", map[string]string{"port":"2222"}, []byte(conf.JWT.Secret))
    q.Set("sig", sig)
    r.URL.RawQuery = q.Encode()

    ok, _ := VerifyWSRequest(r, "ssh", conf)
    if !ok { t.Fatalf("expected ok") }

    // mismatch protocol
    if ok, _ := VerifyWSRequest(r, "telnet", conf); ok {
        t.Fatalf("expected protocol mismatch to fail")
    }
}
