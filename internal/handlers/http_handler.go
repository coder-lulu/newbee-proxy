package handlers

import (
    "encoding/json"
    "net/http"
    "net/url"
    "strings"
    "time"

    "github.com/coder-lulu/newbee-proxy/internal/metrics"
    "github.com/coder-lulu/newbee-proxy/internal/svc"
    httpplugin "github.com/coder-lulu/newbee-proxy/plugins/http"
)

// HTTP 同步请求入口：POST /api/http/request
func HTTPRequestHandler(s *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !s.IsAcceptingNew() { w.Header().Set("Retry-After", "30"); http.Error(w, "draining", http.StatusServiceUnavailable); return }

        var req httpplugin.HTTPRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, "invalid json", http.StatusBadRequest)
            return
        }

        // 获取 http 插件
        plug, ok := s.PluginManager.GetPlugin("http")
        if !ok {
            http.Error(w, "http plugin not loaded", http.StatusServiceUnavailable)
            return
        }
        hp, ok := plug.(*httpplugin.Plugin)
        if !ok {
            http.Error(w, "invalid http plugin", http.StatusInternalServerError)
            return
        }

        // 统计域名
        domain := ""
        if u, err := url.Parse(req.URL); err == nil { domain = u.Hostname() }

        start := time.Now()
        resp, err := hp.DoRequest(r.Context(), &req)
        dur := time.Since(start).Seconds()
        method := strings.ToUpper(req.Method)
        if method == "" { method = http.MethodGet }

        if err != nil {
            metrics.ObserveHTTPRequest(method, domain, statusFromErr(err), dur)
            metrics.IncHTTPError("request_failed")
            http.Error(w, err.Error(), http.StatusBadGateway)
            return
        }

        metrics.ObserveHTTPRequest(method, domain, resp.Status, dur)
        _ = json.NewEncoder(w).Encode(resp)
    }
}

func statusFromErr(_ error) int { return 0 }

