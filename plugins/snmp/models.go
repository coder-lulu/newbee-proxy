package snmpplugin

// 版本：v2c 或 v3
type Version string

const (
    V2c Version = "v2c"
    V3  Version = "v3"
)

// V3 安全级别
type SecurityLevel string

const (
    NoAuthNoPriv SecurityLevel = "noAuthNoPriv"
    AuthNoPriv   SecurityLevel = "authNoPriv"
    AuthPriv     SecurityLevel = "authPriv"
)

// V3 协议
type AuthProtocol string
type PrivProtocol string

const (
    AuthMD5 AuthProtocol = "MD5"
    AuthSHA AuthProtocol = "SHA"

    PrivDES PrivProtocol = "DES"
    PrivAES PrivProtocol = "AES"
)

// SNMP 凭据
type Credentials struct {
    // v2c
    Community string `json:"community,omitempty"`

    // v3
    Username      string        `json:"username,omitempty"`
    SecurityLevel SecurityLevel `json:"security_level,omitempty"`
    AuthProtocol  AuthProtocol  `json:"auth_protocol,omitempty"`
    AuthPassword  string        `json:"auth_password,omitempty"`
    PrivProtocol  PrivProtocol  `json:"priv_protocol,omitempty"`
    PrivPassword  string        `json:"priv_password,omitempty"`
}

// 通用请求参数
type Request struct {
    Version   Version     `json:"version"`
    Target    string      `json:"target"`         // ip 或主机名
    Port      uint16      `json:"port"`           // 默认 161
    TimeoutMs int         `json:"timeout_ms"`     // 默认 3000
    Retries   int         `json:"retries"`        // 默认 1
    Creds     Credentials `json:"creds"`

    // 操作参数
    Oids    []string `json:"oids,omitempty"`      // get 使用
    RootOid string   `json:"root_oid,omitempty"`  // walk 使用
}

// 变量绑定
type VarBind struct {
    Oid   string      `json:"oid"`
    Type  string      `json:"type"`
    Value interface{} `json:"value"`
}

// 响应
type GetResponse struct {
    Binds []VarBind `json:"binds"`
}

type WalkResponse struct {
    Binds []VarBind `json:"binds"`
}
