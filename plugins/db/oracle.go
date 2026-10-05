package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

    "github.com/coder-lulu/newbee-proxy/plugins/common"
	// Oracle驱动 - godror是Go的Oracle驱动
	// _ "github.com/godror/godror" // 注释掉Oracle驱动以避免编译问题
)

// OracleConnector Oracle数据库连接器
type OracleConnector struct {
	config      *DbConfig
	credentials *common.Credentials
}

// NewOracleConnector 创建Oracle连接器
func NewOracleConnector(config *DbConfig, credentials *common.Credentials) *OracleConnector {
	return &OracleConnector{
		config:      config,
		credentials: credentials,
	}
}

// Connect 建立Oracle连接
func (c *OracleConnector) Connect(ctx context.Context) (*sql.DB, *DbInfo, error) {
	// 构建Oracle连接字符串
	dsn, err := c.buildDSN()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build Oracle DSN: %w", err)
	}

	LogConnection("connect_attempt", "", &DbInfo{
		Type: DbTypeOracle,
		Host: c.config.Host,
		Port: c.config.Port,
	}, 0, nil)

	startTime := time.Now()

	// 打开连接
	db, err := sql.Open("godror", dsn)
	if err != nil {
		duration := time.Since(startTime)
		LogConnection("connect_failed", "", nil, duration, err)
		return nil, nil, fmt.Errorf("failed to open Oracle connection: %w", err)
	}

	// 配置连接池
	c.configureConnectionPool(db)

	// 测试连接
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		duration := time.Since(startTime)
		LogConnection("ping_failed", "", nil, duration, err)
		return nil, nil, fmt.Errorf("failed to ping Oracle database: %w", err)
	}

	// 获取数据库信息
	dbInfo, err := c.getDbInfo(ctx, db)
	if err != nil {
		db.Close()
		duration := time.Since(startTime)
		LogConnection("info_failed", "", nil, duration, err)
		return nil, nil, fmt.Errorf("failed to get Oracle database info: %w", err)
	}

	duration := time.Since(startTime)
	LogConnection("connect_success", "", dbInfo, duration, nil)

	return db, dbInfo, nil
}

// buildDSN 构建Oracle连接字符串
func (c *OracleConnector) buildDSN() (string, error) {
	if c.config.Host == "" {
		return "", fmt.Errorf("Oracle host is required")
	}

	if c.config.Port == 0 {
		c.config.Port = 1521 // Oracle默认端口
	}

	if c.credentials.Username == "" {
		return "", fmt.Errorf("Oracle username is required")
	}

	// Oracle DSN格式: user/password@host:port/service_name
	// 或者: user/password@(DESCRIPTION=(ADDRESS=(PROTOCOL=TCP)(HOST=host)(PORT=port))(CONNECT_DATA=(SERVICE_NAME=service_name)))
	var dsn string

	if c.config.Database != "" {
		// 简单格式
		dsn = fmt.Sprintf("%s/%s@%s:%d/%s",
			c.credentials.Username,
			c.credentials.Password,
			c.config.Host,
			c.config.Port,
			c.config.Database,
		)
	} else {
		// 无服务名的连接
		dsn = fmt.Sprintf("%s/%s@%s:%d",
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
		if strings.Contains(dsn, "?") {
			dsn += "&" + strings.Join(params, "&")
		} else {
			dsn += "?" + strings.Join(params, "&")
		}
	}

	return dsn, nil
}

// configureConnectionPool 配置Oracle连接池
func (c *OracleConnector) configureConnectionPool(db *sql.DB) {
	if c.config.MaxOpenConns > 0 {
		db.SetMaxOpenConns(c.config.MaxOpenConns)
	} else {
		db.SetMaxOpenConns(25) // Oracle默认值
	}

	if c.config.MaxIdleConns > 0 {
		db.SetMaxIdleConns(c.config.MaxIdleConns)
	} else {
		db.SetMaxIdleConns(5)
	}

	if c.config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(c.config.ConnMaxLifetime)
	} else {
		db.SetConnMaxLifetime(30 * time.Minute)
	}

	if c.config.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(c.config.ConnMaxIdleTime)
	} else {
		db.SetConnMaxIdleTime(10 * time.Minute)
	}
}

// getDbInfo 获取Oracle数据库信息
func (c *OracleConnector) getDbInfo(ctx context.Context, db *sql.DB) (*DbInfo, error) {
	dbInfo := &DbInfo{
		Type:     DbTypeOracle,
		Host:     c.config.Host,
		Port:     c.config.Port,
		Database: c.config.Database,
	}

	// 获取Oracle版本
	var version string
	err := db.QueryRowContext(ctx, "SELECT BANNER FROM V$VERSION WHERE ROWNUM = 1").Scan(&version)
	if err != nil {
		// 如果查询失败，尝试其他方式
		err = db.QueryRowContext(ctx, "SELECT VERSION FROM PRODUCT_COMPONENT_VERSION WHERE PRODUCT LIKE 'Oracle Database%' AND ROWNUM = 1").Scan(&version)
		if err != nil {
			version = "Unknown"
		}
	}
	dbInfo.Version = version

	// 获取字符集
	var charset string
	err = db.QueryRowContext(ctx, "SELECT VALUE FROM NLS_DATABASE_PARAMETERS WHERE PARAMETER = 'NLS_CHARACTERSET'").Scan(&charset)
	if err != nil {
		charset = "Unknown"
	}
	dbInfo.Charset = charset

	// 获取时区
	var timezone string
	err = db.QueryRowContext(ctx, "SELECT DBTIMEZONE FROM DUAL").Scan(&timezone)
	if err != nil {
		timezone = "Unknown"
	}
	dbInfo.Timezone = timezone

	return dbInfo, nil
}

// ParseTarget 解析Oracle目标字符串
func (c *OracleConnector) ParseTarget(target string) (*DbConfig, *common.Credentials, error) {
	// 支持的格式:
	// oracle://user:pass@host:port/service_name
	// oracle://user:pass@host:port
	// host:port/service_name
	// host:port

	config := &DbConfig{
		Type:    DbTypeOracle,
		Port:    1521,
		Charset: "UTF8",
		Params:  make(map[string]string),
	}

	credentials := &common.Credentials{
		AuthType: "password",
		Timeout:  30,
	}

	if strings.HasPrefix(target, "oracle://") {
		// 解析完整URL
		u, err := url.Parse(target)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid Oracle URL: %w", err)
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
		// 解析简单格式 host:port/service_name
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
		return nil, nil, fmt.Errorf("Oracle host is required")
	}

	return config, credentials, nil
}

// OracleUtils Oracle数据库工具函数
type OracleUtils struct{}

// GetDatabases 获取Oracle数据库列表（实际上是获取用户列表）
func (u *OracleUtils) GetDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	query := `
		SELECT USERNAME 
		FROM ALL_USERS 
		WHERE USERNAME NOT IN ('SYS', 'SYSTEM', 'DBSNMP', 'SYSMAN', 'OUTLN', 'MGMT_VIEW', 'DIP', 'ORACLE_OCM', 'APPQOSSYS')
		ORDER BY USERNAME
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query Oracle users: %w", err)
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

// GetTables 获取Oracle表列表
func (u *OracleUtils) GetTables(ctx context.Context, db *sql.DB, schema string) ([]*TableInfo, error) {
	var query string
	var args []interface{}

	if schema != "" {
		query = `
			SELECT TABLE_NAME, 'TABLE' as TABLE_TYPE, NULL as ENGINE, NULL as TABLE_COMMENT, NUM_ROWS, BLOCKS*8192 as TABLE_SIZE
			FROM ALL_TABLES 
			WHERE OWNER = UPPER(?)
			UNION ALL
			SELECT VIEW_NAME, 'VIEW', NULL, NULL, 0, 0
			FROM ALL_VIEWS
			WHERE OWNER = UPPER(?)
			ORDER BY TABLE_NAME
		`
		args = []interface{}{schema, schema}
	} else {
		query = `
			SELECT TABLE_NAME, 'TABLE' as TABLE_TYPE, NULL as ENGINE, NULL as TABLE_COMMENT, NUM_ROWS, BLOCKS*8192 as TABLE_SIZE
			FROM USER_TABLES
			UNION ALL
			SELECT VIEW_NAME, 'VIEW', NULL, NULL, 0, 0
			FROM USER_VIEWS
			ORDER BY TABLE_NAME
		`
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query Oracle tables: %w", err)
	}
	defer rows.Close()

	var tables []*TableInfo
	for rows.Next() {
		table := &TableInfo{}
		var rowCount, size sql.NullInt64

		err := rows.Scan(
			&table.Name,
			&table.Type,
			&table.Engine,
			&table.Comment,
			&rowCount,
			&size,
		)
		if err != nil {
			continue
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

// GetTableColumns 获取Oracle表列信息
func (u *OracleUtils) GetTableColumns(ctx context.Context, db *sql.DB, schema, tableName string) ([]*ColumnInfo, error) {
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
			FROM ALL_TAB_COLUMNS c
			LEFT JOIN ALL_COL_COMMENTS cc ON c.OWNER = cc.OWNER AND c.TABLE_NAME = cc.TABLE_NAME AND c.COLUMN_NAME = cc.COLUMN_NAME
			LEFT JOIN (
				SELECT cols.OWNER, cols.TABLE_NAME, cols.COLUMN_NAME
				FROM ALL_CONSTRAINTS cons
				JOIN ALL_CONS_COLUMNS cols ON cons.CONSTRAINT_NAME = cols.CONSTRAINT_NAME AND cons.OWNER = cols.OWNER
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
		return nil, fmt.Errorf("failed to query Oracle table columns: %w", err)
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

		// 转换Oracle数据类型到标准类型
		column.Type = u.convertOracleType(column.DatabaseType)

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

// convertOracleType 转换Oracle数据类型到标准类型
func (u *OracleUtils) convertOracleType(oracleType string) string {
	oracleType = strings.ToUpper(oracleType)

	switch {
	case strings.Contains(oracleType, "NUMBER"):
		return "number"
	case strings.Contains(oracleType, "VARCHAR2"):
		return "varchar"
	case strings.Contains(oracleType, "CHAR"):
		return "char"
	case strings.Contains(oracleType, "DATE"):
		return "date"
	case strings.Contains(oracleType, "TIMESTAMP"):
		return "timestamp"
	case strings.Contains(oracleType, "CLOB"):
		return "text"
	case strings.Contains(oracleType, "BLOB"):
		return "blob"
	case strings.Contains(oracleType, "RAW"):
		return "binary"
	default:
		return strings.ToLower(oracleType)
	}
}

// SplitSQL 分割Oracle SQL语句
func (u *OracleUtils) SplitSQL(sql string) []string {
	// Oracle使用分号分隔SQL语句，但需要处理PL/SQL块
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

// DetectSQLType 检测Oracle SQL语句类型
func (u *OracleUtils) DetectSQLType(sql string) SqlType {
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

// BuildPagingSQL 构建Oracle分页查询SQL
func (u *OracleUtils) BuildPagingSQL(sql string, offset, limit int) string {
	// Oracle 12c以上版本支持OFFSET...ROWS FETCH NEXT...ROWS ONLY
	// 为了兼容性，使用ROWNUM方式
	if offset == 0 && limit > 0 {
		// 只有limit，使用ROWNUM
		return fmt.Sprintf("SELECT * FROM (%s) WHERE ROWNUM <= %d", sql, limit)
	} else if offset > 0 && limit > 0 {
		// 有offset和limit，使用子查询
		return fmt.Sprintf(`
			SELECT * FROM (
				SELECT a.*, ROWNUM rnum FROM (
					%s
				) a WHERE ROWNUM <= %d
			) WHERE rnum > %d
		`, sql, offset+limit, offset)
	}

	return sql
}
