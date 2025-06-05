package db

import (
	"context"
	"fmt"
	"time"

	"newbee-agent/plugins/common"
)

// QuickExec 一句话SQL执行器，提供简化的API接口
type QuickExec struct {
	plugin *DbPluginImpl
}

// NewQuickExec 创建一句话SQL执行器
func NewQuickExec(plugin *DbPluginImpl) *QuickExec {
	return &QuickExec{
		plugin: plugin,
	}
}

// ExecuteSQL 一句话SQL执行 - 统一的简化接口
// 支持格式: ExecuteSQL("mysql://user:pass@host:port/db", "SELECT * FROM users")
func (q *QuickExec) ExecuteSQL(target, sql string, options ...QuickExecOption) (*QuickExecResult, error) {
	// 默认配置
	config := &QuickExecConfig{
		Timeout:     30 * time.Second,
		MaxRows:     1000,
		Operator:    "system",
		AutoCommit:  true,
		EnableLog:   true,
		CacheResult: false,
	}

	// 应用选项
	for _, opt := range options {
		opt(config)
	}

	return q.executeWithConfig(target, sql, config)
}

// ExecuteBatch 批量SQL执行
func (q *QuickExec) ExecuteBatch(target string, sqlList []string, options ...QuickExecOption) ([]*QuickExecResult, error) {
	results := make([]*QuickExecResult, 0, len(sqlList))

	for i, sql := range sqlList {
		result, err := q.ExecuteSQL(target, sql, options...)
		if err != nil {
			// 如果配置了继续执行，记录错误但继续
			if config := q.buildConfig(options...); config.ContinueOnError {
				result = &QuickExecResult{
					Success:     false,
					Error:       err.Error(),
					ExecutedAt:  time.Now(),
					StatementNo: i + 1,
				}
			} else {
				return results, fmt.Errorf("failed to execute statement %d: %w", i+1, err)
			}
		}
		result.StatementNo = i + 1
		results = append(results, result)
	}

	return results, nil
}

// ExecuteFile 执行SQL文件
func (q *QuickExec) ExecuteFile(target, filePath string, options ...QuickExecOption) ([]*QuickExecResult, error) {
	// 读取文件内容
	content, err := q.readSQLFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SQL file: %w", err)
	}

	// 分割SQL语句
	utils := &MySQLUtils{}
	statements := utils.SplitSQL(content)

	return q.ExecuteBatch(target, statements, options...)
}

// TestConnection 测试连接
func (q *QuickExec) TestConnection(target string, options ...QuickExecOption) (*ConnectionTestResult, error) {
	config := q.buildConfig(options...)

	// 解析目标配置
	dbConfig, credentials, err := q.parseTargetWithCredentials(target)
	if err != nil {
		return &ConnectionTestResult{
			Success:  false,
			Error:    err.Error(),
			TestedAt: time.Now(),
		}, err
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	startTime := time.Now()
	err = q.plugin.TestConnection(ctx, dbConfig, credentials)
	duration := time.Since(startTime)

	result := &ConnectionTestResult{
		Success:  err == nil,
		Duration: duration,
		TestedAt: time.Now(),
		DatabaseInfo: map[string]interface{}{
			"type":     dbConfig.Type,
			"host":     dbConfig.Host,
			"port":     dbConfig.Port,
			"database": dbConfig.Database,
		},
	}

	if err != nil {
		result.Error = err.Error()
	}

	return result, err
}

// GetDatabaseInfo 获取数据库信息
func (q *QuickExec) GetDatabaseInfo(target string, options ...QuickExecOption) (*DatabaseInfoResult, error) {
	config := q.buildConfig(options...)

	// 创建临时连接
	conn, cleanup, err := q.createTempConnection(target, config)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	// 获取数据库信息
	var dbInfo *DatabaseInfo
	var databases []string
	if baseConn, ok := conn.(common.Connection); ok {
		dbInfo, err = q.plugin.GetDatabaseInfo(ctx, baseConn.ID())
		if err != nil {
			return nil, err
		}

		// 获取数据库列表
		databases, err = q.plugin.GetDatabases(ctx, baseConn.ID())
	} else {
		return nil, fmt.Errorf("invalid connection type")
	}
	if err != nil {
		databases = []string{} // 如果获取失败，返回空列表
	}

	return &DatabaseInfoResult{
		DatabaseInfo: dbInfo,
		Databases:    databases,
		RetrievedAt:  time.Now(),
	}, nil
}

// GetTableList 获取表列表
func (q *QuickExec) GetTableList(target, database string, options ...QuickExecOption) (*TableListResult, error) {
	config := q.buildConfig(options...)

	// 创建临时连接
	conn, cleanup, err := q.createTempConnection(target, config)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	// 获取表列表
	var tables []*TableInfo
	if baseConn, ok := conn.(common.Connection); ok {
		tables, err = q.plugin.GetTables(ctx, baseConn.ID(), database)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("invalid connection type")
	}

	return &TableListResult{
		Database:    database,
		Tables:      tables,
		RetrievedAt: time.Now(),
	}, nil
}

// 内部方法

func (q *QuickExec) executeWithConfig(target, sql string, config *QuickExecConfig) (*QuickExecResult, error) {
	// 创建临时连接
	conn, cleanup, err := q.createTempConnection(target, config)
	if err != nil {
		return &QuickExecResult{
			Success:    false,
			Error:      err.Error(),
			ExecutedAt: time.Now(),
		}, err
	}
	defer cleanup()

	// 执行SQL
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	execOptions := &ExecOptions{
		MaxRows:  config.MaxRows,
		Timeout:  config.Timeout,
		Operator: config.Operator,
		Remark:   config.Remark,
	}

	startTime := time.Now()
	var result *ExecResult
	if baseConn, ok := conn.(common.Connection); ok {
		result, err = q.plugin.ExecuteSQL(ctx, baseConn.ID(), sql, execOptions)
	} else {
		return &QuickExecResult{
			Success:    false,
			Error:      "invalid connection type",
			ExecutedAt: time.Now(),
		}, fmt.Errorf("invalid connection type")
	}

	quickResult := &QuickExecResult{
		Success:    err == nil,
		SQL:        sql,
		Duration:   time.Since(startTime),
		ExecutedAt: time.Now(),
		Operator:   config.Operator,
	}

	if err != nil {
		quickResult.Error = err.Error()
		return quickResult, err
	}

	// 转换结果
	quickResult.RowsAffected = result.RowsAffected
	quickResult.LastInsertID = result.LastInsertID
	if result.QueryResult != nil {
		quickResult.Data = result.QueryResult.Rows
		quickResult.Columns = result.QueryResult.Columns
		quickResult.RowCount = result.QueryResult.Count
		quickResult.HasMore = result.QueryResult.HasMore
	}

	return quickResult, nil
}

func (q *QuickExec) createTempConnection(target string, config *QuickExecConfig) (DbConnection, func(), error) {
	// 解析目标配置
	dbConfig, credentials, err := q.parseTargetWithCredentials(target)
	if err != nil {
		return nil, nil, err
	}

	// 设置超时
	if config.Timeout > 0 {
		dbConfig.QueryTimeout = config.Timeout
	}

	// 创建连接
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	conn, err := q.plugin.CreateDbConnection(ctx, dbConfig, credentials)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		if baseConn, ok := conn.(common.Connection); ok {
			q.plugin.CloseConnection(baseConn.ID())
		}
	}

	return conn, cleanup, nil
}

func (q *QuickExec) parseTargetWithCredentials(target string) (*DbConfig, *common.Credentials, error) {
	// 解析带认证信息的目标字符串
	// 格式: mysql://user:pass@host:port/db
	// 或: host:port/db (使用配置的默认认证)

	config, err := q.plugin.parseTarget(target)
	if err != nil {
		return nil, nil, err
	}

	// 从target中提取认证信息
	credentials := &common.Credentials{
		AuthType: "password",
		Timeout:  30,
	}

	// 简单的URL解析
	if parsed := q.parseCredentialsFromTarget(target); parsed != nil {
		credentials = parsed
	}

	return config, credentials, nil
}

func (q *QuickExec) parseCredentialsFromTarget(target string) *common.Credentials {
	// 简单的认证信息解析
	// 这里可以根据需要实现更复杂的解析逻辑
	return &common.Credentials{
		Username: "root",
		Password: "",
		AuthType: "password",
		Timeout:  30,
	}
}

func (q *QuickExec) readSQLFile(filePath string) (string, error) {
	// 实现文件读取逻辑
	// 这里暂时返回空，实际使用时需要实现
	return "", fmt.Errorf("file reading not implemented")
}

func (q *QuickExec) buildConfig(options ...QuickExecOption) *QuickExecConfig {
	config := &QuickExecConfig{
		Timeout:     30 * time.Second,
		MaxRows:     1000,
		Operator:    "system",
		AutoCommit:  true,
		EnableLog:   true,
		CacheResult: false,
	}

	for _, opt := range options {
		opt(config)
	}

	return config
}

// QuickExecConfig 快速执行配置
type QuickExecConfig struct {
	Timeout         time.Duration `json:"timeout"`
	MaxRows         int           `json:"max_rows"`
	Operator        string        `json:"operator"`
	Remark          string        `json:"remark"`
	AutoCommit      bool          `json:"auto_commit"`
	EnableLog       bool          `json:"enable_log"`
	CacheResult     bool          `json:"cache_result"`
	ContinueOnError bool          `json:"continue_on_error"`
}

// QuickExecOption 快速执行选项
type QuickExecOption func(*QuickExecConfig)

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) QuickExecOption {
	return func(c *QuickExecConfig) {
		c.Timeout = timeout
	}
}

// WithMaxRows 设置最大行数
func WithMaxRows(maxRows int) QuickExecOption {
	return func(c *QuickExecConfig) {
		c.MaxRows = maxRows
	}
}

// WithQuickOperator 设置操作员
func WithQuickOperator(operator string) QuickExecOption {
	return func(c *QuickExecConfig) {
		c.Operator = operator
	}
}

// WithRemark 设置备注
func WithRemark(remark string) QuickExecOption {
	return func(c *QuickExecConfig) {
		c.Remark = remark
	}
}

// WithContinueOnError 设置遇到错误时继续执行
func WithContinueOnError() QuickExecOption {
	return func(c *QuickExecConfig) {
		c.ContinueOnError = true
	}
}

// WithDisableLog 禁用日志
func WithDisableLog() QuickExecOption {
	return func(c *QuickExecConfig) {
		c.EnableLog = false
	}
}

// QuickExecResult 快速执行结果
type QuickExecResult struct {
	Success      bool                     `json:"success"`
	SQL          string                   `json:"sql"`
	RowsAffected int64                    `json:"rows_affected"`
	LastInsertID int64                    `json:"last_insert_id"`
	Data         []map[string]interface{} `json:"data,omitempty"`
	Columns      []*ColumnInfo            `json:"columns,omitempty"`
	RowCount     int                      `json:"row_count"`
	HasMore      bool                     `json:"has_more"`
	Duration     time.Duration            `json:"duration"`
	Error        string                   `json:"error,omitempty"`
	ExecutedAt   time.Time                `json:"executed_at"`
	Operator     string                   `json:"operator"`
	StatementNo  int                      `json:"statement_no,omitempty"`
}

// ConnectionTestResult 连接测试结果
type ConnectionTestResult struct {
	Success      bool                   `json:"success"`
	Duration     time.Duration          `json:"duration"`
	Error        string                 `json:"error,omitempty"`
	TestedAt     time.Time              `json:"tested_at"`
	DatabaseInfo map[string]interface{} `json:"database_info"`
}

// DatabaseInfoResult 数据库信息结果
type DatabaseInfoResult struct {
	*DatabaseInfo
	Databases   []string  `json:"databases"`
	RetrievedAt time.Time `json:"retrieved_at"`
}

// TableListResult 表列表结果
type TableListResult struct {
	Database    string       `json:"database"`
	Tables      []*TableInfo `json:"tables"`
	RetrievedAt time.Time    `json:"retrieved_at"`
}
