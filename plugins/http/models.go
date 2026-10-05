package httpplugin

import "time"

// HTTPAuth 鉴权配置
type HTTPAuth struct {
    Type       string            `json:"type"`        // none|basic|bearer|apiKey|hmac
    Username   string            `json:"username"`
    Password   string            `json:"password"`
    Token      string            `json:"token"`
    APIKeyName string            `json:"api_key_name"`
    APIKeyIn   string            `json:"api_key_in"`  // header|query
    APIKey     string            `json:"api_key"`
    // 预留 HMAC/AWSV4 等
    Params map[string]string `json:"params"`
}

// HTTPRetry 重试配置
type HTTPRetry struct {
    Max                 int      `json:"max"`
    BaseDelay           string   `json:"base_delay"` // e.g. 200ms
    MaxDelay            string   `json:"max_delay"`
    RetryOn5xx          bool     `json:"retry_on_5xx"`
    RetryOnNetworkError bool     `json:"retry_on_network_error"`
    RetryOnCodes        []int    `json:"retry_on_codes"`
}

// HTTPExpect 期望/校验
type HTTPExpect struct {
    StatusIn []int `json:"status_in"`
}

// HTTPRequest 通用HTTP请求
type HTTPRequest struct {
    Method      string            `json:"method"`
    URL         string            `json:"url"`
    PathParams  map[string]string `json:"path_params"`
    Query       map[string]string `json:"query"`
    Headers     map[string]string `json:"headers"`
    Auth        *HTTPAuth         `json:"auth"`
    BodyType    string            `json:"body_type"` // json|raw|form (当前实现 json/raw)
    Body        interface{}       `json:"body"`
    Timeout     string            `json:"timeout"` // e.g. 10s
    Retry       *HTTPRetry        `json:"retry"`
    Expect      *HTTPExpect       `json:"expect"`
    SaveToFile  bool              `json:"save_to_file"`   // 保存响应体到文件
    FileName    string            `json:"file_name"`      // 自定义文件名（可选）
}

// HTTPResponse 简要响应
type HTTPResponse struct {
    Status       int               `json:"status"`
    Headers      map[string]string `json:"headers"`
    SizeBytes    int64             `json:"size_bytes"`
    DurationMs   int64             `json:"duration_ms"`
    BodySnippet  string            `json:"body_snippet,omitempty"`
    BodyFilePath string            `json:"body_file_path,omitempty"`
    BodySHA256   string            `json:"body_sha256,omitempty"`
    // 预留提取字段
    Extracted map[string]interface{} `json:"extracted,omitempty"`
}

// 内部解析后的超时
func (r *HTTPRequest) ParsedTimeout(defaultTimeout time.Duration) time.Duration {
    if r.Timeout == "" {
        return defaultTimeout
    }
    if d, err := time.ParseDuration(r.Timeout); err == nil && d > 0 {
        return d
    }
    return defaultTimeout
}
