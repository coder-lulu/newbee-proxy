package handlers

import (
    "encoding/json"
    "net/http"
    "strconv"
    "strings"

    "github.com/coder-lulu/newbee-proxy/internal/svc"
    snmpplugin "github.com/coder-lulu/newbee-proxy/plugins/snmp"
)

type SNMPHandler struct { svc *svc.ServiceContext }

func NewSNMPHandler(s *svc.ServiceContext) *SNMPHandler { return &SNMPHandler{svc: s} }

// POST /api/snmp/get  body=Request
func (h *SNMPHandler) Get(w http.ResponseWriter, r *http.Request) {
    plug, ok := h.svc.PluginManager.GetPlugin("snmp"); if !ok { http.Error(w, "plugin not loaded", 503); return }
    p, ok := plug.(*snmpplugin.Plugin); if !ok { http.Error(w, "invalid plugin", 500); return }
    var req snmpplugin.Request
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil { http.Error(w, "invalid json", 400); return }
    applySNMPDefaults(&req, h.svc)
    resp, err := p.Get(r.Context(), &req); if err != nil { http.Error(w, err.Error(), 400); return }
    _ = json.NewEncoder(w).Encode(resp)
}

// POST /api/snmp/walk body=Request
func (h *SNMPHandler) Walk(w http.ResponseWriter, r *http.Request) {
    plug, ok := h.svc.PluginManager.GetPlugin("snmp"); if !ok { http.Error(w, "plugin not loaded", 503); return }
    p, ok := plug.(*snmpplugin.Plugin); if !ok { http.Error(w, "invalid plugin", 500); return }
    var req snmpplugin.Request
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil { http.Error(w, "invalid json", 400); return }
    applySNMPDefaults(&req, h.svc)
    resp, err := p.Walk(r.Context(), &req); if err != nil { http.Error(w, err.Error(), 400); return }
    _ = json.NewEncoder(w).Encode(resp)
}

// 简单健康：GET /api/snmp/ping?target=1.2.3.4&version=v2c&community=public
func (h *SNMPHandler) Ping(w http.ResponseWriter, r *http.Request) {
    plug, ok := h.svc.PluginManager.GetPlugin("snmp"); if !ok { http.Error(w, "plugin not loaded", 503); return }
    p, ok := plug.(*snmpplugin.Plugin); if !ok { http.Error(w, "invalid plugin", 500); return }
    q := r.URL.Query()
    port := uint16(161)
    if v := q.Get("port"); v != "" { if i, err := strconv.Atoi(v); err == nil { port = uint16(i) } }
    req := snmpplugin.Request{Version: snmpplugin.Version(q.Get("version")), Target: q.Get("target"), Port: port, TimeoutMs: 0, Retries: 0, Creds: snmpplugin.Credentials{ Community: q.Get("community") }, Oids: []string{"1.3.6.1.2.1.1.1.0"}}
    // 注入默认值
    applySNMPDefaults(&req, h.svc)
    resp, err := p.Get(r.Context(), &req); if err != nil { http.Error(w, err.Error(), 400); return }
    _ = json.NewEncoder(w).Encode(resp)
}

// applySNMPDefaults 读取配置中 Plugins.SNMP 默认值，填充缺失的字段
func applySNMPDefaults(req *snmpplugin.Request, s *svc.ServiceContext) {
    if s == nil { return }
    conf := s.Config.Plugins.SNMP
    // version
    if string(req.Version) == "" {
        if conf.Version != "" { req.Version = snmpplugin.Version(strings.ToLower(conf.Version)) }
    }
    // community（仅 v2c）
    if strings.ToLower(string(req.Version)) == "v2c" {
        if req.Creds.Community == "" && conf.Community != "" { req.Creds.Community = conf.Community }
    }
    // timeout（秒 -> 毫秒）
    if req.TimeoutMs <= 0 && conf.Timeout > 0 { req.TimeoutMs = conf.Timeout * 1000 }
}
