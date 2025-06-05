package db

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"newbee-agent/plugins/common"
)

// SimpleLogger 简单的日志记录器实现
type SimpleLogger struct{}

func (l *SimpleLogger) Info(msg string) {
	log.Printf("[INFO] DB Plugin: %s", msg)
}

func (l *SimpleLogger) Warn(msg string) {
	log.Printf("[WARN] DB Plugin: %s", msg)
}

// DbPluginImpl 数据库插件实现
type DbPluginImpl struct {
	config      map[string]interface{}
	connections map[string]DbConnection
	sessions    map[string]WebSocketSession
	connManager *ConnectionManager
	mutex       sync.RWMutex
	running     bool
	metrics     *common.PluginMetrics
	status      *common.PluginStatus
}

// NewDbPlugin 创建新的数据库插件实例
func NewDbPlugin() *DbPluginImpl {
	// 创建简单的日志记录器
	logger := &SimpleLogger{}

	// 创建连接管理器
	connManager := NewConnectionManager(DefaultConnectionManagerConfig(), logger)

	return &DbPluginImpl{
		connections: make(map[string]DbConnection),
		sessions:    make(map[string]WebSocketSession),
		connManager: connManager,
		metrics:     &common.PluginMetrics{},
		status: &common.PluginStatus{
			Name:     "db",
			Version:  "1.0.0",
			Status:   "stopped",
			LoadedAt: time.Now(),
		},
	}
}

// 实现 common.ProtocolPlugin 接口

func (p *DbPluginImpl) Name() string {
	return "db"
}

func (p *DbPluginImpl) Version() string {
	return "1.0.0"
}

func (p *DbPluginImpl) SupportedProtocols() []string {
	return []string{"mysql", "postgresql", "mssql", "oracle", "sqlite", "dm"}
}

func (p *DbPluginImpl) Description() string {
	return "Database connection and management plugin supporting multiple database types"
}

func (p *DbPluginImpl) Initialize(config map[string]interface{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.config = config
	p.status.Config = config
	p.status.Status = "initialized"

	return nil
}

func (p *DbPluginImpl) Start() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.running {
		return fmt.Errorf("plugin already running")
	}

	p.running = true
	now := time.Now()
	p.status.Status = "running"
	p.status.StartedAt = &now

	return nil
}

func (p *DbPluginImpl) Stop() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.running {
		return fmt.Errorf("plugin not running")
	}

	// 关闭连接管理器
	if p.connManager != nil {
		p.connManager.Shutdown()
	}

	// 关闭所有连接
	for _, conn := range p.connections {
		conn.Close()
	}

	// 停止所有会话
	for _, session := range p.sessions {
		session.Stop()
	}

	p.running = false
	now := time.Now()
	p.status.Status = "stopped"
	p.status.StoppedAt = &now

	return nil
}

func (p *DbPluginImpl) IsRunning() bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.running
}

func (p *DbPluginImpl) CreateConnection(ctx context.Context, target string, credentials *common.Credentials) (common.Connection, error) {
	// 解析target为数据库配置
	config, err := p.parseTarget(target)
	if err != nil {
		return nil, fmt.Errorf("failed to parse target: %w", err)
	}

	return p.CreateDbConnection(ctx, config, credentials)
}

func (p *DbPluginImpl) CloseConnection(connectionId string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	conn, exists := p.connections[connectionId]
	if !exists {
		return fmt.Errorf("connection not found: %s", connectionId)
	}

	err := conn.Close()
	delete(p.connections, connectionId)

	return err
}

func (p *DbPluginImpl) GetConnection(connectionId string) (common.Connection, bool) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	conn, exists := p.connections[connectionId]
	return conn, exists
}

func (p *DbPluginImpl) ListConnections() []string {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	connections := make([]string, 0, len(p.connections))
	for id := range p.connections {
		connections = append(connections, id)
	}

	return connections
}

func (p *DbPluginImpl) GetStatus() *common.PluginStatus {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	status := *p.status
	status.Connections = len(p.connections)
	status.Sessions = len(p.sessions)

	return &status
}

func (p *DbPluginImpl) GetMetrics() *common.PluginMetrics {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	metrics := *p.metrics
	metrics.ActiveConnections = len(p.connections)
	metrics.ActiveSessions = len(p.sessions)
	metrics.UpdatedAt = time.Now()

	return &metrics
}

func (p *DbPluginImpl) UpdateConfig(config map[string]interface{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.config = config
	p.status.Config = config

	return nil
}

// 实现 DbPlugin 接口

func (p *DbPluginImpl) CreateDbConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) (DbConnection, error) {
	// 创建数据库连接
	conn, err := p.createDbConnection(ctx, config, credentials)
	if err != nil {
		p.metrics.FailedConnections++
		return nil, err
	}

	p.mutex.Lock()
	p.connections[conn.ID()] = conn
	p.metrics.TotalConnections++
	p.mutex.Unlock()

	return conn, nil
}

func (p *DbPluginImpl) TestConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) error {
	conn, err := p.createDbConnection(ctx, config, credentials)
	if err != nil {
		return err
	}
	defer conn.Close()

	return conn.Ping(ctx)
}

func (p *DbPluginImpl) ExecuteSQL(ctx context.Context, connectionId string, sql string, options *ExecOptions) (*ExecResult, error) {
	conn, exists := p.GetConnection(connectionId)
	if !exists {
		return nil, fmt.Errorf("connection not found: %s", connectionId)
	}

	dbConn, ok := conn.(DbConnection)
	if !ok {
		return nil, fmt.Errorf("invalid connection type")
	}

	// 设置超时
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}

	startTime := time.Now()

	// 判断SQL类型并执行
	sqlType := p.detectSQLType(sql)
	var result *ExecResult
	var err error

	switch sqlType {
	case SqlTypeSelect:
		queryResult, queryErr := dbConn.ExecuteQuery(ctx, sql)
		result = &ExecResult{
			SQL:           sql,
			QueryResult:   queryResult,
			ExecutionTime: time.Since(startTime),
			ExecutedAt:    time.Now(),
			Operator:      options.Operator,
			Remark:        options.Remark,
		}
		err = queryErr
	default:
		execResult, execErr := dbConn.ExecuteUpdate(ctx, sql)
		if execResult != nil {
			result = execResult
		} else {
			result = &ExecResult{
				SQL:           sql,
				ExecutionTime: time.Since(startTime),
				ExecutedAt:    time.Now(),
				Operator:      options.Operator,
				Remark:        options.Remark,
			}
		}
		err = execErr
	}

	if err != nil {
		dbErr := WrapError(err, sql)
		result.Error = dbErr.Error()
		p.metrics.TotalErrors++

		// 记录错误日志
		LogSQL("execute", connectionId, sql, time.Since(startTime), result.RowsAffected, dbErr, options.Operator)
	} else {
		// 记录成功日志
		LogSQL("execute", connectionId, sql, time.Since(startTime), result.RowsAffected, nil, options.Operator)
	}

	return result, err
}

func (p *DbPluginImpl) ExecuteSQLFile(ctx context.Context, connectionId string, reader io.Reader, options *ExecOptions) error {
	conn, exists := p.GetConnection(connectionId)
	if !exists {
		return fmt.Errorf("connection not found: %s", connectionId)
	}

	dbConn, ok := conn.(DbConnection)
	if !ok {
		return fmt.Errorf("invalid connection type")
	}

	// 读取SQL文件内容
	sqlContent, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read SQL file: %w", err)
	}

	// 分割SQL语句
	statements := p.splitSQL(string(sqlContent))

	// 如果使用事务，开启事务
	var tx Transaction
	if options.UseTransaction {
		tx, err = dbConn.BeginTransaction()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer func() {
			if err != nil {
				tx.Rollback()
			} else {
				tx.Commit()
			}
		}()
	}

	// 执行每个SQL语句
	for i, stmt := range statements {
		if stmt == "" {
			continue
		}

		// 发送进度信息
		if options.UseWebSocket && options.SessionID != "" {
			progress := &ProgressInfo{
				ID:                 options.SessionID,
				Title:              "Executing SQL File",
				ExecutedStatements: i,
				TotalStatements:    len(statements),
				Percentage:         (i * 100) / len(statements),
				CurrentSQL:         stmt,
			}
			p.sendProgress(options.SessionID, progress)
		}

		if tx != nil {
			_, err = tx.ExecuteUpdate(ctx, stmt)
		} else {
			_, err = dbConn.ExecuteUpdate(ctx, stmt)
		}

		if err != nil {
			return fmt.Errorf("failed to execute statement %d: %w", i+1, err)
		}
	}

	return nil
}

func (p *DbPluginImpl) GetDatabases(ctx context.Context, connectionId string) ([]string, error) {
	conn, exists := p.GetConnection(connectionId)
	if !exists {
		return nil, fmt.Errorf("connection not found: %s", connectionId)
	}

	dbConn, ok := conn.(DbConnection)
	if !ok {
		return nil, fmt.Errorf("invalid connection type")
	}

	return dbConn.GetDatabases(ctx)
}

func (p *DbPluginImpl) GetTables(ctx context.Context, connectionId string, database string) ([]*TableInfo, error) {
	conn, exists := p.GetConnection(connectionId)
	if !exists {
		return nil, fmt.Errorf("connection not found: %s", connectionId)
	}

	dbConn, ok := conn.(DbConnection)
	if !ok {
		return nil, fmt.Errorf("invalid connection type")
	}

	return dbConn.GetTables(ctx, database)
}

func (p *DbPluginImpl) GetTableInfo(ctx context.Context, connectionId string, database, table string) (*TableDetail, error) {
	conn, exists := p.GetConnection(connectionId)
	if !exists {
		return nil, fmt.Errorf("connection not found: %s", connectionId)
	}

	dbConn, ok := conn.(DbConnection)
	if !ok {
		return nil, fmt.Errorf("invalid connection type")
	}

	// 获取表基本信息
	tables, err := dbConn.GetTables(ctx, database)
	if err != nil {
		return nil, err
	}

	var tableInfo *TableInfo
	for _, t := range tables {
		if t.Name == table {
			tableInfo = t
			break
		}
	}

	if tableInfo == nil {
		return nil, fmt.Errorf("table not found: %s", table)
	}

	// 获取列信息
	columns, err := dbConn.GetTableColumns(ctx, database, table)
	if err != nil {
		return nil, err
	}

	return &TableDetail{
		TableInfo: tableInfo,
		Columns:   columns,
		Indexes:   []*IndexInfo{}, // 索引信息获取需要根据数据库类型实现
		DDL:       "",             // DDL获取需要根据数据库类型实现
	}, nil
}

func (p *DbPluginImpl) GetDatabaseInfo(ctx context.Context, connectionId string) (*DatabaseInfo, error) {
	conn, exists := p.GetConnection(connectionId)
	if !exists {
		return nil, fmt.Errorf("connection not found: %s", connectionId)
	}

	dbConn, ok := conn.(DbConnection)
	if !ok {
		return nil, fmt.Errorf("invalid connection type")
	}

	dbInfo := dbConn.GetDbInfo()

	// 获取数据库列表
	databases, err := dbConn.GetDatabases(ctx)
	if err != nil {
		return nil, err
	}

	// 获取表数量
	var tableCount int
	for _, db := range databases {
		tables, err := dbConn.GetTables(ctx, db)
		if err == nil {
			tableCount += len(tables)
		}
	}

	return &DatabaseInfo{
		Name:       dbInfo.Database,
		Version:    dbInfo.Version,
		Charset:    dbInfo.Charset,
		TableCount: tableCount,
		CreatedAt:  time.Now(),
	}, nil
}

// 辅助方法

func (p *DbPluginImpl) parseTarget(target string) (*DbConfig, error) {
	// 支持多种数据库类型的target解析
	if strings.HasPrefix(target, "mysql://") {
		return ParseMySQLTarget(target)
	} else if strings.HasPrefix(target, "postgresql://") || strings.HasPrefix(target, "postgres://") {
		return ParsePostgreSQLTarget(target)
	} else if strings.HasPrefix(target, "mssql://") || strings.HasPrefix(target, "sqlserver://") {
		return ParseMSSQLTarget(target)
	} else if strings.HasPrefix(target, "oracle://") {
		connector := &OracleConnector{}
		config, _, err := connector.ParseTarget(target)
		return config, err
	} else if strings.HasPrefix(target, "sqlite://") || strings.Contains(target, ".db") || strings.Contains(target, ".sqlite") || target == ":memory:" {
		connector := &SQLiteConnector{}
		config, _, err := connector.ParseTarget(target)
		return config, err
	} else if strings.HasPrefix(target, "dm://") {
		connector := &DmConnector{}
		config, _, err := connector.ParseTarget(target)
		return config, err
	} else {
		// 默认尝试MySQL格式
		return ParseMySQLTarget("mysql://" + target)
	}
}

func (p *DbPluginImpl) createDbConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) (DbConnection, error) {
	// 根据数据库类型创建相应的连接
	switch config.Type {
	case DbTypeMySQL:
		return p.createMySQLConnection(ctx, config, credentials)
	case DbTypePostgreSQL:
		return p.createPostgreSQLConnection(ctx, config, credentials)
	case DbTypeMSSQL:
		return p.createMSSQLConnection(ctx, config, credentials)
	case DbTypeOracle:
		return p.createOracleConnection(ctx, config, credentials)
	case DbTypeSQLite:
		return p.createSQLiteConnection(ctx, config, credentials)
	case DbTypeDM:
		return p.createDmConnection(ctx, config, credentials)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", config.Type)
	}
}

func (p *DbPluginImpl) createMySQLConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) (DbConnection, error) {
	connector := &MySQLConnector{}
	return connector.CreateConnection(ctx, config, credentials)
}

func (p *DbPluginImpl) createPostgreSQLConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) (DbConnection, error) {
	connector := &PostgreSQLConnector{}
	return connector.CreateConnection(ctx, config, credentials)
}

func (p *DbPluginImpl) createMSSQLConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) (DbConnection, error) {
	connector := &MSSQLConnector{}
	return connector.CreateConnection(ctx, config, credentials)
}

func (p *DbPluginImpl) createOracleConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) (DbConnection, error) {
	connector := NewOracleConnector(config, credentials)
	db, dbInfo, err := connector.Connect(ctx)
	if err != nil {
		return nil, err
	}

	// 创建连接对象
	connectionId := fmt.Sprintf("oracle_%s_%d_%s_%d", config.Host, config.Port, config.Database, time.Now().Unix())
	target := fmt.Sprintf("oracle://%s:%d/%s", config.Host, config.Port, config.Database)

	conn := NewDbConnection(connectionId, target, config, credentials)
	conn.SetDB(db, dbInfo)

	return conn, nil
}

func (p *DbPluginImpl) createSQLiteConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) (DbConnection, error) {
	connector := NewSQLiteConnector(config, credentials)
	db, dbInfo, err := connector.Connect(ctx)
	if err != nil {
		return nil, err
	}

	// 创建连接对象
	connectionId := fmt.Sprintf("sqlite_%s_%d", strings.ReplaceAll(config.Database, "/", "_"), time.Now().Unix())
	target := fmt.Sprintf("sqlite://%s", config.Database)

	conn := NewDbConnection(connectionId, target, config, credentials)
	conn.SetDB(db, dbInfo)

	return conn, nil
}

func (p *DbPluginImpl) createDmConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) (DbConnection, error) {
	connector := NewDmConnector(config, credentials)
	db, dbInfo, err := connector.Connect(ctx)
	if err != nil {
		return nil, err
	}

	// 创建连接对象
	connectionId := fmt.Sprintf("dm_%s_%d_%s_%d", config.Host, config.Port, config.Database, time.Now().Unix())
	target := fmt.Sprintf("dm://%s:%d/%s", config.Host, config.Port, config.Database)

	conn := NewDbConnection(connectionId, target, config, credentials)
	conn.SetDB(db, dbInfo)

	return conn, nil
}

func (p *DbPluginImpl) detectSQLType(sql string) SqlType {
	utils := &MySQLUtils{}
	return utils.DetectSQLType(sql)
}

func (p *DbPluginImpl) splitSQL(content string) []string {
	utils := &MySQLUtils{}
	return utils.SplitSQL(content)
}

func (p *DbPluginImpl) sendProgress(sessionID string, progress *ProgressInfo) {
	// 通过WebSocket会话发送进度信息
	if session, exists := p.sessions[sessionID]; exists {
		message := &WSMessage{
			Type:      WSMsgTypeProgress,
			Data:      progress,
			Timestamp: time.Now(),
			SessionID: sessionID,
		}
		session.SendMessage(message)
	}
}
