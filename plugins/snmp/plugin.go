package snmpplugin

import (
    "context"
    "fmt"
    "time"
    "strings"

    gosnmp "github.com/gosnmp/gosnmp"

    "github.com/coder-lulu/newbee-proxy/plugins/common"
    metrics "github.com/coder-lulu/newbee-proxy/internal/metrics"
)

// Plugin 提供 SNMP v2c/v3 的 get/walk 能力（最小实现）
type Plugin struct{}

func New() *Plugin { return &Plugin{} }

// 基本信息
func (p *Plugin) Name() string                 { return "snmp" }
func (p *Plugin) Version() string              { return "0.1.0" }
func (p *Plugin) SupportedProtocols() []string { return []string{"snmp"} }
func (p *Plugin) Description() string          { return "SNMP v2c/v3 GET/WALK plugin" }
func (p *Plugin) Initialize(map[string]any) error { return nil }
func (p *Plugin) Start() error { return nil }
func (p *Plugin) Stop() error  { return nil }
func (p *Plugin) IsRunning() bool { return true }
func (p *Plugin) GetStatus() *common.PluginStatus  { return &common.PluginStatus{Name: p.Name(), Version: p.Version(), Status: "running"} }
func (p *Plugin) GetMetrics() *common.PluginMetrics { return &common.PluginMetrics{} }
func (p *Plugin) UpdateConfig(map[string]any) error { return nil }

// 无连接模型
func (p *Plugin) CreateConnection(context.Context, string, *common.Credentials) (common.Connection, error) { return nil, common.NewPluginError("snmp", common.ErrCodeUnsupportedFeature, "connectionless") }
func (p *Plugin) CloseConnection(string) error { return nil }
func (p *Plugin) GetConnection(string) (common.Connection, bool) { return nil, false }
func (p *Plugin) ListConnections() []string { return nil }

func buildGoSNMP(req *Request) (*gosnmp.GoSNMP, error) {
    port := uint16(161)
    if req.Port > 0 { port = req.Port }
    timeout := time.Duration(req.TimeoutMs)
    if timeout <= 0 { timeout = 3000 }
    target := req.Target
    if target == "" { return nil, fmt.Errorf("empty target") }

    g := &gosnmp.GoSNMP{
        Target:    target,
        Port:      port,
        Retries:   req.Retries,
        Timeout:   timeout * time.Millisecond,
        Transport: "udp",
        MaxOids:   gosnmp.MaxOids,
    }
    switch strings.ToLower(string(req.Version)) {
    case "v2c":
        g.Version = gosnmp.Version2c
        g.Community = req.Creds.Community
    case "v3":
        g.Version = gosnmp.Version3
        secParams := &gosnmp.UsmSecurityParameters{UserName: req.Creds.Username}
        level := gosnmp.NoAuthNoPriv
        switch req.Creds.SecurityLevel {
        case AuthNoPriv:
            level = gosnmp.AuthNoPriv
            secParams.AuthenticationPassphrase = req.Creds.AuthPassword
            if req.Creds.AuthProtocol == AuthSHA { secParams.AuthenticationProtocol = gosnmp.SHA } else { secParams.AuthenticationProtocol = gosnmp.MD5 }
        case AuthPriv:
            level = gosnmp.AuthPriv
            secParams.AuthenticationPassphrase = req.Creds.AuthPassword
            if req.Creds.AuthProtocol == AuthSHA { secParams.AuthenticationProtocol = gosnmp.SHA } else { secParams.AuthenticationProtocol = gosnmp.MD5 }
            secParams.PrivacyPassphrase = req.Creds.PrivPassword
            if req.Creds.PrivProtocol == PrivAES { secParams.PrivacyProtocol = gosnmp.AES } else { secParams.PrivacyProtocol = gosnmp.DES }
        default:
            level = gosnmp.NoAuthNoPriv
        }
        g.SecurityModel = gosnmp.UserSecurityModel
        g.MsgFlags = level
        g.SecurityParameters = secParams
    default:
        return nil, fmt.Errorf("unsupported version: %s", req.Version)
    }
    return g, nil
}

// Get 执行 SNMP GET
func (p *Plugin) Get(ctx context.Context, req *Request) (*GetResponse, error) {
    if len(req.Oids) == 0 { return nil, fmt.Errorf("oids empty") }
    g, err := buildGoSNMP(req); if err != nil { metrics.IncSNMPError("get", "build"); return nil, err }
    if err := g.Connect(); err != nil { metrics.IncSNMPError("get", "connect"); return nil, err }
    defer g.Conn.Close()
    start := time.Now()
    pkt, err := g.Get(req.Oids)
    if err != nil { metrics.IncSNMPError("get", "request"); return nil, err }
    metrics.IncSNMPReq("get", string(req.Version))
    metrics.ObserveSNMPDur("get", time.Since(start).Seconds())
    resp := &GetResponse{ Binds: make([]VarBind, 0, len(pkt.Variables)) }
    for _, v := range pkt.Variables { resp.Binds = append(resp.Binds, toVarBind(v)) }
    return resp, nil
}

// Walk 执行 SNMP WALK
func (p *Plugin) Walk(ctx context.Context, req *Request) (*WalkResponse, error) {
    if req.RootOid == "" { return nil, fmt.Errorf("root_oid empty") }
    g, err := buildGoSNMP(req); if err != nil { metrics.IncSNMPError("walk", "build"); return nil, err }
    if err := g.Connect(); err != nil { metrics.IncSNMPError("walk", "connect"); return nil, err }
    defer g.Conn.Close()
    start := time.Now()
    binds := make([]VarBind, 0, 64)
    err = g.Walk(req.RootOid, func(v gosnmp.SnmpPDU) error { binds = append(binds, toVarBind(v)); return nil })
    if err != nil { metrics.IncSNMPError("walk", "request"); return nil, err }
    metrics.IncSNMPReq("walk", string(req.Version))
    metrics.ObserveSNMPDur("walk", time.Since(start).Seconds())
    return &WalkResponse{ Binds: binds }, nil
}

func toVarBind(v gosnmp.SnmpPDU) VarBind {
    val := v.Value
    t := v.Type.String()
    return VarBind{ Oid: v.Name, Type: t, Value: val }
}
