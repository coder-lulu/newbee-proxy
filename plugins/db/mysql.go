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

	_ "github.com/go-sql-driver/mysql"
)

// MySQLConnector MySQL连接器
type MySQLConnector struct{}

// CreateConnection 创建MySQL连接
func (m *MySQLConnector) CreateConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) (DbConnection, error) {
	// 构建连接字符串
	dsn, err := m.buildDSN(config, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}

	// 创建数据库连接
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open MySQL connection: %w", err)
	}

	// 配置连接池
	m.configureConnectionPool(db, config)

	// 测试连接
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping MySQL database: %w", err)
	}

	// 获取数据库版本信息
	dbInfo, err := m.getDbInfo(ctx, db, config)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to get database info: %w", err)
	}

	// 创建连接对象
	connectionId := fmt.Sprintf("mysql_%s_%d_%s_%d", config.Host, config.Port, config.Database, time.Now().Unix())
	target := fmt.Sprintf("mysql://%s:%d/%s", config.Host, config.Port, config.Database)

	conn := NewDbConnection(connectionId, target, config, credentials)
	conn.SetDB(db, dbInfo)

	return conn, nil
}

// buildDSN 构建MySQL连接字符串
func (m *MySQLConnector) buildDSN(config *DbConfig, credentials *common.Credentials) (string, error) {
	if config.Host == "" {
		return "", fmt.Errorf("host is required")
	}

	if config.Port == 0 {
		config.Port = 3306 // MySQL默认端口
	}

	if credentials.Username == "" {
		return "", fmt.Errorf("username is required")
	}

	// 基本连接参数
	params := url.Values{}

	// 字符集
	if config.Charset != "" {
		params.Add("charset", config.Charset)
	} else {
		params.Add("charset", "utf8mb4")
	}

	// 确保Unicode字符集正确处理
	params.Add("collation", "utf8mb4_unicode_ci")

	// 时区
	if config.Timezone != "" {
		params.Add("loc", config.Timezone)
	} else {
		params.Add("loc", "Local")
	}

	// 连接超时
	if config.QueryTimeout > 0 {
		params.Add("timeout", config.QueryTimeout.String())
	} else {
		params.Add("timeout", "30s")
	}

	// 读取超时
	params.Add("readTimeout", "30s")

	// 写入超时
	params.Add("writeTimeout", "30s")

	// 解析时间
	params.Add("parseTime", "true")

	// SSL配置
	if config.SSLMode != "" {
		switch config.SSLMode {
		case "disabled", "false":
			params.Add("tls", "false")
		case "required", "true":
			params.Add("tls", "true")
		case "skip-verify":
			params.Add("tls", "skip-verify")
		default:
			params.Add("tls", config.SSLMode)
		}
	}

	// 其他自定义参数
	for key, value := range config.Params {
		params.Add(key, value)
	}

	// 构建DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s",
		url.QueryEscape(credentials.Username),
		url.QueryEscape(credentials.Password),
		config.Host,
		config.Port,
		config.Database,
		params.Encode(),
	)

	return dsn, nil
}

// configureConnectionPool 配置连接池
func (m *MySQLConnector) configureConnectionPool(db *sql.DB, config *DbConfig) {
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
func (m *MySQLConnector) getDbInfo(ctx context.Context, db *sql.DB, config *DbConfig) (*DbInfo, error) {
	dbInfo := &DbInfo{
		Type:     DbTypeMySQL,
		Host:     config.Host,
		Port:     config.Port,
		Database: config.Database,
		Charset:  config.Charset,
		Timezone: config.Timezone,
	}

	// 获取MySQL版本
	var version string
	err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version)
	if err != nil {
		return nil, fmt.Errorf("failed to get MySQL version: %w", err)
	}
	dbInfo.Version = version

	// 获取字符集（如果未指定）
	if dbInfo.Charset == "" {
		var charset string
		err := db.QueryRowContext(ctx, "SELECT @@character_set_database").Scan(&charset)
		if err == nil {
			dbInfo.Charset = charset
		}
	}

	// 获取时区（如果未指定）
	if dbInfo.Timezone == "" {
		var timezone string
		err := db.QueryRowContext(ctx, "SELECT @@time_zone").Scan(&timezone)
		if err == nil {
			dbInfo.Timezone = timezone
		}
	}

	return dbInfo, nil
}

// ParseMySQLTarget 解析MySQL目标字符串
func ParseMySQLTarget(target string) (*DbConfig, error) {
	// 支持格式:
	// mysql://host:port/database
	// mysql://host/database
	// host:port/database
	// host/database

	target = strings.TrimPrefix(target, "mysql://")

	parts := strings.Split(target, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid MySQL target format: %s", target)
	}

	hostPort := parts[0]
	database := parts[1]

	var host string
	var port int = 3306

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
		Type:     DbTypeMySQL,
		Host:     host,
		Port:     port,
		Database: database,
		Charset:  "utf8mb4",
		Timezone: "Local",
	}, nil
}

// MySQLUtils MySQL工具函数
type MySQLUtils struct{}

// DetectSQLType 检测SQL语句类型
func (u *MySQLUtils) DetectSQLType(sql string) SqlType {
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
		strings.HasPrefix(sql, "START") || strings.HasPrefix(sql, "BEGIN") {
		return SqlTypeTCL
	}

	return SqlTypeOther
}

// SplitSQL 分割SQL语句
func (u *MySQLUtils) SplitSQL(content string) []string {
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
		if char == '\'' || char == '"' || char == '`' {
			if !inString {
				inString = true
				stringChar = char
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
