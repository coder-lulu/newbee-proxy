package db

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"sync"
	"time"

    "github.com/coder-lulu/newbee-proxy/plugins/common"
)

// DbConnectionImpl 数据库连接实现
type DbConnectionImpl struct {
	id         string
	target     string
	protocol   string
	status     common.ConnectionStatus
	createdAt  time.Time
	lastActive time.Time
	metadata   map[string]string

	// 数据库特定字段
	dbInfo      *DbInfo
	db          *sql.DB
	config      *DbConfig
	credentials *common.Credentials

	// 统计信息
	stats *ConnectionStats
	mutex sync.RWMutex

	// WebSocket支持
	wsWriter io.Writer
	wsReader io.Reader
	session  common.Session
}

// NewDbConnection 创建新的数据库连接
func NewDbConnection(id, target string, config *DbConfig, credentials *common.Credentials) *DbConnectionImpl {
	return &DbConnectionImpl{
		id:          id,
		target:      target,
		protocol:    string(config.Type),
		status:      common.StatusConnecting,
		createdAt:   time.Now(),
		lastActive:  time.Now(),
		metadata:    make(map[string]string),
		config:      config,
		credentials: credentials,
		stats: &ConnectionStats{
			LastActivity: time.Now(),
		},
	}
}

// 实现 common.Connection 接口

func (c *DbConnectionImpl) ID() string {
	return c.id
}

func (c *DbConnectionImpl) Target() string {
	return c.target
}

func (c *DbConnectionImpl) Protocol() string {
	return c.protocol
}

func (c *DbConnectionImpl) Status() common.ConnectionStatus {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.status
}

func (c *DbConnectionImpl) CreatedAt() time.Time {
	return c.createdAt
}

func (c *DbConnectionImpl) LastActiveAt() time.Time {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.lastActive
}

func (c *DbConnectionImpl) Write(data []byte) (int, error) {
	c.mutex.Lock()
	c.lastActive = time.Now()
	// 更新最后活跃时间
	c.lastActive = time.Now()
	c.mutex.Unlock()

	if c.wsWriter != nil {
		return c.wsWriter.Write(data)
	}

	return len(data), nil
}

func (c *DbConnectionImpl) Read(data []byte) (int, error) {
	if c.wsReader != nil {
		n, err := c.wsReader.Read(data)
		if err == nil {
			c.mutex.Lock()
			c.lastActive = time.Now()
			// 更新统计信息
			c.mutex.Unlock()
		}
		return n, err
	}

	return 0, fmt.Errorf("no reader available")
}

func (c *DbConnectionImpl) SetWebSocketWriter(writer io.Writer) error {
	c.wsWriter = writer
	return nil
}

func (c *DbConnectionImpl) SetWebSocketReader(reader io.Reader) error {
	c.wsReader = reader
	return nil
}

func (c *DbConnectionImpl) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.db != nil {
		err := c.db.Close()
		c.db = nil
		c.status = common.StatusDisconnected
		return err
	}

	c.status = common.StatusDisconnected
	return nil
}

func (c *DbConnectionImpl) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.status == common.StatusConnected && c.db != nil
}

func (c *DbConnectionImpl) GetSession() common.Session {
	return c.session
}

func (c *DbConnectionImpl) GetMetadata() map[string]string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	metadata := make(map[string]string)
	for k, v := range c.metadata {
		metadata[k] = v
	}

	return metadata
}

func (c *DbConnectionImpl) SetMetadata(key, value string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.metadata[key] = value
}

// 实现 DbConnection 接口

func (c *DbConnectionImpl) GetDbInfo() *DbInfo {
	return c.dbInfo
}

func (c *DbConnectionImpl) ExecuteQuery(ctx context.Context, sql string, args ...any) (*QueryResult, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("connection not established")
	}

	c.mutex.Lock()
	c.lastActive = time.Now()
	c.stats.TotalQueries++
	c.mutex.Unlock()

	startTime := time.Now()

	// 调试日志: 记录SQL执行前的状态
	LogSQL("query_start", c.id, sql, 0, 0, nil, "system")

	rows, err := c.db.QueryContext(ctx, sql, args...)
	if err != nil {
		// 调试日志: 记录SQL执行错误
		LogSQL("query_error", c.id, sql, time.Since(startTime), 0, err, "system")
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	// 获取列信息
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	// 构建列信息
	columnInfos := make([]*ColumnInfo, len(columns))
	for i, col := range columns {
		columnInfos[i] = &ColumnInfo{
			Name:         col,
			Type:         columnTypes[i].DatabaseTypeName(),
			DatabaseType: columnTypes[i].DatabaseTypeName(),
		}

		if length, ok := columnTypes[i].Length(); ok {
			columnInfos[i].Length = length
		}

		if precision, scale, ok := columnTypes[i].DecimalSize(); ok {
			columnInfos[i].Precision = int(precision)
			columnInfos[i].Scale = int(scale)
		}

		columnInfos[i].Nullable, _ = columnTypes[i].Nullable()
	}

	// 读取数据行
	var resultRows []map[string]interface{}
	for rows.Next() {
		// 创建扫描目标
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// 构建行数据
		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}

		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	// 更新统计信息
	c.mutex.Lock()
	c.stats.AvgQueryTime = (c.stats.AvgQueryTime + time.Since(startTime)) / 2
	c.mutex.Unlock()

	return &QueryResult{
		Columns: columnInfos,
		Rows:    resultRows,
		Count:   len(resultRows),
		HasMore: false,
	}, nil
}

func (c *DbConnectionImpl) ExecuteUpdate(ctx context.Context, sql string, args ...any) (*ExecResult, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("connection not established")
	}

	c.mutex.Lock()
	c.lastActive = time.Now()
	c.stats.TotalExecs++
	c.mutex.Unlock()

	startTime := time.Now()

	// 调试日志: 记录SQL执行前的状态
	LogSQL("update_start", c.id, sql, 0, 0, nil, "system")

	result, err := c.db.ExecContext(ctx, sql, args...)
	if err != nil {
		// 调试日志: 记录SQL执行错误
		LogSQL("update_error", c.id, sql, time.Since(startTime), 0, err, "system")
		return nil, fmt.Errorf("update execution failed: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	lastInsertId, _ := result.LastInsertId()

	// 更新统计信息
	c.mutex.Lock()
	c.stats.AvgExecTime = (c.stats.AvgExecTime + time.Since(startTime)) / 2
	c.mutex.Unlock()

	return &ExecResult{
		SQL:           sql,
		RowsAffected:  rowsAffected,
		LastInsertID:  lastInsertId,
		ExecutionTime: time.Since(startTime),
		ExecutedAt:    time.Now(),
	}, nil
}

func (c *DbConnectionImpl) BeginTransaction() (Transaction, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("connection not established")
	}

	tx, err := c.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	c.mutex.Lock()
	c.stats.TotalTransactions++
	c.mutex.Unlock()

	return &TransactionImpl{
		tx:   tx,
		conn: c,
	}, nil
}

func (c *DbConnectionImpl) GetTableColumns(ctx context.Context, database, table string) ([]*ColumnInfo, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("connection not established")
	}

	// 根据数据库类型使用不同的查询语句
	var query string
	var args []interface{}

	switch c.config.Type {
	case DbTypeMySQL:
		query = `
			SELECT 
				COLUMN_NAME,
				DATA_TYPE,
				IS_NULLABLE,
				COLUMN_DEFAULT,
				CHARACTER_MAXIMUM_LENGTH,
				NUMERIC_PRECISION,
				NUMERIC_SCALE,
				COLUMN_KEY,
				EXTRA,
				COLUMN_COMMENT
			FROM INFORMATION_SCHEMA.COLUMNS 
			WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
			ORDER BY ORDINAL_POSITION`
		args = []interface{}{database, table}

	case DbTypePostgreSQL:
		query = `
			SELECT 
				column_name,
				data_type,
				is_nullable,
				column_default,
				character_maximum_length,
				numeric_precision,
				numeric_scale,
				'' as column_key,
				'' as extra,
				'' as column_comment
			FROM information_schema.columns 
			WHERE table_schema = $1 AND table_name = $2
			ORDER BY ordinal_position`
		args = []interface{}{database, table}

	default:
		return nil, fmt.Errorf("unsupported database type for column info: %s", c.config.Type)
	}

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query column info: %w", err)
	}
	defer rows.Close()

	var columns []*ColumnInfo
	for rows.Next() {
		var col ColumnInfo
		var nullable, columnKey, extra string
		var length, precision, scale sql.NullInt64
		var defaultValue sql.NullString

		err := rows.Scan(
			&col.Name,
			&col.Type,
			&nullable,
			&defaultValue,
			&length,
			&precision,
			&scale,
			&columnKey,
			&extra,
			&col.Comment,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan column info: %w", err)
		}

		col.Nullable = nullable == "YES"
		col.IsPrimaryKey = columnKey == "PRI"
		col.IsAutoIncr = extra == "auto_increment"

		if defaultValue.Valid {
			col.DefaultValue = defaultValue.String
		}

		if length.Valid {
			col.Length = length.Int64
		}

		if precision.Valid {
			col.Precision = int(precision.Int64)
		}

		if scale.Valid {
			col.Scale = int(scale.Int64)
		}

		columns = append(columns, &col)
	}

	return columns, nil
}

func (c *DbConnectionImpl) GetDatabases(ctx context.Context) ([]string, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("connection not established")
	}

	var query string

	switch c.config.Type {
	case DbTypeMySQL:
		query = "SHOW DATABASES"
	case DbTypePostgreSQL:
		query = "SELECT datname FROM pg_database WHERE datistemplate = false"
	default:
		return nil, fmt.Errorf("unsupported database type for database list: %s", c.config.Type)
	}

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query databases: %w", err)
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			return nil, fmt.Errorf("failed to scan database name: %w", err)
		}
		databases = append(databases, dbName)
	}

	return databases, nil
}

func (c *DbConnectionImpl) GetTables(ctx context.Context, database string) ([]*TableInfo, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("connection not established")
	}

	var query string
	var args []interface{}

	switch c.config.Type {
	case DbTypeMySQL:
		query = `
			SELECT 
				TABLE_NAME,
				TABLE_TYPE,
				ENGINE,
				TABLE_COLLATION,
				TABLE_COMMENT,
				TABLE_ROWS,
				DATA_LENGTH + INDEX_LENGTH
			FROM INFORMATION_SCHEMA.TABLES 
			WHERE TABLE_SCHEMA = ?`
		args = []interface{}{database}

	case DbTypePostgreSQL:
		query = `
			SELECT 
				tablename,
				'BASE TABLE' as table_type,
				'' as engine,
				'' as table_collation,
				'' as table_comment,
				0 as table_rows,
				0 as table_size
			FROM pg_tables 
			WHERE schemaname = $1`
		args = []interface{}{database}

	default:
		return nil, fmt.Errorf("unsupported database type for table list: %s", c.config.Type)
	}

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []*TableInfo
	for rows.Next() {
		var table TableInfo
		var engine, charset sql.NullString
		var tableRows, size sql.NullInt64

		err := rows.Scan(
			&table.Name,
			&table.Type,
			&engine,
			&charset,
			&table.Comment,
			&tableRows,
			&size,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan table info: %w", err)
		}

		if engine.Valid {
			table.Engine = engine.String
		}

		if charset.Valid {
			table.Charset = charset.String
		}

		if tableRows.Valid {
			table.Rows = tableRows.Int64
		}

		if size.Valid {
			table.Size = size.Int64
		}

		tables = append(tables, &table)
	}

	return tables, nil
}

func (c *DbConnectionImpl) SetDatabase(database string) error {
	if !c.IsConnected() {
		return fmt.Errorf("connection not established")
	}

	var query string

	switch c.config.Type {
	case DbTypeMySQL:
		query = fmt.Sprintf("USE `%s`", database)
	case DbTypePostgreSQL:
		// PostgreSQL 需要重新连接到指定数据库
		return fmt.Errorf("PostgreSQL requires reconnection to change database")
	default:
		return fmt.Errorf("unsupported database type for database change: %s", c.config.Type)
	}

	_, err := c.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to change database: %w", err)
	}

	c.dbInfo.Database = database
	return nil
}

func (c *DbConnectionImpl) Ping(ctx context.Context) error {
	if c.db == nil {
		return fmt.Errorf("database connection not established")
	}

	return c.db.PingContext(ctx)
}

func (c *DbConnectionImpl) GetStats() *ConnectionStats {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	stats := *c.stats
	if c.db != nil {
		dbStats := c.db.Stats()
		stats.OpenConnections = dbStats.OpenConnections
		stats.InUseConnections = dbStats.InUse
		stats.IdleConnections = dbStats.Idle
	}

	return &stats
}

// 设置数据库连接
func (c *DbConnectionImpl) SetDB(db *sql.DB, dbInfo *DbInfo) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.db = db
	c.dbInfo = dbInfo
	c.status = common.StatusConnected
	c.lastActive = time.Now()
}

// TransactionImpl 事务实现
type TransactionImpl struct {
	tx   *sql.Tx
	conn *DbConnectionImpl
}

func (t *TransactionImpl) ExecuteQuery(ctx context.Context, sql string, args ...any) (*QueryResult, error) {
	rows, err := t.tx.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("transaction query failed: %w", err)
	}
	defer rows.Close()

	// 获取列信息
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	// 构建列信息
	columnInfos := make([]*ColumnInfo, len(columns))
	for i, col := range columns {
		columnInfos[i] = &ColumnInfo{
			Name:         col,
			Type:         columnTypes[i].DatabaseTypeName(),
			DatabaseType: columnTypes[i].DatabaseTypeName(),
		}
	}

	// 读取数据行
	var resultRows []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}

		resultRows = append(resultRows, row)
	}

	return &QueryResult{
		Columns: columnInfos,
		Rows:    resultRows,
		Count:   len(resultRows),
		HasMore: false,
	}, nil
}

func (t *TransactionImpl) ExecuteUpdate(ctx context.Context, sql string, args ...any) (*ExecResult, error) {
	startTime := time.Now()

	result, err := t.tx.ExecContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("transaction update failed: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	lastInsertId, _ := result.LastInsertId()

	return &ExecResult{
		SQL:           sql,
		RowsAffected:  rowsAffected,
		LastInsertID:  lastInsertId,
		ExecutionTime: time.Since(startTime),
		ExecutedAt:    time.Now(),
	}, nil
}

func (t *TransactionImpl) Commit() error {
	return t.tx.Commit()
}

func (t *TransactionImpl) Rollback() error {
	return t.tx.Rollback()
}
