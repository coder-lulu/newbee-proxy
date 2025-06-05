# SSH 隧道与连接管理技术详解

## 概述

本文档详细分析 Mayfly-Go 项目中的 SSH 隧道和连接管理实现，这是支持多级跳板机连接和安全远程访问的核心技术。

## 核心架构

### 1. 组件结构

```
internal/machine/mcm/
├── machine.go           # 机器信息和连接管理
├── client.go           # SSH 客户端封装
├── sshtunnel.go        # SSH 隧道实现
├── terminal.go         # 终端会话管理
├── terminal_session.go # 终端会话实现
├── terminal_handler.go # 终端处理器
├── stats.go           # 统计和监控
└── recorder.go        # 会话录制
```

### 2. 架构层次

```
┌─────────────────────────────────────────────────────────┐
│                    Web Client                           │
└─────────────────────────────────────────────────────────┘
                            │
                            │ WebSocket
                            ▼
┌─────────────────────────────────────────────────────────┐
│                 Terminal Handler                        │
│              (terminal_handler.go)                     │
└─────────────────────────────────────────────────────────┘
                            │
                            │ SSH Protocol
                            ▼
┌─────────────────────────────────────────────────────────┐
│                SSH Tunnel Manager                       │
│                (sshtunnel.go)                          │
└─────────────────────────────────────────────────────────┘
                            │
                  ┌─────────┴─────────┐
                  │                   │
                  ▼                   ▼
         ┌─────────────────┐  ┌─────────────────┐
         │  Jump Server    │  │  Target Server  │
         │ (Bastion Host)  │  │                 │
         └─────────────────┘  └─────────────────┘
```

## 核心实现分析

### 1. 机器信息管理 (machine.go)

#### 1.1 MachineInfo 结构
```go
type MachineInfo struct {
    model.ExtraData
    
    Key      string `json:"key"` // 缓存key
    Id       uint64 `json:"id"`
    Name     string `json:"name"`
    Code     string `json:"code"`
    Protocol int    `json:"protocol"`
    
    Ip   string `json:"ip"` // IP地址
    Port int    `json:"-"`  // 端口号
    
    AuthCertName string                 `json:"authCertName"`
    AuthCertType tagentity.AuthCertType `json:"-"`
    AuthMethod   int8                   `json:"-"` // 授权认证方式
    Username     string                 `json:"-"` // 用户名
    Password     string                 `json:"-"`
    Passphrase   string                 `json:"-"` // 私钥口令
    
    SshTunnelMachine *MachineInfo `json:"-"` // ssh隧道机器
    TempSshMachineId uint64       `json:"-"` // ssh隧道机器id
    EnableRecorder   int8         `json:"-"` // 是否启用终端回放记录
    CodePath         []string     `json:"codePath"`
}
```

#### 1.2 连接建立流程
```go
func (mi *MachineInfo) Conn(ctx context.Context) (*Cli, error) {
    logx.Infof("the machine[%s] is connecting: %s:%d", mi.Name, mi.Ip, mi.Port)
    
    // 如果使用了ssh隧道，则修改机器ip port为暴露的ip port
    err := mi.IfUseSshTunnelChangeIpPort(ctx, false)
    if err != nil {
        return nil, errorx.NewBiz("ssh tunnel connection failed: %s", err.Error())
    }
    
    cli := &Cli{Info: mi}
    sshClient, err := GetSshClient(mi, nil)
    if err != nil {
        if mi.UseSshTunnel() {
            CloseSshTunnelMachine(mi.TempSshMachineId, mi.GetTunnelId())
        }
        return nil, err
    }
    cli.sshClient = sshClient
    return cli, nil
}
```

### 2. SSH 隧道实现 (sshtunnel.go)

#### 2.1 隧道管理器结构
```go
type SshTunnelMachine struct {
    machineId          int
    machine            *MachineInfo
    sshClient          *ssh.Client
    forwardedPortInfos map[string]*ForwardedPortInfo
    mutex              sync.RWMutex
    ctx                context.Context
    cancel             context.CancelFunc
    lastActiveTime     int64
}
```

#### 2.2 端口转发信息
```go
type ForwardedPortInfo struct {
    LocalPort    int
    RemoteHost   string
    RemotePort   int
    LastUseTime  int64
    listener     net.Listener
    cancel       context.CancelFunc
    connections  map[string]*ForwardedConnection
    mutex        sync.RWMutex
}
```

#### 2.3 隧道建立流程
```go
func (stm *SshTunnelMachine) OpenSshTunnel(tunnelId, remoteHost string, remotePort int) (string, int, error) {
    stm.mutex.Lock()
    defer stm.mutex.Unlock()
    
    // 检查是否已存在该隧道
    if portInfo, exists := stm.forwardedPortInfos[tunnelId]; exists {
        portInfo.LastUseTime = time.Now().Unix()
        return "127.0.0.1", portInfo.LocalPort, nil
    }
    
    // 获取可用的本地端口
    localPort, err := netx.GetAvailablePort()
    if err != nil {
        return "", 0, err
    }
    
    // 创建本地监听器
    localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)
    listener, err := net.Listen("tcp", localAddr)
    if err != nil {
        return "", 0, err
    }
    
    // 创建端口转发信息
    ctx, cancel := context.WithCancel(stm.ctx)
    portInfo := &ForwardedPortInfo{
        LocalPort:   localPort,
        RemoteHost:  remoteHost,
        RemotePort:  remotePort,
        LastUseTime: time.Now().Unix(),
        listener:    listener,
        cancel:      cancel,
        connections: make(map[string]*ForwardedConnection),
    }
    
    stm.forwardedPortInfos[tunnelId] = portInfo
    
    // 启动端口转发服务
    go stm.handlePortForwarding(ctx, portInfo)
    
    return "127.0.0.1", localPort, nil
}
```

### 3. 多级 SSH 连接 (GetSshClient)

#### 3.1 递归连接实现
```go
func GetSshClient(m *MachineInfo, jumpClient *ssh.Client) (*ssh.Client, error) {
    // 递归一直取到底层没有跳板机的机器信息
    if m.SshTunnelMachine != nil {
        jumpClient, err := GetSshClient(m.SshTunnelMachine, jumpClient)
        if err != nil {
            return nil, err
        }
        // 新建一个没有跳板机的机器信息
        m1 := &MachineInfo{
            Ip:         m.Ip,
            Port:       m.Port,
            AuthMethod: m.AuthMethod,
            Username:   m.Username,
            Password:   m.Password,
            Passphrase: m.Passphrase,
        }
        // 使用跳板机连接目标机器
        return GetSshClient(m1, jumpClient)
    }
    
    // 配置 SSH 客户端
    config := &ssh.ClientConfig{
        User: m.Username,
        HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
            return nil
        },
        Timeout: 5 * time.Second,
    }
    
    // 加密算法配置
    if ciphers := m.GetExtraString("ciphers"); ciphers != "" {
        config.Ciphers = strings.Split(ciphers, ",")
    }
    if keyExchanges := m.GetExtraString("keyExchanges"); keyExchanges != "" {
        config.KeyExchanges = strings.Split(keyExchanges, ",")
    }
    
    // 认证方式配置
    if m.AuthMethod == int8(tagentity.AuthCertCiphertextTypePassword) {
        config.Auth = []ssh.AuthMethod{ssh.Password(m.Password)}
    } else if m.AuthMethod == int8(tagentity.AuthCertCiphertextTypePrivateKey) {
        var key ssh.Signer
        var err error
        
        if len(m.Passphrase) > 0 {
            key, err = ssh.ParsePrivateKeyWithPassphrase([]byte(m.Password), []byte(m.Passphrase))
        } else {
            key, err = ssh.ParsePrivateKey([]byte(m.Password))
        }
        if err != nil {
            return nil, err
        }
        config.Auth = []ssh.AuthMethod{ssh.PublicKeys(key)}
    }
    
    addr := fmt.Sprintf("%s:%d", m.Ip, m.Port)
    if jumpClient != nil {
        // 通过跳板机连接目标服务器
        netConn, err := jumpClient.Dial("tcp", addr)
        if err != nil {
            return nil, err
        }
        conn, channel, reqs, err := ssh.NewClientConn(netConn, addr, config)
        if err != nil {
            return nil, err
        }
        return ssh.NewClient(conn, channel, reqs), nil
    }
    
    // 直接连接
    sshClient, err := ssh.Dial("tcp", addr, config)
    if err != nil {
        return nil, err
    }
    return sshClient, nil
}
```

### 4. 终端会话管理 (terminal_session.go)

#### 4.1 TerminalSession 结构
```go
type TerminalSession struct {
    Id           string
    machine      *MachineInfo
    sshClient    *ssh.Client
    sshSession   *ssh.Session
    
    recorder     Recorder
    stdin        io.WriteCloser
    comboOutput  *wsWriterWrapper
    
    ctx          context.Context
    cancel       context.CancelFunc
    closed       int32
}
```

#### 4.2 会话建立和配置
```go
func (ts *TerminalSession) Start(ctx context.Context, cols, rows uint32) error {
    ts.ctx, ts.cancel = context.WithCancel(ctx)
    
    // 创建SSH会话
    sshSession, err := ts.sshClient.NewSession()
    if err != nil {
        return err
    }
    ts.sshSession = sshSession
    
    // 配置终端模式
    modes := ssh.TerminalModes{
        ssh.ECHO:          1,
        ssh.TTY_OP_ISPEED: 14400,
        ssh.TTY_OP_OSPEED: 14400,
    }
    
    // 请求伪终端
    if err := sshSession.RequestPty("xterm", int(rows), int(cols), modes); err != nil {
        return err
    }
    
    // 获取标准输入输出
    ts.stdin, err = sshSession.StdinPipe()
    if err != nil {
        return err
    }
    
    stdout, err := sshSession.StdoutPipe()
    if err != nil {
        return err
    }
    
    stderr, err := sshSession.StderrPipe()
    if err != nil {
        return err
    }
    
    // 创建组合输出
    ts.comboOutput = &wsWriterWrapper{
        writer: io.MultiWriter(ts.recorder),
    }
    
    // 启动shell
    if err := sshSession.Shell(); err != nil {
        return err
    }
    
    // 启动数据转发
    go ts.handleOutput(stdout, stderr)
    
    return nil
}
```

### 5. 会话录制功能 (recorder.go)

#### 5.1 录制器接口
```go
type Recorder interface {
    Write(p []byte) (n int, err error)
    Close() error
}
```

#### 5.2 文件录制器实现
```go
type FileRecorder struct {
    file     *os.File
    filename string
    startTime time.Time
}

func (fr *FileRecorder) Write(p []byte) (n int, err error) {
    timestamp := time.Since(fr.startTime).Milliseconds()
    record := fmt.Sprintf("[%d] %s", timestamp, string(p))
    return fr.file.WriteString(record)
}
```

### 6. 连接统计和监控 (stats.go)

#### 6.1 统计信息结构
```go
type MachineStats struct {
    MachineId      uint64 `json:"machineId"`
    MachineName    string `json:"machineName"`
    
    // 连接统计
    TotalConnections    int64 `json:"totalConnections"`
    ActiveConnections   int64 `json:"activeConnections"`
    FailedConnections   int64 `json:"failedConnections"`
    
    // 流量统计
    BytesTransferred    int64 `json:"bytesTransferred"`
    BytesReceived       int64 `json:"bytesReceived"`
    BytesSent           int64 `json:"bytesSent"`
    
    // 会话统计
    TotalSessions       int64 `json:"totalSessions"`
    ActiveSessions      int64 `json:"activeSessions"`
    AverageSessionTime  int64 `json:"averageSessionTime"`
    
    // 时间信息
    LastConnectTime     time.Time `json:"lastConnectTime"`
    LastDisconnectTime  time.Time `json:"lastDisconnectTime"`
    CreatedAt          time.Time `json:"createdAt"`
    UpdatedAt          time.Time `json:"updatedAt"`
}
```

#### 6.2 实时监控
```go
func (ms *MachineStats) RecordConnection() {
    atomic.AddInt64(&ms.TotalConnections, 1)
    atomic.AddInt64(&ms.ActiveConnections, 1)
    ms.LastConnectTime = time.Now()
    ms.UpdatedAt = time.Now()
}

func (ms *MachineStats) RecordDisconnection() {
    atomic.AddInt64(&ms.ActiveConnections, -1)
    ms.LastDisconnectTime = time.Now()
    ms.UpdatedAt = time.Now()
}

func (ms *MachineStats) RecordTraffic(sent, received int64) {
    atomic.AddInt64(&ms.BytesSent, sent)
    atomic.AddInt64(&ms.BytesReceived, received)
    atomic.AddInt64(&ms.BytesTransferred, sent+received)
    ms.UpdatedAt = time.Now()
}
```

## 高级特性

### 1. 连接池管理

#### 1.1 连接复用策略
- **会话复用**: 相同目标机器的多个会话共享 SSH 连接
- **隧道复用**: 相同路径的隧道复用端口转发
- **智能清理**: 基于最后使用时间的自动清理机制

#### 1.2 连接池实现
```go
type ConnectionPool struct {
    connections map[string]*PooledConnection
    mutex       sync.RWMutex
    maxIdle     int
    maxActive   int
    idleTimeout time.Duration
}

type PooledConnection struct {
    client      *ssh.Client
    machine     *MachineInfo
    lastUsed    time.Time
    useCount    int64
    created     time.Time
}
```

### 2. 故障恢复机制

#### 2.1 自动重连
```go
func (ts *TerminalSession) handleReconnect() {
    for {
        select {
        case <-ts.ctx.Done():
            return
        case <-time.After(30 * time.Second):
            if ts.isDisconnected() {
                if err := ts.reconnect(); err != nil {
                    logx.Errorf("reconnect failed: %v", err)
                } else {
                    logx.Info("reconnected successfully")
                }
            }
        }
    }
}
```

#### 2.2 连接健康检查
```go
func (cp *ConnectionPool) healthCheck() {
    ticker := time.NewTicker(time.Minute)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            cp.mutex.Lock()
            for key, conn := range cp.connections {
                if time.Since(conn.lastUsed) > cp.idleTimeout {
                    conn.client.Close()
                    delete(cp.connections, key)
                }
            }
            cp.mutex.Unlock()
        }
    }
}
```

### 3. 安全增强

#### 3.1 认证增强
- **多因子认证**: 支持密码 + 私钥双重认证
- **证书验证**: 主机密钥验证和管理
- **会话加密**: 端到端加密通信

#### 3.2 访问控制
```go
func (ts *TerminalSession) checkPermission(command string) bool {
    // 命令白名单检查
    if ts.machine.IsCommandRestricted(command) {
        return false
    }
    
    // 用户权限检查
    if !ts.user.HasPermission("execute", ts.machine.Id) {
        return false
    }
    
    return true
}
```

### 4. 性能优化

#### 4.1 数据压缩
```go
// SSH 连接配置压缩
config.ClientConfig.Config.SetDefaults()
config.ClientConfig.Config.Ciphers = []string{
    "aes128-ctr", "aes192-ctr", "aes256-ctr",
}
config.ClientConfig.Config.KeyExchanges = []string{
    "curve25519-sha256@libssh.org",
    "ecdh-sha2-nistp256",
}
```

#### 4.2 缓冲优化
```go
type BufferedWriter struct {
    writer io.Writer
    buffer []byte
    size   int
    flush  chan struct{}
}

func (bw *BufferedWriter) Write(p []byte) (n int, err error) {
    if len(p) > len(bw.buffer)-bw.size {
        bw.Flush()
    }
    
    copy(bw.buffer[bw.size:], p)
    bw.size += len(p)
    
    select {
    case bw.flush <- struct{}{}:
    default:
    }
    
    return len(p), nil
}
```

## 最佳实践和运维

### 1. 配置优化
- **连接超时**: 合理设置连接和读写超时
- **重试策略**: 指数退避的重连机制
- **资源限制**: 限制并发连接数和会话数

### 2. 监控告警
- **连接状态监控**: 实时监控连接健康状态
- **性能指标**: 监控延迟、吞吐量等关键指标
- **异常告警**: 连接失败、异常断开等告警

### 3. 安全合规
- **审计日志**: 完整的连接和操作日志
- **访问控制**: 基于角色的访问控制
- **合规要求**: 满足企业安全合规要求

这个 SSH 隧道和连接管理系统提供了企业级的远程访问解决方案，支持复杂的网络拓扑和严格的安全要求，是现代 DevOps 平台的重要组成部分。 