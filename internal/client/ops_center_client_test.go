package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpsCenterBusinessResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantErr string
		status              int
	}{
		{"success", `{"code":0,"msg":"success"}`, "", 200},
		{"unauthorized", `{"code":401,"msg":"Invalid PSK"}`, "business code 401", 200},
		{"backend error", `{"code":500}`, "business code 500", 200},
		{"missing code", `{}`, "missing business code", 200},
		{"invalid json", `oops`, "invalid response", 200},
		{"http error", `{"code":0}`, "status 503", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/proxy/register" || r.Method != http.MethodPost {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("X-OPS-PSK") != "test-psk" {
					t.Error("missing PSK header")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := NewOpsCenterClient([]string{srv.URL}, nil)
			c.PSK = "test-psk"
			err := c.Register(map[string]string{"proxy_id": "local-worker"})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v; want %q", err, tc.wantErr)
			}
		})
	}
}
