package db

import (
	"context"
	"database/sql"
	"io"
	"time"

    "github.com/coder-lulu/newbee-proxy/plugins/common"
)

// DbPlugin 数据库插件接口
type DbPlugin interface {
	common.ProtocolPlugin

	// 数据库特定功能
	CreateDbConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) (DbConnection, error)
	TestConnection(ctx context.Context, config *DbConfig, credentials *common.Credentials) error
	ExecuteSQL(ctx context.Context, connectionId string, sql string, options *ExecOptions) (*ExecResult, error)
	ExecuteSQLFile(ctx context.Context, connectionId string, reader io.Reader, options *ExecOptions) error
	GetDatabases(ctx context.Context, connectionId string) ([]string, error)
	GetTables(ctx context.Context, connectionId string, database string) ([]*TableInfo, error)
	GetTableInfo(ctx context.Context, connectionId string, database, table string) (*TableDetail, error)
	GetDatabaseInfo(ctx context.Context, connectionId string) (*DatabaseInfo, error)
}

// DbConnection 数据库连接接口
type DbConnection interface {
	common.Connection

	// 基础操作
	ID() string
	Close() error

	// 数据库特定操作
	GetDbInfo() *DbInfo
	ExecuteQuery(ctx context.Context, sql string, args ...any) (*QueryResult, error)
	ExecuteUpdate(ctx context.Context, sql string, args ...any) (*ExecResult, error)
	BeginTransaction() (Transaction, error)
	GetTableColumns(ctx context.Context, database, table string) ([]*ColumnInfo, error)
	GetDatabases(ctx context.Context) ([]string, error)
	GetTables(ctx context.Context, database string) ([]*TableInfo, error)
	SetDatabase(database string) error
	Ping(ctx context.Context) error
	GetStats() *ConnectionStats
	SetDB(db *sql.DB, info *DbInfo) // 设置数据库连接和信息
}

// Transaction 事务接口
type Transaction interface {
	ExecuteQuery(ctx context.Context, sql string, args ...any) (*QueryResult, error)
	ExecuteUpdate(ctx context.Context, sql string, args ...any) (*ExecResult, error)
	Commit() error
	Rollback() error
}

// WebSocketSession WebSocket会话接口，用于连续命令执行
type WebSocketSession interface {
	// 会话控制
	Start(ctx context.Context) error
	Stop() error
	IsActive() bool

	// 命令执行
	ExecuteCommand(command string) error
	SendMessage(message *WSMessage) error

	// 事件处理
	OnMessage(handler func(*WSMessage))
	OnError(handler func(error))
	OnClose(handler func())

	// 会话信息
	GetSessionID() string
	GetConnectionID() string
	GetMetrics() *SessionMetrics
}

// DbConfig 数据库配置
type DbConfig struct {
	Type     DbType `json:"type"` // mysql, postgresql, mssql, oracle, sqlite
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Charset  string `json:"charset"`
	Timezone string `json:"timezone"`

	// 连接池配置
	MaxOpenConns    int           `json:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time"`

	// SSL配置
	SSLMode   string `json:"ssl_mode"`
	SSLCert   string `json:"ssl_cert"`
	SSLKey    string `json:"ssl_key"`
	SSLRootCA string `json:"ssl_root_ca"`

	// 其他参数
	Params map[string]string `json:"params"`

	// 操作配置
	QueryTimeout time.Duration `json:"query_timeout"`
	ExecTimeout  time.Duration `json:"exec_timeout"`
}

// DbType 数据库类型
type DbType string

const (
	DbTypeMySQL      DbType = "mysql"
	DbTypePostgreSQL DbType = "postgresql"
	DbTypeMSSQL      DbType = "mssql"
	DbTypeOracle     DbType = "oracle"
	DbTypeSQLite     DbType = "sqlite"
	DbTypeDM         DbType = "dm"
)

// DbInfo 数据库信息
type DbInfo struct {
	Type     DbType `json:"type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Version  string `json:"version"`
	Charset  string `json:"charset"`
	Timezone string `json:"timezone"`
}

// ExecOptions SQL执行选项
type ExecOptions struct {
	MaxRows        int           `json:"max_rows"`
	Timeout        time.Duration `json:"timeout"`
	UseTransaction bool          `json:"use_transaction"`
	DryRun         bool          `json:"dry_run"`

	// WebSocket相关
	UseWebSocket bool   `json:"use_websocket"`
	SessionID    string `json:"session_id"`
	ClientID     string `json:"client_id"`

	// 审计相关
	Operator string `json:"operator"`
	Remark   string `json:"remark"`
}

// ExecResult SQL执行结果
type ExecResult struct {
	SQL           string        `json:"sql"`
	RowsAffected  int64         `json:"rows_affected"`
	LastInsertID  int64         `json:"last_insert_id"`
	ExecutionTime time.Duration `json:"execution_time"`
	QueryResult   *QueryResult  `json:"query_result,omitempty"`
	Error         string        `json:"error,omitempty"`

	// 审计信息
	ExecutedAt time.Time `json:"executed_at"`
	Operator   string    `json:"operator"`
	Remark     string    `json:"remark"`
}

// QueryResult 查询结果
type QueryResult struct {
	Columns []*ColumnInfo            `json:"columns"`
	Rows    []map[string]interface{} `json:"rows"`
	Count   int                      `json:"count"`
	HasMore bool                     `json:"has_more"`
}

// ColumnInfo 列信息
type ColumnInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	DatabaseType string `json:"database_type"`
	Length       int64  `json:"length"`
	Precision    int    `json:"precision"`
	Scale        int    `json:"scale"`
	Nullable     bool   `json:"nullable"`
	IsPrimaryKey bool   `json:"is_primary_key"`
	IsAutoIncr   bool   `json:"is_auto_incr"`
	DefaultValue string `json:"default_value"`
	Comment      string `json:"comment"`
}

// TableInfo 表信息
type TableInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // table, view
	Engine  string `json:"engine"`
	Charset string `json:"charset"`
	Comment string `json:"comment"`
	Rows    int64  `json:"rows"`
	Size    int64  `json:"size"`
}

// TableDetail 表详细信息
type TableDetail struct {
	*TableInfo
	Columns []*ColumnInfo `json:"columns"`
	Indexes []*IndexInfo  `json:"indexes"`
	DDL     string        `json:"ddl"`
}

// IndexInfo 索引信息
type IndexInfo struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	IsUnique   bool     `json:"is_unique"`
	IsPrimary  bool     `json:"is_primary"`
	Columns    []string `json:"columns"`
	Definition string   `json:"definition"`
}

// DatabaseInfo 数据库信息
type DatabaseInfo struct {
	Name           string    `json:"name"`
	Version        string    `json:"version"`
	Charset        string    `json:"charset"`
	Collation      string    `json:"collation"`
	Size           int64     `json:"size"`
	TableCount     int       `json:"table_count"`
	ViewCount      int       `json:"view_count"`
	ProcedureCount int       `json:"procedure_count"`
	FunctionCount  int       `json:"function_count"`
	CreatedAt      time.Time `json:"created_at"`
}

// ConnectionStats 连接统计信息
type ConnectionStats struct {
	OpenConnections   int           `json:"open_connections"`
	InUseConnections  int           `json:"in_use_connections"`
	IdleConnections   int           `json:"idle_connections"`
	TotalQueries      int64         `json:"total_queries"`
	TotalExecs        int64         `json:"total_execs"`
	TotalTransactions int64         `json:"total_transactions"`
	AvgQueryTime      time.Duration `json:"avg_query_time"`
	AvgExecTime       time.Duration `json:"avg_exec_time"`
	LastActivity      time.Time     `json:"last_activity"`
}

// SessionMetrics 会话指标
type SessionMetrics struct {
	SessionID        string        `json:"session_id"`
	ConnectionID     string        `json:"connection_id"`
	StartTime        time.Time     `json:"start_time"`
	Duration         time.Duration `json:"duration"`
	CommandsExecuted int64         `json:"commands_executed"`
	QueriesExecuted  int64         `json:"queries_executed"`
	UpdatesExecuted  int64         `json:"updates_executed"`
	ErrorsCount      int64         `json:"errors_count"`
	BytesSent        int64         `json:"bytes_sent"`
	BytesReceived    int64         `json:"bytes_received"`
	LastActivity     time.Time     `json:"last_activity"`
}

// WSMessage WebSocket消息
type WSMessage struct {
	Type      string                 `json:"type"`
	Data      interface{}            `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
	SessionID string                 `json:"session_id"`
	RequestID string                 `json:"request_id"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// WebSocket消息类型
const (
	WSMsgTypeCommand   = "command"
	WSMsgTypeResult    = "result"
	WSMsgTypeError     = "error"
	WSMsgTypeProgress  = "progress"
	WSMsgTypeStatus    = "status"
	WSMsgTypeHeartbeat = "heartbeat"
)

// ProgressInfo 执行进度信息
type ProgressInfo struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	ExecutedStatements int    `json:"executed_statements"`
	TotalStatements    int    `json:"total_statements"`
	Percentage         int    `json:"percentage"`
	CurrentSQL         string `json:"current_sql"`
	Terminated         bool   `json:"terminated"`
	Error              string `json:"error,omitempty"`
}

// SqlType SQL语句类型
type SqlType int

const (
	SqlTypeSelect SqlType = iota
	SqlTypeInsert
	SqlTypeUpdate
	SqlTypeDelete
	SqlTypeDDL
	SqlTypeDCL
	SqlTypeTCL
	SqlTypeOther
)

// SqlStatement SQL语句
type SqlStatement struct {
	SQL       string            `json:"sql"`
	Type      SqlType           `json:"type"`
	Tables    []string          `json:"tables"`
	Operation string            `json:"operation"`
	Params    map[string]string `json:"params"`
}
