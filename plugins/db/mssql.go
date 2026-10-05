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
	// _ "github.com/denisenkom/go-mssqldb" // 注释掉，避免编译错误
)

// MSSQLConnector MSSQL连接器
type MSSQLConnector struct{}

// CreateConnection 创建MSSQL连接
func (m *MSSQLConnector) CreateConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) (DbConnection, error) {
	// 构建连接字符串
	dsn, err := m.buildDSN(config, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}

	// 创建数据库连接
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open MSSQL connection: %w", err)
	}

	// 配置连接池
	m.configureConnectionPool(db, config)

	// 测试连接
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping MSSQL database: %w", err)
	}

	// 获取数据库版本信息
	dbInfo, err := m.getDbInfo(ctx, db, config)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to get database info: %w", err)
	}

	// 创建连接对象
	connectionId := fmt.Sprintf("mssql_%s_%d_%s_%d", config.Host, config.Port, config.Database, time.Now().Unix())
	target := fmt.Sprintf("mssql://%s:%d/%s", config.Host, config.Port, config.Database)

	conn := NewDbConnection(connectionId, target, config, credentials)
	conn.SetDB(db, dbInfo)

	return conn, nil
}

// buildDSN 构建MSSQL连接字符串
func (m *MSSQLConnector) buildDSN(config *DbConfig, credentials *common.Credentials) (string, error) {
	if config.Host == "" {
		return "", fmt.Errorf("host is required")
	}

	if config.Port == 0 {
		config.Port = 1433 // MSSQL默认端口
	}

	if credentials.Username == "" {
		return "", fmt.Errorf("username is required")
	}

	// 构建连接参数
	params := url.Values{}

	// 应用名称
	params.Add("app name", "newbee-agent")

	// 数据库名称
	if config.Database != "" {
		params.Add("database", config.Database)
	}

	// 连接超时
	if config.QueryTimeout > 0 {
		timeoutSeconds := int(config.QueryTimeout.Seconds())
		params.Add("connection timeout", strconv.Itoa(timeoutSeconds))
	} else {
		params.Add("connection timeout", "30")
	}

	// 登录超时
	params.Add("dial timeout", "30")

	// 加密连接
	if config.SSLMode != "" {
		switch config.SSLMode {
		case "disabled", "disable", "false":
			params.Add("encrypt", "disable")
		case "required", "true":
			params.Add("encrypt", "true")
		default:
			params.Add("encrypt", config.SSLMode)
		}
	} else {
		params.Add("encrypt", "disable")
	}

	// 信任服务器证书
	params.Add("trustservercertificate", "true")

	// 其他自定义参数
	for key, value := range config.Params {
		params.Add(key, value)
	}

	// 构建DSN
	dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?%s",
		url.QueryEscape(credentials.Username),
		url.QueryEscape(credentials.Password),
		config.Host,
		config.Port,
		params.Encode(),
	)

	return dsn, nil
}

// configureConnectionPool 配置连接池
func (m *MSSQLConnector) configureConnectionPool(db *sql.DB, config *DbConfig) {
	// 最大打开连接数
	if config.MaxOpenConns > 0 {
		db.SetMaxOpenConns(config.MaxOpenConns)
	} else {
		db.SetMaxOpenConns(100)
	}

	// 最大空闲连接数
	if config.MaxIdleConns > 0 {
		db.SetMaxIdleConns(config.MaxIdleConns)
	} else {
		db.SetMaxIdleConns(10)
	}

	// 连接最大生存时间
	if config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(config.ConnMaxLifetime)
	} else {
		db.SetConnMaxLifetime(time.Hour)
	}

	// 连接最大空闲时间
	if config.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(config.ConnMaxIdleTime)
	} else {
		db.SetConnMaxIdleTime(time.Minute * 30)
	}
}

// getDbInfo 获取数据库信息
func (m *MSSQLConnector) getDbInfo(ctx context.Context, db *sql.DB, config *DbConfig) (*DbInfo, error) {
	dbInfo := &DbInfo{
		Type:     DbTypeMSSQL,
		Host:     config.Host,
		Port:     config.Port,
		Database: config.Database,
		Charset:  config.Charset,
		Timezone: config.Timezone,
	}

	// 获取SQL Server版本
	var version string
	err := db.QueryRowContext(ctx, "SELECT @@VERSION").Scan(&version)
	if err != nil {
		return nil, fmt.Errorf("failed to get SQL Server version: %w", err)
	}
	dbInfo.Version = version

	// 获取当前数据库名称（如果未指定）
	if dbInfo.Database == "" {
		var dbName string
		err := db.QueryRowContext(ctx, "SELECT DB_NAME()").Scan(&dbName)
		if err == nil {
			dbInfo.Database = dbName
		}
	}

	// 获取排序规则
	var collation string
	err = db.QueryRowContext(ctx, "SELECT DATABASEPROPERTYEX(DB_NAME(), 'Collation')").Scan(&collation)
	if err == nil {
		dbInfo.Charset = collation
	}

	return dbInfo, nil
}

// ParseMSSQLTarget 解析MSSQL目标字符串
func ParseMSSQLTarget(target string) (*DbConfig, error) {
	// 支持格式:
	// mssql://host:port/database
	// sqlserver://host:port/database
	// host:port/database

	target = strings.TrimPrefix(target, "mssql://")
	target = strings.TrimPrefix(target, "sqlserver://")

	parts := strings.Split(target, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid MSSQL target format: %s", target)
	}

	hostPort := parts[0]
	database := parts[1]

	var host string
	var port int = 1433

	if strings.Contains(hostPort, ":") {
		hostPortParts := strings.Split(hostPort, ":")
		if len(hostPortParts) != 2 {
			return nil, fmt.Errorf("invalid host:port format: %s", hostPort)
		}

		host = hostPortParts[0]
		var err error
		port, err = strconv.Atoi(hostPortParts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid port number: %s", hostPortParts[1])
		}
	} else {
		host = hostPort
	}

	return &DbConfig{
		Type:     DbTypeMSSQL,
		Host:     host,
		Port:     port,
		Database: database,
	}, nil
}

// MSSQLUtils MSSQL工具函数
type MSSQLUtils struct{}

// DetectSQLType 检测SQL语句类型
func (u *MSSQLUtils) DetectSQLType(sql string) SqlType {
	sql = strings.TrimSpace(strings.ToUpper(sql))

	if strings.HasPrefix(sql, "SELECT") || strings.HasPrefix(sql, "WITH") {
		return SqlTypeSelect
	} else if strings.HasPrefix(sql, "INSERT") {
		return SqlTypeInsert
	} else if strings.HasPrefix(sql, "UPDATE") {
		return SqlTypeUpdate
	} else if strings.HasPrefix(sql, "DELETE") {
		return SqlTypeDelete
	} else if strings.HasPrefix(sql, "CREATE") || strings.HasPrefix(sql, "ALTER") ||
		strings.HasPrefix(sql, "DROP") || strings.HasPrefix(sql, "TRUNCATE") {
		return SqlTypeDDL
	} else if strings.HasPrefix(sql, "GRANT") || strings.HasPrefix(sql, "REVOKE") {
		return SqlTypeDCL
	} else if strings.HasPrefix(sql, "COMMIT") || strings.HasPrefix(sql, "ROLLBACK") ||
		strings.HasPrefix(sql, "BEGIN") {
		return SqlTypeTCL
	}

	return SqlTypeOther
}

// SplitSQL 分割SQL语句
func (u *MSSQLUtils) SplitSQL(content string) []string {
	var statements []string
	var current strings.Builder
	var inString bool
	var stringChar rune
	var inComment bool
	var commentType int // 1: --, 2: /* */

	runes := []rune(content)
	for i := 0; i < len(runes); i++ {
		char := runes[i]

		// 处理注释
		if !inString {
			// 检查单行注释 --
			if !inComment && i < len(runes)-1 && char == '-' && runes[i+1] == '-' {
				inComment = true
				commentType = 1
				continue
			}

			// 检查多行注释 /* */
			if !inComment && i < len(runes)-1 && char == '/' && runes[i+1] == '*' {
				inComment = true
				commentType = 2
				i++ // 跳过 *
				continue
			}

			// 结束多行注释
			if inComment && commentType == 2 && i < len(runes)-1 && char == '*' && runes[i+1] == '/' {
				inComment = false
				commentType = 0
				i++ // 跳过 /
				continue
			}

			// 结束单行注释
			if inComment && commentType == 1 && char == '\n' {
				inComment = false
				commentType = 0
			}
		}

		// 如果在注释中，跳过
		if inComment {
			continue
		}

		// 处理字符串
		if char == '\'' || char == '"' || char == '[' {
			if !inString {
				inString = true
				if char == '[' {
					stringChar = ']'
				} else {
					stringChar = char
				}
			} else if char == stringChar {
				// 检查是否是转义字符
				if i > 0 && runes[i-1] != '\\' {
					inString = false
				}
			}
		}

		// 如果在字符串中，直接添加字符
		if inString {
			current.WriteRune(char)
			continue
		}

		// 检查GO分隔符（MSSQL特有）
		if i < len(runes)-2 && char == 'G' && runes[i+1] == 'O' &&
			(i+2 >= len(runes) || runes[i+2] == '\n' || runes[i+2] == '\r') {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
			i += 1 // 跳过 O
			continue
		}

		// 检查语句分隔符
		if char == ';' {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
			continue
		}

		current.WriteRune(char)
	}

	// 添加最后一个语句
	stmt := strings.TrimSpace(current.String())
	if stmt != "" {
		statements = append(statements, stmt)
	}

	return statements
}
