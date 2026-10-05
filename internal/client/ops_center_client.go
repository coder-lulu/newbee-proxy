package client

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/zeromicro/go-zero/core/logx"
)

type OpsCenterClient struct {
    endpoints []string
    http      *http.Client
    logger    logx.Logger
    PSK       string
}

func NewOpsCenterClient(endpoints []string, logger logx.Logger) *OpsCenterClient {
    return &OpsCenterClient{
        endpoints: endpoints,
        http: &http.Client{Timeout: 10 * time.Second},
        logger: logger,
    }
}

// Register 调用 /worker/register (新版API)
func (c *OpsCenterClient) Register(payload any) error {
    return c.post("/worker/register", payload)
}

// Heartbeat 调用 /worker/heartbeat (新版API)
func (c *OpsCenterClient) Heartbeat(payload any) error {
    return c.post("/worker/heartbeat", payload)
}

// RegisterLegacy 调用 /ops/proxy/register (旧版API，向后兼容)
func (c *OpsCenterClient) RegisterLegacy(payload any) error {
    return c.post("/ops/proxy/register", payload)
}

// HeartbeatLegacy 调用 /ops/proxy/heartbeat (旧版API，向后兼容)
func (c *OpsCenterClient) HeartbeatLegacy(payload any) error {
    return c.post("/ops/proxy/heartbeat", payload)
}

// ReportTaskResult 上报任务执行结果 (Phase 2)
func (c *OpsCenterClient) ReportTaskResult(payload any) error {
    return c.post("/task/result", payload)
}

func (c *OpsCenterClient) post(path string, payload any) error {
    if len(c.endpoints) == 0 {
        return fmt.Errorf("no ops center endpoints configured")
    }
    b, err := json.Marshal(payload)
    if err != nil { return err }
    var lastErr error
    for _, base := range c.endpoints {
        url := base + path
        req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
        if err != nil { lastErr = err; continue }
        req.Header.Set("Content-Type", "application/json")
        if c.PSK != "" { req.Header.Set("X-OPS-PSK", c.PSK) }
        resp, err := c.http.Do(req)
        if err != nil { lastErr = err; continue }
        _ = resp.Body.Close()
        if resp.StatusCode >= 200 && resp.StatusCode < 300 {
            return nil
        }
        lastErr = fmt.Errorf("%s: status %d", url, resp.StatusCode)
    }
    return lastErr
}
