package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"newbee-agent/plugins/common"
	// 达梦数据库驱动 - 使用dm包
	// _ "dm"
)

// DmConnector 达梦数据库连接器
type DmConnector struct {
	config      *DbConfig
	credentials *common.Credentials
}

// NewDmConnector 创建达梦连接器
func NewDmConnector(config *DbConfig, credentials *common.Credentials) *DmConnector {
	return &DmConnector{
		config:      config,
		credentials: credentials,
	}
}

// Connect 建立达梦连接
func (c *DmConnector) Connect(ctx context.Context) (*sql.DB, *DbInfo, error) {
	// 构建达梦连接字符串
	dsn, err := c.buildDSN()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build DM DSN: %w", err)
	}

	LogConnection("connect_attempt", "", &DbInfo{
		Type: DbTypeDM,
		Host: c.config.Host,
		Port: c.config.Port,
	}, 0, nil)

	startTime := time.Now()

	// 打开连接
	db, err := sql.Open("dm", dsn)
	if err != nil {
		duration := time.Since(startTime)
		LogConnection("connect_failed", "", nil, duration, err)
		return nil, nil, fmt.Errorf("failed to open DM connection: %w", err)
	}

	// 配置连接池
	c.configureConnectionPool(db)

	// 测试连接
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		duration := time.Since(startTime)
		LogConnection("ping_failed", "", nil, duration, err)
		return nil, nil, fmt.Errorf("failed to ping DM database: %w", err)
	}

	// 获取数据库信息
	dbInfo, err := c.getDbInfo(ctx, db)
	if err != nil {
		db.Close()
		duration := time.Since(startTime)
		LogConnection("info_failed", "", nil, duration, err)
		return nil, nil, fmt.Errorf("failed to get DM database info: %w", err)
	}

	duration := time.Since(startTime)
	LogConnection("connect_success", "", dbInfo, duration, nil)

	return db, dbInfo, nil
}

// buildDSN 构建达梦连接字符串
func (c *DmConnector) buildDSN() (string, error) {
	if c.config.Host == "" {
		return "", fmt.Errorf("DM host is required")
	}

	if c.config.Port == 0 {
		c.config.Port = 5236 // 达梦默认端口
	}

	if c.credentials.Username == "" {
		return "", fmt.Errorf("DM username is required")
	}

	// 达梦DSN格式: dm://username:password@host:port/database?param1=value1&param2=value2
	// 或者: username/password@host:port:database
	var dsn string

	if c.config.Database != "" {
		dsn = fmt.Sprintf("dm://%s:%s@%s:%d/%s",
			c.credentials.Username,
			c.credentials.Password,
			c.config.Host,
			c.config.Port,
			c.config.Database,
		)
	} else {
		dsn = fmt.Sprintf("dm://%s:%s@%s:%d",
			c.credentials.Username,
			c.credentials.Password,
			c.config.Host,
			c.config.Port,
		)
	}

	// 添加额外参数
	if len(c.config.Params) > 0 {
		params := make([]string, 0, len(c.config.Params))
		for key, value := range c.config.Params {
			params = append(params, fmt.Sprintf("%s=%s", key, value))
		}
		dsn += "?" + strings.Join(params, "&")
	}

	return dsn, nil
}

// configureConnectionPool 配置达梦连接池
func (c *DmConnector) configureConnectionPool(db *sql.DB) {
	if c.config.MaxOpenConns > 0 {
		db.SetMaxOpenConns(c.config.MaxOpenConns)
	} else {
		db.SetMaxOpenConns(50) // 达梦默认值
	}

	if c.config.MaxIdleConns > 0 {
		db.SetMaxIdleConns(c.config.MaxIdleConns)
	} else {
		db.SetMaxIdleConns(10)
	}

	if c.config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(c.config.ConnMaxLifetime)
	} else {
		db.SetConnMaxLifetime(time.Hour)
	}

	if c.config.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(c.config.ConnMaxIdleTime)
	} else {
		db.SetConnMaxIdleTime(30 * time.Minute)
	}
}

// getDbInfo 获取达梦数据库信息
func (c *DmConnector) getDbInfo(ctx context.Context, db *sql.DB) (*DbInfo, error) {
	dbInfo := &DbInfo{
		Type:     DbTypeDM,
		Host:     c.config.Host,
		Port:     c.config.Port,
		Database: c.config.Database,
	}

	// 获取达梦版本
	var version string
	err := db.QueryRowContext(ctx, "SELECT BANNER FROM V$VERSION WHERE ROWNUM = 1").Scan(&version)
	if err != nil {
		// 如果查询失败，尝试其他方式
		err = db.QueryRowContext(ctx, "SELECT @@version").Scan(&version)
		if err != nil {
			version = "Unknown"
		}
	}
	dbInfo.Version = version

	// 获取字符集
	var charset string
	err = db.QueryRowContext(ctx, "SELECT PARA_VALUE FROM V$DM_INI WHERE PARA_NAME = 'CHARSET'").Scan(&charset)
	if err != nil {
		charset = "UTF-8" // 默认字符集
	}
	dbInfo.Charset = charset

	// 获取时区
	var timezone string
	err = db.QueryRowContext(ctx, "SELECT TIMEZONE()").Scan(&timezone)
	if err != nil {
		timezone = "Asia/Shanghai" // 默认时区
	}
	dbInfo.Timezone = timezone

	return dbInfo, nil
}

// ParseTarget 解析达梦目标字符串
func (c *DmConnector) ParseTarget(target string) (*DbConfig, *common.Credentials, error) {
	// 支持的格式:
	// dm://user:pass@host:port/database
	// dm://user:pass@host:port
	// host:port/database
	// host:port

	config := &DbConfig{
		Type:    DbTypeDM,
		Port:    5236,
		Charset: "UTF-8",
		Params:  make(map[string]string),
	}

	credentials := &common.Credentials{
		AuthType: "password",
		Timeout:  30,
	}

	if strings.HasPrefix(target, "dm://") {
		// 解析完整URL
		u, err := url.Parse(target)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid DM URL: %w", err)
		}

		config.Host = u.Hostname()
		if u.Port() != "" {
			if port, err := strconv.Atoi(u.Port()); err == nil {
				config.Port = port
			}
		}

		config.Database = strings.TrimPrefix(u.Path, "/")

		if u.User != nil {
			credentials.Username = u.User.Username()
			if password, ok := u.User.Password(); ok {
				credentials.Password = password
			}
		}

		// 解析查询参数
		for key, values := range u.Query() {
			if len(values) > 0 {
				config.Params[key] = values[0]
			}
		}
	} else {
		// 解析简单格式 host:port/database
		parts := strings.Split(target, "/")
		hostPort := parts[0]
		if len(parts) > 1 {
			config.Database = parts[1]
		}

		hostPortParts := strings.Split(hostPort, ":")
		config.Host = hostPortParts[0]
		if len(hostPortParts) > 1 {
			if port, err := strconv.Atoi(hostPortParts[1]); err == nil {
				config.Port = port
			}
		}
	}

	if config.Host == "" {
		return nil, nil, fmt.Errorf("DM host is required")
	}

	return config, credentials, nil
}

// DmUtils 达梦数据库工具函数
type DmUtils struct{}

// GetDatabases 获取达梦数据库列表（实际上是获取模式列表）
func (u *DmUtils) GetDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	query := `
		SELECT DISTINCT OWNER 
		FROM DBA_OBJECTS 
		WHERE OWNER NOT IN ('SYS', 'SYSTEM', 'SYSAUX', 'CTXSYS', 'XDB', 'MDSYS', 'OLAPSYS', 'OWBSYS', 'ORDPLUGINS', 'ORDSYS', 'SI_INFORMTN_SCHEMA')
		ORDER BY OWNER
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query DM schemas: %w", err)
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			continue
		}
		databases = append(databases, dbName)
	}

	return databases, nil
}

// GetTables 获取达梦表列表
func (u *DmUtils) GetTables(ctx context.Context, db *sql.DB, schema string) ([]*TableInfo, error) {
	var query string
	var args []interface{}

	if schema != "" {
		query = `
			SELECT TABLE_NAME, 'TABLE' as TABLE_TYPE, NULL as ENGINE, COMMENTS as TABLE_COMMENT, NUM_ROWS, BLOCKS*8192 as TABLE_SIZE
			FROM DBA_TABLES 
			WHERE OWNER = UPPER(?)
			UNION ALL
			SELECT VIEW_NAME, 'VIEW', NULL, NULL, 0, 0
			FROM DBA_VIEWS
			WHERE OWNER = UPPER(?)
			ORDER BY TABLE_NAME
		`
		args = []interface{}{schema, schema}
	} else {
		query = `
			SELECT TABLE_NAME, 'TABLE' as TABLE_TYPE, NULL as ENGINE, COMMENTS as TABLE_COMMENT, NUM_ROWS, BLOCKS*8192 as TABLE_SIZE
			FROM USER_TABLES
			UNION ALL
			SELECT VIEW_NAME, 'VIEW', NULL, NULL, 0, 0
			FROM USER_VIEWS
			ORDER BY TABLE_NAME
		`
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query DM tables: %w", err)
	}
	defer rows.Close()

	var tables []*TableInfo
	for rows.Next() {
		table := &TableInfo{}
		var rowCount, size sql.NullInt64
		var comment sql.NullString

		err := rows.Scan(
			&table.Name,
			&table.Type,
			&table.Engine,
			&comment,
			&rowCount,
			&size,
		)
		if err != nil {
			continue
		}

		if comment.Valid {
			table.Comment = comment.String
		}
		if rowCount.Valid {
			table.Rows = rowCount.Int64
		}
		if size.Valid {
			table.Size = size.Int64
		}

		tables = append(tables, table)
	}

	return tables, nil
}

// GetTableColumns 获取达梦表列信息
func (u *DmUtils) GetTableColumns(ctx context.Context, db *sql.DB, schema, tableName string) ([]*ColumnInfo, error) {
	var query string
	var args []interface{}

	if schema != "" {
		query = `
			SELECT 
				c.COLUMN_NAME,
				c.DATA_TYPE,
				c.DATA_LENGTH,
				c.DATA_PRECISION,
				c.DATA_SCALE,
				CASE WHEN c.NULLABLE = 'Y' THEN 1 ELSE 0 END as NULLABLE,
				CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 1 ELSE 0 END as IS_PRIMARY_KEY,
				c.DATA_DEFAULT,
				cc.COMMENTS
			FROM DBA_TAB_COLUMNS c
			LEFT JOIN DBA_COL_COMMENTS cc ON c.OWNER = cc.OWNER AND c.TABLE_NAME = cc.TABLE_NAME AND c.COLUMN_NAME = cc.COLUMN_NAME
			LEFT JOIN (
				SELECT cols.OWNER, cols.TABLE_NAME, cols.COLUMN_NAME
				FROM DBA_CONSTRAINTS cons
				JOIN DBA_CONS_COLUMNS cols ON cons.CONSTRAINT_NAME = cols.CONSTRAINT_NAME AND cons.OWNER = cols.OWNER
				WHERE cons.CONSTRAINT_TYPE = 'P'
			) pk ON c.OWNER = pk.OWNER AND c.TABLE_NAME = pk.TABLE_NAME AND c.COLUMN_NAME = pk.COLUMN_NAME
			WHERE c.OWNER = UPPER(?) AND c.TABLE_NAME = UPPER(?)
			ORDER BY c.COLUMN_ID
		`
		args = []interface{}{schema, tableName}
	} else {
		query = `
			SELECT 
				c.COLUMN_NAME,
				c.DATA_TYPE,
				c.DATA_LENGTH,
				c.DATA_PRECISION,
				c.DATA_SCALE,
				CASE WHEN c.NULLABLE = 'Y' THEN 1 ELSE 0 END as NULLABLE,
				CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 1 ELSE 0 END as IS_PRIMARY_KEY,
				c.DATA_DEFAULT,
				cc.COMMENTS
			FROM USER_TAB_COLUMNS c
			LEFT JOIN USER_COL_COMMENTS cc ON c.TABLE_NAME = cc.TABLE_NAME AND c.COLUMN_NAME = cc.COLUMN_NAME
			LEFT JOIN (
				SELECT cols.TABLE_NAME, cols.COLUMN_NAME
				FROM USER_CONSTRAINTS cons
				JOIN USER_CONS_COLUMNS cols ON cons.CONSTRAINT_NAME = cols.CONSTRAINT_NAME
				WHERE cons.CONSTRAINT_TYPE = 'P'
			) pk ON c.TABLE_NAME = pk.TABLE_NAME AND c.COLUMN_NAME = pk.COLUMN_NAME
			WHERE c.TABLE_NAME = UPPER(?)
			ORDER BY c.COLUMN_ID
		`
		args = []interface{}{tableName}
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query DM table columns: %w", err)
	}
	defer rows.Close()

	var columns []*ColumnInfo
	for rows.Next() {
		column := &ColumnInfo{}
		var length, precision, scale sql.NullInt64
		var nullable, isPrimaryKey int
		var defaultValue, comment sql.NullString

		err := rows.Scan(
			&column.Name,
			&column.DatabaseType,
			&length,
			&precision,
			&scale,
			&nullable,
			&isPrimaryKey,
			&defaultValue,
			&comment,
		)
		if err != nil {
			continue
		}

		// 转换达梦数据类型到标准类型
		column.Type = u.convertDmType(column.DatabaseType)

		if length.Valid {
			column.Length = length.Int64
		}
		if precision.Valid {
			column.Precision = int(precision.Int64)
		}
		if scale.Valid {
			column.Scale = int(scale.Int64)
		}

		column.Nullable = nullable == 1
		column.IsPrimaryKey = isPrimaryKey == 1

		if defaultValue.Valid {
			column.DefaultValue = defaultValue.String
		}
		if comment.Valid {
			column.Comment = comment.String
		}

		columns = append(columns, column)
	}

	return columns, nil
}

// convertDmType 转换达梦数据类型到标准类型
func (u *DmUtils) convertDmType(dmType string) string {
	dmType = strings.ToUpper(dmType)

	switch {
	case strings.Contains(dmType, "INT"), strings.Contains(dmType, "BIGINT"), strings.Contains(dmType, "SMALLINT"):
		return "integer"
	case strings.Contains(dmType, "NUMBER"), strings.Contains(dmType, "NUMERIC"), strings.Contains(dmType, "DECIMAL"):
		return "decimal"
	case strings.Contains(dmType, "VARCHAR2"), strings.Contains(dmType, "VARCHAR"):
		return "varchar"
	case strings.Contains(dmType, "CHAR"):
		return "char"
	case strings.Contains(dmType, "TEXT"), strings.Contains(dmType, "CLOB"):
		return "text"
	case strings.Contains(dmType, "DATE"):
		return "date"
	case strings.Contains(dmType, "TIMESTAMP"):
		return "timestamp"
	case strings.Contains(dmType, "TIME"):
		return "time"
	case strings.Contains(dmType, "BLOB"):
		return "blob"
	case strings.Contains(dmType, "BINARY"), strings.Contains(dmType, "VARBINARY"):
		return "binary"
	case strings.Contains(dmType, "FLOAT"), strings.Contains(dmType, "DOUBLE"):
		return "float"
	case strings.Contains(dmType, "BOOLEAN"):
		return "boolean"
	default:
		return strings.ToLower(dmType)
	}
}

// SplitSQL 分割达梦SQL语句
func (u *DmUtils) SplitSQL(sql string) []string {
	// 达梦使用分号分隔SQL语句，类似Oracle
	statements := make([]string, 0)
	current := strings.Builder{}
	lines := strings.Split(sql, "\n")

	inBlock := false
	blockKeywords := []string{"BEGIN", "DECLARE", "CREATE OR REPLACE"}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 跳过注释和空行
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}

		// 检查是否进入PL/SQL块
		for _, keyword := range blockKeywords {
			if strings.HasPrefix(strings.ToUpper(trimmed), keyword) {
				inBlock = true
				break
			}
		}

		current.WriteString(line)
		current.WriteString("\n")

		// 检查语句结束
		if strings.HasSuffix(trimmed, ";") {
			if !inBlock {
				stmt := strings.TrimSpace(current.String())
				if stmt != "" {
					statements = append(statements, stmt)
				}
				current.Reset()
			}
		}

		// 检查PL/SQL块结束
		if inBlock && strings.HasSuffix(strings.ToUpper(trimmed), "END;") {
			inBlock = false
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
		}

		// 检查存储过程/包结束
		if inBlock && strings.Contains(strings.ToUpper(trimmed), "/") {
			inBlock = false
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
		}
	}

	// 处理最后一个语句
	if current.Len() > 0 {
		stmt := strings.TrimSpace(current.String())
		if stmt != "" {
			statements = append(statements, stmt)
		}
	}

	return statements
}

// DetectSQLType 检测达梦SQL语句类型
func (u *DmUtils) DetectSQLType(sql string) SqlType {
	sql = strings.TrimSpace(strings.ToUpper(sql))

	switch {
	case strings.HasPrefix(sql, "SELECT"):
		return SqlTypeSelect
	case strings.HasPrefix(sql, "INSERT"):
		return SqlTypeInsert
	case strings.HasPrefix(sql, "UPDATE"):
		return SqlTypeUpdate
	case strings.HasPrefix(sql, "DELETE"):
		return SqlTypeDelete
	case strings.HasPrefix(sql, "CREATE"), strings.HasPrefix(sql, "ALTER"), strings.HasPrefix(sql, "DROP"):
		return SqlTypeDDL
	case strings.HasPrefix(sql, "GRANT"), strings.HasPrefix(sql, "REVOKE"):
		return SqlTypeDCL
	case strings.HasPrefix(sql, "COMMIT"), strings.HasPrefix(sql, "ROLLBACK"), strings.HasPrefix(sql, "SAVEPOINT"):
		return SqlTypeTCL
	case strings.HasPrefix(sql, "BEGIN"), strings.HasPrefix(sql, "DECLARE"):
		return SqlTypeOther // PL/SQL块
	default:
		return SqlTypeOther
	}
}

// BuildPagingSQL 构建达梦分页查询SQL
func (u *DmUtils) BuildPagingSQL(sql string, offset, limit int) string {
	// 达梦支持LIMIT和OFFSET语法（类似MySQL）
	if limit > 0 {
		if offset > 0 {
			return fmt.Sprintf("%s LIMIT %d OFFSET %d", sql, limit, offset)
		} else {
			return fmt.Sprintf("%s LIMIT %d", sql, limit)
		}
	}

	return sql
}

// GetDatabaseSize 获取达梦数据库大小
func (u *DmUtils) GetDatabaseSize(ctx context.Context, db *sql.DB) (int64, error) {
	query := `
		SELECT SUM(BYTES) 
		FROM DBA_DATA_FILES
	`

	var size sql.NullInt64
	err := db.QueryRowContext(ctx, query).Scan(&size)
	if err != nil {
		return 0, err
	}

	if size.Valid {
		return size.Int64, nil
	}

	return 0, nil
}

// GetTablespace 获取表空间信息
func (u *DmUtils) GetTablespace(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	query := `
		SELECT 
			TABLESPACE_NAME,
			BYTES / 1024 / 1024 as SIZE_MB,
			MAXBYTES / 1024 / 1024 as MAX_SIZE_MB,
			(BYTES - NVL(FREE_BYTES, 0)) / 1024 / 1024 as USED_MB,
			ROUND(((BYTES - NVL(FREE_BYTES, 0)) / BYTES) * 100, 2) as USED_PCT
		FROM (
			SELECT 
				df.TABLESPACE_NAME,
				SUM(df.BYTES) as BYTES,
				SUM(df.MAXBYTES) as MAXBYTES,
				SUM(fs.BYTES) as FREE_BYTES
			FROM DBA_DATA_FILES df
			LEFT JOIN (
				SELECT TABLESPACE_NAME, SUM(BYTES) as BYTES
				FROM DBA_FREE_SPACE
				GROUP BY TABLESPACE_NAME
			) fs ON df.TABLESPACE_NAME = fs.TABLESPACE_NAME
			GROUP BY df.TABLESPACE_NAME
		)
		ORDER BY TABLESPACE_NAME
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query DM tablespace: %w", err)
	}
	defer rows.Close()

	var tablespaces []map[string]interface{}
	for rows.Next() {
		var name string
		var size, maxSize, used, usedPct sql.NullFloat64

		err := rows.Scan(&name, &size, &maxSize, &used, &usedPct)
		if err != nil {
			continue
		}

		tablespace := map[string]interface{}{
			"name": name,
		}

		if size.Valid {
			tablespace["size_mb"] = size.Float64
		}
		if maxSize.Valid {
			tablespace["max_size_mb"] = maxSize.Float64
		}
		if used.Valid {
			tablespace["used_mb"] = used.Float64
		}
		if usedPct.Valid {
			tablespace["used_pct"] = usedPct.Float64
		}

		tablespaces = append(tablespaces, tablespace)
	}

	return tablespaces, nil
}
