package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

    "github.com/coder-lulu/newbee-proxy/plugins/common"

	// SQLite驱动
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteConnector SQLite数据库连接器
type SQLiteConnector struct {
	config      *DbConfig
	credentials *common.Credentials
}

// NewSQLiteConnector 创建SQLite连接器
func NewSQLiteConnector(config *DbConfig, credentials *common.Credentials) *SQLiteConnector {
	return &SQLiteConnector{
		config:      config,
		credentials: credentials,
	}
}

// Connect 建立SQLite连接
func (c *SQLiteConnector) Connect(ctx context.Context) (*sql.DB, *DbInfo, error) {
	// 构建SQLite连接字符串
	dsn, err := c.buildDSN()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build SQLite DSN: %w", err)
	}

	LogConnection("connect_attempt", "", &DbInfo{
		Type:     DbTypeSQLite,
		Database: c.config.Database,
	}, 0, nil)

	startTime := time.Now()

	// 打开连接
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		duration := time.Since(startTime)
		LogConnection("connect_failed", "", nil, duration, err)
		return nil, nil, fmt.Errorf("failed to open SQLite connection: %w", err)
	}

	// 配置连接池
	c.configureConnectionPool(db)

	// 测试连接
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		duration := time.Since(startTime)
		LogConnection("ping_failed", "", nil, duration, err)
		return nil, nil, fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	// 设置SQLite特定配置
	if err := c.configureSQLite(ctx, db); err != nil {
		db.Close()
		duration := time.Since(startTime)
		LogConnection("config_failed", "", nil, duration, err)
		return nil, nil, fmt.Errorf("failed to configure SQLite: %w", err)
	}

	// 获取数据库信息
	dbInfo, err := c.getDbInfo(ctx, db)
	if err != nil {
		db.Close()
		duration := time.Since(startTime)
		LogConnection("info_failed", "", nil, duration, err)
		return nil, nil, fmt.Errorf("failed to get SQLite database info: %w", err)
	}

	duration := time.Since(startTime)
	LogConnection("connect_success", "", dbInfo, duration, nil)

	return db, dbInfo, nil
}

// buildDSN 构建SQLite连接字符串
func (c *SQLiteConnector) buildDSN() (string, error) {
	if c.config.Database == "" {
		return "", fmt.Errorf("SQLite database file path is required")
	}

	// SQLite DSN格式: file:path/to/database.db?param1=value1&param2=value2
	dsn := c.config.Database

	// 检查文件路径
	if dsn != ":memory:" {
		// 确保目录存在
		dir := filepath.Dir(dsn)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}
	}

	// 添加SQLite特定参数
	params := make([]string, 0)

	// 默认参数
	params = append(params, "_pragma=foreign_keys(1)")     // 启用外键约束
	params = append(params, "_pragma=journal_mode(WAL)")   // 启用WAL模式
	params = append(params, "_pragma=synchronous(NORMAL)") // 设置同步模式

	// 用户自定义参数
	for key, value := range c.config.Params {
		params = append(params, fmt.Sprintf("%s=%s", key, value))
	}

	if len(params) > 0 {
		if strings.Contains(dsn, "?") {
			dsn += "&" + strings.Join(params, "&")
		} else {
			dsn += "?" + strings.Join(params, "&")
		}
	}

	return dsn, nil
}

// configureConnectionPool 配置SQLite连接池
func (c *SQLiteConnector) configureConnectionPool(db *sql.DB) {
	// SQLite在WAL模式下支持多个读连接和一个写连接
	if c.config.MaxOpenConns > 0 {
		db.SetMaxOpenConns(c.config.MaxOpenConns)
	} else {
		db.SetMaxOpenConns(10) // SQLite默认值
	}

	if c.config.MaxIdleConns > 0 {
		db.SetMaxIdleConns(c.config.MaxIdleConns)
	} else {
		db.SetMaxIdleConns(5)
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

// configureSQLite 配置SQLite特定设置
func (c *SQLiteConnector) configureSQLite(ctx context.Context, db *sql.DB) error {
	// 设置PRAGMA配置
	pragmas := []string{
		"PRAGMA foreign_keys = ON",     // 启用外键约束
		"PRAGMA journal_mode = WAL",    // 启用WAL模式
		"PRAGMA synchronous = NORMAL",  // 设置同步模式
		"PRAGMA cache_size = -64000",   // 设置缓存大小(64MB)
		"PRAGMA temp_store = MEMORY",   // 临时表存储在内存中
		"PRAGMA mmap_size = 268435456", // 设置内存映射大小(256MB)
	}

	for _, pragma := range pragmas {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			// 某些PRAGMA可能不被支持，记录但不失败
			LogConnection("pragma_warning", "", nil, 0, fmt.Errorf("failed to execute %s: %w", pragma, err))
		}
	}

	return nil
}

// getDbInfo 获取SQLite数据库信息
func (c *SQLiteConnector) getDbInfo(ctx context.Context, db *sql.DB) (*DbInfo, error) {
	dbInfo := &DbInfo{
		Type:     DbTypeSQLite,
		Host:     "localhost",
		Port:     0,
		Database: c.config.Database,
	}

	// 获取SQLite版本
	var version string
	err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version)
	if err != nil {
		version = "Unknown"
	}
	dbInfo.Version = "SQLite " + version

	// SQLite使用UTF-8编码
	dbInfo.Charset = "UTF-8"

	// SQLite没有时区概念，使用系统时区
	dbInfo.Timezone = "System"

	return dbInfo, nil
}

// ParseTarget 解析SQLite目标字符串
func (c *SQLiteConnector) ParseTarget(target string) (*DbConfig, *common.Credentials, error) {
	// 支持的格式:
	// sqlite://path/to/database.db
	// sqlite:///absolute/path/to/database.db
	// file:path/to/database.db
	// path/to/database.db
	// :memory:

	config := &DbConfig{
		Type:   DbTypeSQLite,
		Host:   "localhost",
		Port:   0,
		Params: make(map[string]string),
	}

	credentials := &common.Credentials{
		AuthType: "none",
		Timeout:  30,
	}

	var dbPath string

	if strings.HasPrefix(target, "sqlite://") {
		// 解析sqlite://格式
		u, err := url.Parse(target)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid SQLite URL: %w", err)
		}

		dbPath = u.Path
		if strings.HasPrefix(target, "sqlite:///") {
			// 绝对路径
			dbPath = strings.TrimPrefix(u.Path, "/")
		}

		// 解析查询参数
		for key, values := range u.Query() {
			if len(values) > 0 {
				config.Params[key] = values[0]
			}
		}
	} else if strings.HasPrefix(target, "file:") {
		// 解析file:格式
		dbPath = strings.TrimPrefix(target, "file:")
	} else {
		// 直接的文件路径
		dbPath = target
	}

	config.Database = dbPath

	if config.Database == "" {
		return nil, nil, fmt.Errorf("SQLite database path is required")
	}

	return config, credentials, nil
}

// SQLiteUtils SQLite数据库工具函数
type SQLiteUtils struct{}

// GetDatabases 获取SQLite数据库列表（SQLite每个文件就是一个数据库）
func (u *SQLiteUtils) GetDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	// SQLite的ATTACH语句可以附加多个数据库
	query := "PRAGMA database_list"

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query SQLite databases: %w", err)
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			continue
		}
		databases = append(databases, name)
	}

	return databases, nil
}

// GetTables 获取SQLite表列表
func (u *SQLiteUtils) GetTables(ctx context.Context, db *sql.DB, database string) ([]*TableInfo, error) {
	// SQLite系统表查询
	query := `
		SELECT 
			name,
			type,
			'' as engine,
			'' as comment,
			0 as table_rows,
			0 as data_length
		FROM sqlite_master 
		WHERE type IN ('table', 'view') 
		AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query SQLite tables: %w", err)
	}
	defer rows.Close()

	var tables []*TableInfo
	for rows.Next() {
		table := &TableInfo{}
		err := rows.Scan(
			&table.Name,
			&table.Type,
			&table.Engine,
			&table.Comment,
			&table.Rows,
			&table.Size,
		)
		if err != nil {
			continue
		}

		// 获取实际行数（对于表）
		if table.Type == "table" {
			var count int64
			countQuery := fmt.Sprintf("SELECT COUNT(*) FROM `%s`", table.Name)
			if err := db.QueryRowContext(ctx, countQuery).Scan(&count); err == nil {
				table.Rows = count
			}
		}

		tables = append(tables, table)
	}

	return tables, nil
}

// GetTableColumns 获取SQLite表列信息
func (u *SQLiteUtils) GetTableColumns(ctx context.Context, db *sql.DB, database, tableName string) ([]*ColumnInfo, error) {
	// 使用PRAGMA table_info获取列信息
	query := fmt.Sprintf("PRAGMA table_info(`%s`)", tableName)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query SQLite table columns: %w", err)
	}
	defer rows.Close()

	var columns []*ColumnInfo
	for rows.Next() {
		var cid int
		var name, dataType, defaultValue sql.NullString
		var notNull, pk int

		err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk)
		if err != nil {
			continue
		}

		column := &ColumnInfo{
			Name:         name.String,
			DatabaseType: dataType.String,
			Type:         u.convertSQLiteType(dataType.String),
			Nullable:     notNull == 0,
			IsPrimaryKey: pk == 1,
		}

		if defaultValue.Valid {
			column.DefaultValue = defaultValue.String
		}

		// SQLite的类型信息比较简单，尝试解析长度信息
		if strings.Contains(dataType.String, "(") {
			// 解析类型中的长度信息，如 VARCHAR(255)
			parts := strings.Split(dataType.String, "(")
			if len(parts) == 2 {
				lengthPart := strings.TrimSuffix(parts[1], ")")
				if length, err := strconv.Atoi(lengthPart); err == nil {
					column.Length = int64(length)
				}
			}
		}

		columns = append(columns, column)
	}

	return columns, nil
}

// convertSQLiteType 转换SQLite数据类型到标准类型
func (u *SQLiteUtils) convertSQLiteType(sqliteType string) string {
	sqliteType = strings.ToUpper(sqliteType)

	switch {
	case strings.Contains(sqliteType, "INT"):
		return "integer"
	case strings.Contains(sqliteType, "TEXT"), strings.Contains(sqliteType, "VARCHAR"), strings.Contains(sqliteType, "CHAR"):
		return "text"
	case strings.Contains(sqliteType, "REAL"), strings.Contains(sqliteType, "FLOAT"), strings.Contains(sqliteType, "DOUBLE"):
		return "real"
	case strings.Contains(sqliteType, "NUMERIC"), strings.Contains(sqliteType, "DECIMAL"):
		return "numeric"
	case strings.Contains(sqliteType, "BLOB"):
		return "blob"
	case strings.Contains(sqliteType, "DATE"):
		return "date"
	case strings.Contains(sqliteType, "TIME"):
		return "datetime"
	case strings.Contains(sqliteType, "BOOLEAN"):
		return "boolean"
	default:
		return strings.ToLower(sqliteType)
	}
}

// SplitSQL 分割SQLite SQL语句
func (u *SQLiteUtils) SplitSQL(sql string) []string {
	// SQLite使用分号分隔SQL语句
	statements := make([]string, 0)
	current := strings.Builder{}
	lines := strings.Split(sql, "\n")

	inQuote := false
	quoteChar := byte(0)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 跳过注释和空行
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}

		for i, char := range []byte(line) {
			current.WriteByte(char)

			// 处理引号
			if char == '\'' || char == '"' {
				if !inQuote {
					inQuote = true
					quoteChar = char
				} else if char == quoteChar {
					// 检查是否是转义引号
					if i > 0 && line[i-1] != '\\' {
						inQuote = false
						quoteChar = 0
					}
				}
			}

			// 检查语句结束
			if char == ';' && !inQuote {
				stmt := strings.TrimSpace(current.String())
				if stmt != "" {
					statements = append(statements, stmt)
				}
				current.Reset()
			}
		}

		current.WriteByte('\n')
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

// DetectSQLType 检测SQLite SQL语句类型
func (u *SQLiteUtils) DetectSQLType(sql string) SqlType {
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
	case strings.HasPrefix(sql, "BEGIN"), strings.HasPrefix(sql, "COMMIT"), strings.HasPrefix(sql, "ROLLBACK"):
		return SqlTypeTCL
	case strings.HasPrefix(sql, "PRAGMA"), strings.HasPrefix(sql, "ATTACH"), strings.HasPrefix(sql, "DETACH"):
		return SqlTypeOther
	default:
		return SqlTypeOther
	}
}

// BuildPagingSQL 构建SQLite分页查询SQL
func (u *SQLiteUtils) BuildPagingSQL(sql string, offset, limit int) string {
	// SQLite支持LIMIT和OFFSET
	if limit > 0 {
		if offset > 0 {
			return fmt.Sprintf("%s LIMIT %d OFFSET %d", sql, limit, offset)
		} else {
			return fmt.Sprintf("%s LIMIT %d", sql, limit)
		}
	}

	return sql
}

// VacuumDatabase 清理SQLite数据库
func (u *SQLiteUtils) VacuumDatabase(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "VACUUM")
	return err
}

// AnalyzeDatabase 分析SQLite数据库
func (u *SQLiteUtils) AnalyzeDatabase(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "ANALYZE")
	return err
}

// GetDatabaseSize 获取SQLite数据库文件大小
func (u *SQLiteUtils) GetDatabaseSize(ctx context.Context, db *sql.DB) (int64, error) {
	var pageCount, pageSize int64

	err := db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount)
	if err != nil {
		return 0, err
	}

	err = db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize)
	if err != nil {
		return 0, err
	}

	return pageCount * pageSize, nil
}
