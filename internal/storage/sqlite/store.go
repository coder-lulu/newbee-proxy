package sqlite

import (
    "database/sql"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "time"

    // 使用纯 Go 的 modernc.org/sqlite，避免 CGO 依赖
    _ "modernc.org/sqlite"
)

type Store struct {
    DB   *sql.DB
    path string
}

// OutboxEvent 出站事件
type OutboxEvent struct {
    ID          int64
    EventType   string
    Payload     []byte
    Status      string
    RetryCount  int
    NextRetryAt time.Time
    LastError   string
    CreatedAt   time.Time
}

// 已移除 backlog 任务（outbox_tasks），仅保留 outbox_events

// Open 打开或创建 SQLite 数据库，并执行自动迁移
func Open(dbPath string) (*Store, error) {
    if dbPath == "" {
        dbPath = filepath.Join("data", "proxy.db")
    }
    if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
        return nil, fmt.Errorf("mkdir: %w", err)
    }
    // modernc 驱动名为 "sqlite"，PRAGMA 通过 _pragma 参数传递
    dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
    db, err := sql.Open("sqlite", dsn)
    if err != nil {
        return nil, err
    }
    if err := pingRetry(db, 3); err != nil {
        _ = db.Close()
        return nil, err
    }
    s := &Store{DB: db, path: dbPath}
    if err := s.migrate(); err != nil {
        _ = db.Close()
        return nil, err
    }
    return s, nil
}

func pingRetry(db *sql.DB, n int) error {
    var err error
    for i := 0; i < n; i++ {
        if err = db.Ping(); err == nil {
            return nil
        }
        time.Sleep(100 * time.Millisecond)
    }
    return err
}

// migrate 执行基础表迁移（幂等）
func (s *Store) migrate() error {
    stmts := []string{
        `CREATE TABLE IF NOT EXISTS tasks (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            task_id TEXT UNIQUE,
            type TEXT,
            status TEXT,
            target TEXT,
            protocol TEXT,
            started_at INTEGER,
            finished_at INTEGER,
            exit_code INTEGER,
            error_message TEXT,
            result_path TEXT,
            bytes INTEGER,
            tenant_id TEXT,
            labels TEXT
        );`,
        `CREATE INDEX IF NOT EXISTS idx_tasks_status_finished ON tasks(status, finished_at);`,
        `CREATE INDEX IF NOT EXISTS idx_tasks_finished ON tasks(finished_at);`,
        `CREATE INDEX IF NOT EXISTS idx_tasks_tenant ON tasks(tenant_id);`,
        // 已移除 agent 相关字段/索引
        // `CREATE INDEX IF NOT EXISTS idx_tasks_agent ON tasks(agent_id);`,
        `CREATE INDEX IF NOT EXISTS idx_tasks_taskid ON tasks(task_id);`,

        `CREATE TABLE IF NOT EXISTS sessions (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            session_id TEXT UNIQUE,
            protocol TEXT,
            target TEXT,
            start_at INTEGER,
            end_at INTEGER,
            bytes_sent INTEGER,
            bytes_recv INTEGER,
            commands INTEGER,
            errors INTEGER,
            close_reason TEXT
        );`,
        `CREATE INDEX IF NOT EXISTS idx_sessions_id ON sessions(session_id);`,
        `CREATE INDEX IF NOT EXISTS idx_sessions_end ON sessions(end_at);`,
        `CREATE INDEX IF NOT EXISTS idx_sessions_start ON sessions(start_at);`,

        `CREATE TABLE IF NOT EXISTS outbox_events (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            event_type TEXT,
            payload BLOB,
            status TEXT,
            retry_count INTEGER,
            next_retry_at INTEGER,
            last_error TEXT,
            created_at INTEGER
        );`,
        `CREATE INDEX IF NOT EXISTS idx_outbox_status_next ON outbox_events(status, next_retry_at);`,
        // outbox_tasks 已移除（以前用于 agent backlog）

        `CREATE TABLE IF NOT EXISTS config_cache (
            key TEXT PRIMARY KEY,
            value TEXT,
            version TEXT,
            updated_at INTEGER
        );`,

        // 指标历史：用于可视化与健康追踪，保留期由 RunGC 管理
        `CREATE TABLE IF NOT EXISTS metrics_history (
            ts INTEGER,
            cpu_usage REAL,
            memory_usage REAL,
            goroutines INTEGER,
            tasks_running INTEGER,
            sessions INTEGER,
            db_size_bytes INTEGER,
            outbox_pending INTEGER
        );`,
        `CREATE INDEX IF NOT EXISTS idx_metrics_ts ON metrics_history(ts);`,
    }
    for _, sql := range stmts {
        if _, err := s.DB.Exec(sql); err != nil {
            return err
        }
    }
    return nil
}

func (s *Store) Close() error {
    if s == nil || s.DB == nil {
        return nil
    }
    return s.DB.Close()
}

// DBSizeBytes 返回数据库文件大小
func (s *Store) DBSizeBytes() (int64, error) {
    fi, err := os.Stat(s.path)
    if err != nil { return 0, err }
    return fi.Size(), nil
}

// RunGC 执行保留期与容量控制的清理，并在发生删除时执行 VACUUM
func (s *Store) RunGC(retentionDays int, maxSizeMB int) (map[string]int, error) {
    stats := map[string]int{"tasks_deleted":0, "sessions_deleted":0, "files_deleted":0, "metrics_deleted":0}
    var deleted bool

    // 1) 按保留期清理
    if retentionDays > 0 {
        before := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
        // 逐批清理 tasks
        for {
            rows, err := s.DB.Query(`SELECT task_id, result_path FROM tasks WHERE finished_at > 0 AND finished_at < ? LIMIT 500`, before)
            if err != nil { return stats, err }
            var ids []string
            var files []string
            for rows.Next() {
                var id, path string
                _ = rows.Scan(&id, &path)
                ids = append(ids, id)
                files = append(files, path)
            }
            _ = rows.Close()
            if len(ids) == 0 { break }
            // 删除文件（仅删除 task_results 目录下的文件，避免误删）
            for _, fp := range files {
                if fp == "" { continue }
                if safeResultPath(fp) { _ = os.Remove(fp); stats["files_deleted"]++ }
            }
            // 删除行
            if err := s.deleteTasksByTaskIDs(ids); err != nil { return stats, err }
            stats["tasks_deleted"] += len(ids)
            deleted = true
        }
        // 逐批清理 sessions
        for {
            rows, err := s.DB.Query(`SELECT session_id FROM sessions WHERE end_at > 0 AND end_at < ? LIMIT 1000`, before)
            if err != nil { return stats, err }
            var ids []string
            for rows.Next() { var id string; _ = rows.Scan(&id); ids = append(ids, id) }
            _ = rows.Close()
            if len(ids) == 0 { break }
            if err := s.deleteSessionsBySessionIDs(ids); err != nil { return stats, err }
            stats["sessions_deleted"] += len(ids)
            deleted = true
        }

        // 清理指标历史（metrics_history）
        if res, err := s.DB.Exec(`DELETE FROM metrics_history WHERE ts < ?`, before); err == nil {
            if n, _ := res.RowsAffected(); n > 0 {
                stats["metrics_deleted"] += int(n)
                deleted = true
            }
        }
    }

    // 2) 按容量控制（仅当超过时继续删除最老任务）
    if maxSizeMB > 0 {
        threshold := int64(maxSizeMB) * 1024 * 1024
        for i := 0; i < 20; i++ { // 最多循环 20 次防止过度
            size, err := s.DBSizeBytes()
            if err != nil { return stats, err }
            if size <= threshold { break }
            // 拉取最老的任务批次
            rows, err := s.DB.Query(`SELECT task_id, result_path FROM tasks WHERE finished_at > 0 ORDER BY finished_at ASC LIMIT 500`)
            if err != nil { return stats, err }
            var ids []string
            var files []string
            for rows.Next() {
                var id, path string
                _ = rows.Scan(&id, &path)
                ids = append(ids, id)
                files = append(files, path)
            }
            _ = rows.Close()
            if len(ids) == 0 {
                // 尝试从 metrics_history 删老数据
                if _, err := s.DB.Exec(`DELETE FROM metrics_history WHERE rowid IN (SELECT rowid FROM metrics_history ORDER BY ts ASC LIMIT 5000)`); err == nil {
                    stats["metrics_deleted"] += 5000 // 估算，实际受限于表行数
                    deleted = true
                }
                break
            }
            for _, fp := range files { if fp != "" && safeResultPath(fp) { _ = os.Remove(fp); stats["files_deleted"]++ } }
            if err := s.deleteTasksByTaskIDs(ids); err != nil { return stats, err }
            stats["tasks_deleted"] += len(ids)
            deleted = true
        }
    }

    // 3) VACUUM 优化（仅当发生删除时）
    if deleted {
        // 检查点 WAL，随后 VACUUM
        _, _ = s.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
        _, _ = s.DB.Exec("VACUUM;")
        _, _ = s.DB.Exec("PRAGMA optimize;")
    }
    return stats, nil
}

func (s *Store) deleteTasksByTaskIDs(ids []string) error {
    if len(ids) == 0 { return nil }
    tx, err := s.DB.Begin()
    if err != nil { return err }
    defer func(){ _ = tx.Rollback() }()
    stmt, err := tx.Prepare(`DELETE FROM tasks WHERE task_id=?`)
    if err != nil { return err }
    defer stmt.Close()
    for _, id := range ids { if _, err := stmt.Exec(id); err != nil { return err } }
    return tx.Commit()
}

func (s *Store) deleteSessionsBySessionIDs(ids []string) error {
    if len(ids) == 0 { return nil }
    tx, err := s.DB.Begin()
    if err != nil { return err }
    defer func(){ _ = tx.Rollback() }()
    stmt, err := tx.Prepare(`DELETE FROM sessions WHERE session_id=?`)
    if err != nil { return err }
    defer stmt.Close()
    for _, id := range ids { if _, err := stmt.Exec(id); err != nil { return err } }
    return tx.Commit()
}

func safeResultPath(p string) bool {
    if p == "" { return false }
    cp := filepath.Clean(p)
    // 仅允许删除包含 task_results 目录的路径，避免误删其他文件
    return strings.Contains(cp, string(os.PathSeparator)+"task_results"+string(os.PathSeparator)) ||
        strings.HasPrefix(cp, "task_results"+string(os.PathSeparator))
}

// EnqueueOutbox 将事件写入 outbox（pending）
func (s *Store) EnqueueOutbox(eventType string, payload []byte) error {
    if s == nil || s.DB == nil { return errors.New("store not initialized") }
    _, err := s.DB.Exec(`INSERT INTO outbox_events(event_type,payload,status,retry_count,next_retry_at,last_error,created_at)
        VALUES(?,?,"pending",0,?,"",?)`,
        eventType, payload, time.Now().Unix(), time.Now().Unix(),
    )
    return err
}

// FetchOutboxBatch 读取待发送事件（next_retry_at 到期）
func (s *Store) FetchOutboxBatch(limit int) ([]OutboxEvent, error) {
    if s == nil || s.DB == nil { return nil, errors.New("store not initialized") }
    if limit <= 0 { limit = 100 }
    rows, err := s.DB.Query(`SELECT id,event_type,payload,status,retry_count,next_retry_at,last_error,created_at
        FROM outbox_events
        WHERE status='pending' AND (next_retry_at IS NULL OR next_retry_at<=?)
        ORDER BY id ASC LIMIT ?`, time.Now().Unix(), limit)
    if err != nil { return nil, err }
    defer rows.Close()
    var list []OutboxEvent
    for rows.Next() {
        var e OutboxEvent
        var nextRetry, created int64
        if err := rows.Scan(&e.ID, &e.EventType, &e.Payload, &e.Status, &e.RetryCount, &nextRetry, &e.LastError, &created); err != nil {
            return nil, err
        }
        if nextRetry > 0 { e.NextRetryAt = time.Unix(nextRetry, 0) }
        if created > 0 { e.CreatedAt = time.Unix(created, 0) }
        list = append(list, e)
    }
    return list, nil
}

// MarkOutboxSent 标记一批事件已发送
func (s *Store) MarkOutboxSent(ids []int64) error {
    if len(ids) == 0 { return nil }
    tx, err := s.DB.Begin()
    if err != nil { return err }
    defer func(){ _ = tx.Rollback() }()
    stmt, err := tx.Prepare(`UPDATE outbox_events SET status='sent' WHERE id=?`)
    if err != nil { return err }
    defer stmt.Close()
    for _, id := range ids { if _, err := stmt.Exec(id); err != nil { return err } }
    return tx.Commit()
}

// MarkOutboxFail 标记事件发送失败并安排下次重试
func (s *Store) MarkOutboxFail(id int64, lastErr string, nextRetryAt time.Time) error {
    _, err := s.DB.Exec(`UPDATE outbox_events SET status='pending', retry_count=retry_count+1, last_error=?, next_retry_at=? WHERE id=?`,
        lastErr, nextRetryAt.Unix(), id,
    )
    return err
}

// ==== Backlog 功能已移除（统一用 outbox_events 支撑断网重放）====

// InsertTaskMeta 插入或更新任务元数据
func (s *Store) InsertTaskMeta(taskID, ttype, status, target, protocol string, startedAt, finishedAt time.Time, exitCode int, errMsg, resultPath string, bytes int64, tenantID, labels string) error {
    if s == nil || s.DB == nil {
        return errors.New("store not initialized")
    }
    _, err := s.DB.Exec(`INSERT INTO tasks(task_id,type,status,target,protocol,started_at,finished_at,exit_code,error_message,result_path,bytes,tenant_id,labels)
        VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(task_id) DO UPDATE SET
            type=excluded.type,
            status=excluded.status,
            target=excluded.target,
            protocol=excluded.protocol,
            started_at=excluded.started_at,
            finished_at=excluded.finished_at,
            exit_code=excluded.exit_code,
            error_message=excluded.error_message,
            result_path=excluded.result_path,
            bytes=excluded.bytes,
            tenant_id=excluded.tenant_id,
            labels=excluded.labels
    ;`,
        taskID, ttype, status, target, protocol,
        startedAt.Unix(), finishedAt.Unix(), exitCode, errMsg, resultPath, bytes,
        tenantID, labels,
    )
    return err
}

// InsertMetricsHistory 插入一条指标历史记录
func (s *Store) InsertMetricsHistory(ts time.Time, cpuUsage, memUsage float64, goroutines, tasksRunning, sessions int, dbSizeBytes, outboxPending int64) error {
    if s == nil || s.DB == nil { return errors.New("store not initialized") }
    _, err := s.DB.Exec(`INSERT INTO metrics_history(ts,cpu_usage,memory_usage,goroutines,tasks_running,sessions,db_size_bytes,outbox_pending) VALUES(?,?,?,?,?,?,?,?)`,
        ts.Unix(), cpuUsage, memUsage, goroutines, tasksRunning, sessions, dbSizeBytes, outboxPending,
    )
    return err
}
