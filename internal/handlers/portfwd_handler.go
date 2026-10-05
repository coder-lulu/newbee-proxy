package handlers

import (
    "encoding/json"
    "net/http"

    "github.com/coder-lulu/newbee-proxy/internal/svc"
    pf "github.com/coder-lulu/newbee-proxy/plugins/portfwd"
)

type PortFwdHandler struct { svc *svc.ServiceContext }

func NewPortFwdHandler(s *svc.ServiceContext) *PortFwdHandler { return &PortFwdHandler{svc: s} }

func (h *PortFwdHandler) CreateRule(w http.ResponseWriter, r *http.Request) {
    plug, ok := h.svc.PluginManager.GetPlugin("portfwd"); if !ok { http.Error(w, "plugin not loaded", 503); return }
    p, ok := plug.(*pf.Plugin); if !ok { http.Error(w, "invalid plugin", 500); return }
    var req pf.ForwardRule
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil { http.Error(w, "invalid json", 400); return }
    if req.ID == "" || req.Listen == "" || req.Target == "" { http.Error(w, "missing id/listen/target", 400); return }
    if err := p.CreateRule(&req); err != nil { http.Error(w, err.Error(), 400); return }
    if err := p.StartRule(req.ID); err != nil { http.Error(w, err.Error(), 400); return }
    _ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": req.ID})
}

func (h *PortFwdHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    plug, ok := h.svc.PluginManager.GetPlugin("portfwd"); if !ok { http.Error(w, "plugin not loaded", 503); return }
    p, ok := plug.(*pf.Plugin); if !ok { http.Error(w, "invalid plugin", 500); return }
    _ = p.DeleteRule(id)
    _ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (h *PortFwdHandler) StartRule(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    plug, ok := h.svc.PluginManager.GetPlugin("portfwd"); if !ok { http.Error(w, "plugin not loaded", 503); return }
    p, ok := plug.(*pf.Plugin); if !ok { http.Error(w, "invalid plugin", 500); return }
    if err := p.StartRule(id); err != nil { http.Error(w, err.Error(), 400); return }
    _ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (h *PortFwdHandler) StopRule(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    plug, ok := h.svc.PluginManager.GetPlugin("portfwd"); if !ok { http.Error(w, "plugin not loaded", 503); return }
    p, ok := plug.(*pf.Plugin); if !ok { http.Error(w, "invalid plugin", 500); return }
    if err := p.StopRule(id); err != nil { http.Error(w, err.Error(), 400); return }
    _ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (h *PortFwdHandler) ListRules(w http.ResponseWriter, r *http.Request) {
    plug, ok := h.svc.PluginManager.GetPlugin("portfwd"); if !ok { http.Error(w, "plugin not loaded", 503); return }
    p, ok := plug.(*pf.Plugin); if !ok { http.Error(w, "invalid plugin", 500); return }
    _ = json.NewEncoder(w).Encode(map[string]any{"rules": p.ListRules()})
}
