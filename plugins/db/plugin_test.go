package db

import (
	"testing"

	"newbee-agent/plugins/common"
)

// TestDbPlugin 测试数据库插件基本功能
func TestDbPlugin(t *testing.T) {
	// 创建插件实例
	plugin := NewDbPlugin()

	// 测试插件属性
	if plugin.Name() != "db" {
		t.Errorf("Expected plugin name 'db', got '%s'", plugin.Name())
	}

	if plugin.Version() != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", plugin.Version())
	}

	protocols := plugin.SupportedProtocols()
	expectedProtocols := []string{"mysql", "postgresql", "mssql", "oracle", "sqlite", "dm"}
	if len(protocols) != len(expectedProtocols) {
		t.Errorf("Expected %d protocols, got %d", len(expectedProtocols), len(protocols))
	}

	// 测试初始化
	config := map[string]interface{}{
		"max_connections": 100,
		"timeout":         30,
	}

	err := plugin.Initialize(config)
	if err != nil {
		t.Errorf("Failed to initialize plugin: %v", err)
	}

	// 测试启动和停止
	err = plugin.Start()
	if err != nil {
		t.Errorf("Failed to start plugin: %v", err)
	}

	if !plugin.IsRunning() {
		t.Error("Plugin should be running")
	}

	err = plugin.Stop()
	if err != nil {
		t.Errorf("Failed to stop plugin: %v", err)
	}

	if plugin.IsRunning() {
		t.Error("Plugin should not be running")
	}
}

// TestMySQLTargetParsing 测试MySQL目标字符串解析
func TestMySQLTargetParsing(t *testing.T) {
	testCases := []struct {
		target   string
		expected *DbConfig
		hasError bool
	}{
		{
			target: "mysql://localhost:3306/test",
			expected: &DbConfig{
				Type:     DbTypeMySQL,
				Host:     "localhost",
				Port:     3306,
				Database: "test",
				Charset:  "utf8mb4",
				Timezone: "Local",
			},
			hasError: false,
		},
		{
			target: "localhost:3306/test",
			expected: &DbConfig{
				Type:     DbTypeMySQL,
				Host:     "localhost",
				Port:     3306,
				Database: "test",
				Charset:  "utf8mb4",
				Timezone: "Local",
			},
			hasError: false,
		},
		{
			target: "localhost/test",
			expected: &DbConfig{
				Type:     DbTypeMySQL,
				Host:     "localhost",
				Port:     3306,
				Database: "test",
				Charset:  "utf8mb4",
				Timezone: "Local",
			},
			hasError: false,
		},
		{
			target:   "invalid",
			expected: nil,
			hasError: true,
		},
	}

	for _, tc := range testCases {
		result, err := ParseMySQLTarget(tc.target)

		if tc.hasError {
			if err == nil {
				t.Errorf("Expected error for target '%s', but got none", tc.target)
			}
			continue
		}

		if err != nil {
			t.Errorf("Unexpected error for target '%s': %v", tc.target, err)
			continue
		}

		if result.Type != tc.expected.Type {
			t.Errorf("Expected type %s, got %s", tc.expected.Type, result.Type)
		}

		if result.Host != tc.expected.Host {
			t.Errorf("Expected host %s, got %s", tc.expected.Host, result.Host)
		}

		if result.Port != tc.expected.Port {
			t.Errorf("Expected port %d, got %d", tc.expected.Port, result.Port)
		}

		if result.Database != tc.expected.Database {
			t.Errorf("Expected database %s, got %s", tc.expected.Database, result.Database)
		}
	}
}

// TestPostgreSQLTargetParsing 测试PostgreSQL目标字符串解析
func TestPostgreSQLTargetParsing(t *testing.T) {
	testCases := []struct {
		target   string
		expected *DbConfig
		hasError bool
	}{
		{
			target: "postgresql://localhost:5432/test",
			expected: &DbConfig{
				Type:     DbTypePostgreSQL,
				Host:     "localhost",
				Port:     5432,
				Database: "test",
				Charset:  "UTF8",
				Timezone: "UTC",
			},
			hasError: false,
		},
		{
			target: "postgres://localhost/test",
			expected: &DbConfig{
				Type:     DbTypePostgreSQL,
				Host:     "localhost",
				Port:     5432,
				Database: "test",
				Charset:  "UTF8",
				Timezone: "UTC",
			},
			hasError: false,
		},
	}

	for _, tc := range testCases {
		result, err := ParsePostgreSQLTarget(tc.target)

		if tc.hasError {
			if err == nil {
				t.Errorf("Expected error for target '%s', but got none", tc.target)
			}
			continue
		}

		if err != nil {
			t.Errorf("Unexpected error for target '%s': %v", tc.target, err)
			continue
		}

		if result.Type != tc.expected.Type {
			t.Errorf("Expected type %s, got %s", tc.expected.Type, result.Type)
		}

		if result.Host != tc.expected.Host {
			t.Errorf("Expected host %s, got %s", tc.expected.Host, result.Host)
		}

		if result.Port != tc.expected.Port {
			t.Errorf("Expected port %d, got %d", tc.expected.Port, result.Port)
		}

		if result.Database != tc.expected.Database {
			t.Errorf("Expected database %s, got %s", tc.expected.Database, result.Database)
		}
	}
}

// TestSQLTypeDetection 测试SQL类型检测
func TestSQLTypeDetection(t *testing.T) {
	utils := &MySQLUtils{}

	testCases := []struct {
		sql      string
		expected SqlType
	}{
		{"SELECT * FROM users", SqlTypeSelect},
		{"select id from table", SqlTypeSelect},
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", SqlTypeSelect},
		{"INSERT INTO users (name) VALUES ('test')", SqlTypeInsert},
		{"UPDATE users SET name = 'test'", SqlTypeUpdate},
		{"DELETE FROM users WHERE id = 1", SqlTypeDelete},
		{"CREATE TABLE test (id INT)", SqlTypeDDL},
		{"ALTER TABLE test ADD COLUMN name VARCHAR(50)", SqlTypeDDL},
		{"DROP TABLE test", SqlTypeDDL},
		{"TRUNCATE TABLE test", SqlTypeDDL},
		{"GRANT SELECT ON test TO user", SqlTypeDCL},
		{"REVOKE SELECT ON test FROM user", SqlTypeDCL},
		{"COMMIT", SqlTypeTCL},
		{"ROLLBACK", SqlTypeTCL},
		{"BEGIN", SqlTypeTCL},
		{"START TRANSACTION", SqlTypeTCL},
		{"SHOW TABLES", SqlTypeOther},
		{"EXPLAIN SELECT * FROM users", SqlTypeOther},
	}

	for _, tc := range testCases {
		result := utils.DetectSQLType(tc.sql)
		if result != tc.expected {
			t.Errorf("For SQL '%s', expected type %d, got %d", tc.sql, tc.expected, result)
		}
	}
}

// TestSQLSplitting 测试SQL语句分割
func TestSQLSplitting(t *testing.T) {
	utils := &MySQLUtils{}

	testCases := []struct {
		input    string
		expected []string
	}{
		{
			input:    "SELECT 1; SELECT 2;",
			expected: []string{"SELECT 1", "SELECT 2"},
		},
		{
			input:    "INSERT INTO test VALUES ('hello; world'); UPDATE test SET name = 'test';",
			expected: []string{"INSERT INTO test VALUES ('hello; world')", "UPDATE test SET name = 'test'"},
		},
		{
			input:    "-- This is a comment\nSELECT 1; /* Another comment */ SELECT 2;",
			expected: []string{"SELECT 1", "SELECT 2"},
		},
		{
			input:    "SELECT 'quoted;string'; SELECT \"another;quoted\";",
			expected: []string{"SELECT 'quoted;string'", "SELECT \"another;quoted\""},
		},
	}

	for _, tc := range testCases {
		result := utils.SplitSQL(tc.input)

		if len(result) != len(tc.expected) {
			t.Errorf("For input '%s', expected %d statements, got %d", tc.input, len(tc.expected), len(result))
			continue
		}

		for i, stmt := range result {
			if stmt != tc.expected[i] {
				t.Errorf("For input '%s', statement %d: expected '%s', got '%s'", tc.input, i, tc.expected[i], stmt)
			}
		}
	}
}

// TestErrorHandling 测试错误处理
func TestErrorHandling(t *testing.T) {
	// 测试错误包装
	originalErr := &DbError{
		Type:    ErrTypeSQL,
		Code:    "SYNTAX_ERROR",
		Message: "Invalid SQL syntax",
	}

	wrappedErr := WrapError(originalErr)
	if wrappedErr != originalErr {
		t.Error("Wrapping a DbError should return the same error")
	}

	// 测试错误类型判断
	connectionErr := NewConnectionError("Connection failed", nil)
	if !IsConnectionError(connectionErr) {
		t.Error("Should identify connection error")
	}

	if IsAuthError(connectionErr) {
		t.Error("Should not identify as auth error")
	}

	// 测试错误重试性
	retryableErr := NewTimeoutError("Query timeout", nil)
	if !IsRetryableError(retryableErr) {
		t.Error("Timeout error should be retryable")
	}

	fatalErr := NewAuthError("Authentication failed", nil)
	if !IsFatalError(fatalErr) {
		t.Error("Auth error should be fatal")
	}
}

// TestLogger 测试日志记录
func TestLogger(t *testing.T) {
	logger := NewDbLogger(LogLevelInfo)

	// 测试日志级别
	if !logger.shouldLog(LogLevelError) {
		t.Error("Should log error level when set to info")
	}

	if logger.shouldLog(LogLevelDebug) {
		t.Error("Should not log debug level when set to info")
	}

	// 测试错误收集器
	collector := NewErrorCollector(10)

	err1 := NewConnectionError("Connection error", nil)
	err2 := NewSQLError("SQL error", "SELECT * FROM invalid", nil)

	collector.Add(err1)
	collector.Add(err2)

	summary := collector.GetSummary()
	if summary.TotalErrors != 2 {
		t.Errorf("Expected 2 total errors, got %d", summary.TotalErrors)
	}

	if summary.ErrorsByType["connection"] != 1 {
		t.Errorf("Expected 1 connection error, got %d", summary.ErrorsByType["connection"])
	}

	if summary.ErrorsByType["sql"] != 1 {
		t.Errorf("Expected 1 SQL error, got %d", summary.ErrorsByType["sql"])
	}
}

// TestConnectionLifecycle 测试连接生命周期（模拟）
func TestConnectionLifecycle(t *testing.T) {
	// 创建模拟连接配置
	config := &DbConfig{
		Type:     DbTypeMySQL,
		Host:     "localhost",
		Port:     3306,
		Database: "test",
	}

	credentials := &common.Credentials{
		Username: "test",
		Password: "test",
		AuthType: "password",
	}

	// 创建连接对象（不实际连接数据库）
	conn := NewDbConnection("test_conn", "mysql://localhost:3306/test", config, credentials)

	// 测试连接属性
	if conn.ID() != "test_conn" {
		t.Errorf("Expected connection ID 'test_conn', got '%s'", conn.ID())
	}

	if conn.Protocol() != "mysql" {
		t.Errorf("Expected protocol 'mysql', got '%s'", conn.Protocol())
	}

	if conn.Status() != common.StatusConnecting {
		t.Errorf("Expected status connecting, got %v", conn.Status())
	}

	// 测试元数据
	conn.SetMetadata("key1", "value1")
	metadata := conn.GetMetadata()
	if metadata["key1"] != "value1" {
		t.Error("Metadata not set correctly")
	}

	// 测试关闭
	err := conn.Close()
	if err != nil {
		t.Errorf("Unexpected error closing connection: %v", err)
	}

	if conn.Status() != common.StatusDisconnected {
		t.Errorf("Expected status disconnected after close, got %v", conn.Status())
	}
}

// BenchmarkSQLTypeDetection SQL类型检测基准测试
func BenchmarkSQLTypeDetection(b *testing.B) {
	utils := &MySQLUtils{}
	sql := "SELECT id, name, email FROM users WHERE status = 1 ORDER BY created_at DESC LIMIT 100"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		utils.DetectSQLType(sql)
	}
}

// BenchmarkSQLSplitting SQL分割基准测试
func BenchmarkSQLSplitting(b *testing.B) {
	utils := &MySQLUtils{}
	content := `
		INSERT INTO users (name, email) VALUES ('John', 'john@example.com');
		UPDATE users SET status = 1 WHERE email = 'john@example.com';
		DELETE FROM logs WHERE created_at < '2023-01-01';
		SELECT COUNT(*) FROM users WHERE status = 1;
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		utils.SplitSQL(content)
	}
}
