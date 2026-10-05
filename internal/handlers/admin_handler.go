package handlers

import (
    "database/sql"
    "encoding/json"
    "net/http"
    "time"

    "github.com/coder-lulu/newbee-proxy/internal/storage/sqlite"
    "github.com/coder-lulu/newbee-proxy/internal/svc"
)

// AdminHandler 提供简单管理接口：查询/设置运行状态（online/draining/offline）
// 保护：要求 Header: X-PSK 与配置 OpsCenter.PSK 一致；否则 401

type adminStateReq struct {
    State string `json:"state"`
}

type adminStateResp struct {
    State           string `json:"state"`
    AcceptingNew    bool   `json:"accepting_new"`
    ActiveTasks     int    `json:"active_tasks"`
    ActiveSessions  int    `json:"active_sessions"`
}

func AdminGetStateHandler(s *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !checkPSK(w, r, s) { return }
        tasks, sessions := s.GetActiveCounts()
        _ = json.NewEncoder(w).Encode(adminStateResp{
            State:          s.GetState(),
            AcceptingNew:   s.IsAcceptingNew(),
            ActiveTasks:    tasks,
            ActiveSessions: sessions,
        })
    }
}

func AdminSetStateHandler(s *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !checkPSK(w, r, s) { return }
        var req adminStateReq
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, "invalid json", http.StatusBadRequest)
            return
        }
        s.SetState(req.State)
        tasks, sessions := s.GetActiveCounts()
        _ = json.NewEncoder(w).Encode(adminStateResp{
            State:          s.GetState(),
            AcceptingNew:   s.IsAcceptingNew(),
            ActiveTasks:    tasks,
            ActiveSessions: sessions,
        })
    }
}

// AdminSetDrain 切换到 draining（拒绝新建会话/任务）
func AdminSetDrain(s *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !checkPSK(w, r, s) { return }
        s.SetState("draining")
        tasks, sessions := s.GetActiveCounts()
        _ = json.NewEncoder(w).Encode(adminStateResp{State: s.GetState(), AcceptingNew: s.IsAcceptingNew(), ActiveTasks: tasks, ActiveSessions: sessions})
    }
}

// AdminSetOnline 切换到 online（接受新建会话/任务）
func AdminSetOnline(s *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !checkPSK(w, r, s) { return }
        s.SetState("online")
        tasks, sessions := s.GetActiveCounts()
        _ = json.NewEncoder(w).Encode(adminStateResp{State: s.GetState(), AcceptingNew: s.IsAcceptingNew(), ActiveTasks: tasks, ActiveSessions: sessions})
    }
}

// AdminShutdown 触发优雅停机
func AdminShutdown(s *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !checkPSK(w, r, s) { return }
        go func(){
            s.DrainAndWait(15 * time.Second)
            _ = s.Stop()
        }()
        _ = json.NewEncoder(w).Encode(map[string]any{"accepted": true})
    }
}

func checkPSK(w http.ResponseWriter, r *http.Request, s *svc.ServiceContext) bool {
    want := s.Config.OpsCenter.PSK
    if want == "" {
        // 未配置 PSK，则放行（测试环境）
        return true
    }
    got := r.Header.Get("X-PSK")
    if got == "" {
        got = r.URL.Query().Get("psk")
    }
    if got != want {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return false
    }
    return true
}

// --------- Storage Stats ---------

type storageStats struct {
    DBSizeBytes   int64             `json:"db_size_bytes"`
    Rows          map[string]int64  `json:"rows"`
    OutboxPending int64             `json:"outbox_pending"`
    RecentErrors  []any             `json:"recent_errors"`
    DBPath        string            `json:"db_path"`
}

// AdminGetStorageStatsHandler 返回 SQLite 大小、行数与最近错误
func AdminGetStorageStatsHandler(s *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !checkPSK(w, r, s) { return }
        if s.Store == nil {
            http.Error(w, "storage disabled", http.StatusNotFound)
            return
        }
        store, ok := s.Store.(*sqlite.Store)
        if !ok {
            http.Error(w, "storage not sqlite", http.StatusNotImplemented)
            return
        }

        var resp storageStats
        resp.Rows = map[string]int64{}
        resp.DBPath = s.Config.Storage.DBPath

        if sz, err := store.DBSizeBytes(); err == nil { resp.DBSizeBytes = sz }
        // 统计行数（存在即可，出错忽略）
        resp.Rows["tasks"] = queryCount(store.DB, `SELECT COUNT(*) FROM tasks`)
        resp.Rows["sessions"] = queryCount(store.DB, `SELECT COUNT(*) FROM sessions`)
        resp.Rows["outbox_events"] = queryCount(store.DB, `SELECT COUNT(*) FROM outbox_events`)
        resp.Rows["config_cache"] = queryCount(store.DB, `SELECT COUNT(*) FROM config_cache`)
        resp.OutboxPending = queryCount(store.DB, `SELECT COUNT(*) FROM outbox_events WHERE status='pending'`)

        // 最近错误：任务失败 + outbox last_error
        var recent []any
        // 任务错误
        if rows, err := store.DB.Query(`SELECT task_id, error_message, finished_at FROM tasks WHERE status!='TASK_COMPLETED' AND error_message!='' ORDER BY finished_at DESC LIMIT 10`); err == nil {
            defer rows.Close()
            for rows.Next() {
                var id, msg string
                var finished int64
                _ = rows.Scan(&id, &msg, &finished)
                recent = append(recent, map[string]any{
                    "source": "task",
                    "task_id": id,
                    "message": msg,
                    "finished_at": finished,
                })
            }
        }
        // Outbox 错误
        if rows, err := store.DB.Query(`SELECT id, last_error, retry_count, next_retry_at FROM outbox_events WHERE last_error!='' ORDER BY id DESC LIMIT 10`); err == nil {
            defer rows.Close()
            for rows.Next() {
                var id int64
                var msg string
                var retry int
                var next int64
                _ = rows.Scan(&id, &msg, &retry, &next)
                recent = append(recent, map[string]any{
                    "source": "outbox",
                    "id": id,
                    "message": msg,
                    "retry_count": retry,
                    "next_retry_at": time.Unix(next, 0).Unix(),
                })
            }
        }
        resp.RecentErrors = recent

        _ = json.NewEncoder(w).Encode(resp)
    }
}

func queryCount(db *sql.DB, q string) int64 {
    if db == nil { return 0 }
    var n int64
    if err := db.QueryRow(q).Scan(&n); err != nil { return 0 }
    return n
}
