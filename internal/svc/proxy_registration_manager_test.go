package svc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder-lulu/newbee-proxy/internal/config"
	"github.com/zeromicro/go-zero/core/logx"
)

func TestProxyRegistrationAndHeartbeatContract(t *testing.T) {
	requests := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		for _, field := range []string{"worker_id", "proxy_id"} {
			if payload[field] != "worker-preferred" {
				t.Errorf("%s = %v", field, payload[field])
			}
		}
		if payload["psk"] != "test-psk" || r.Header.Get("X-OPS-PSK") != "test-psk" {
			t.Error("missing PSK")
		}
		switch r.URL.Path {
		case "/proxy/register":
			if payload["name"] != "worker-preferred" || payload["ip"] != "192.0.2.10" || payload["port"] != float64(9090) {
				t.Errorf("invalid registration identity")
			}
		case "/proxy/heartbeat":
			if payload["proxy_status"] != "online" {
				t.Errorf("proxy_status = %v", payload["proxy_status"])
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		requests <- r.URL.Path
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer srv.Close()
	service := &ServiceContext{}
	service.Config.Port = 9090
	service.Config.Network.PublicIP = "192.0.2.10"
	m := NewProxyRegistrationManager(config.OpsCenterConf{Endpoints: []string{srv.URL}, WorkerID: "worker-preferred", ProxyID: "legacy-id", PSK: "test-psk"}, logx.WithContext(context.Background()), service)
	if err := m.register(); err != nil {
		t.Fatal(err)
	}
	if err := m.heartbeat(); err != nil {
		t.Fatal(err)
	}
	if got := <-requests; got != "/proxy/register" {
		t.Fatal(got)
	}
	if got := <-requests; got != "/proxy/heartbeat" {
		t.Fatal(got)
	}
	m.cfg.WorkerID = ""
	if got := m.workerID(); got != "legacy-id" {
		t.Fatalf("legacy identity = %q", got)
	}
}
