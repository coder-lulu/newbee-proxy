package metrics

import (
    "sync/atomic"

    "github.com/prometheus/client_golang/prometheus"
)

// 任务执行相关指标
var (
    taskQueueLenGauge = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: "worker", Subsystem: "task", Name: "queue_length", Help: "Tasks queued (pending in channel)",
    })
    taskResultQueueLenGauge = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: "worker", Subsystem: "task", Name: "result_queue_length", Help: "Task results queued for reporting",
    })
    taskRunningGauge = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: "worker", Subsystem: "task", Name: "running", Help: "Currently running tasks",
    })
    taskMaxConcurrencyGauge = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: "worker", Subsystem: "task", Name: "max_concurrency", Help: "Executor max concurrent tasks",
    })
    taskSubmittedCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "task", Name: "submitted_total", Help: "Total tasks submitted",
    })
    taskCompletedCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "task", Name: "completed_total", Help: "Total tasks completed",
    })
    taskFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "task", Name: "failed_total", Help: "Total tasks failed",
    })
    taskTimeoutCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "task", Name: "timeout_total", Help: "Total tasks timeout",
    })
    taskCancelledCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "task", Name: "cancelled_total", Help: "Total tasks cancelled",
    })
    taskRetriesCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "task", Name: "retries_total", Help: "Total task retries",
    })
    taskQueueRejectsCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "task", Name: "queue_rejects_total", Help: "Total task submissions rejected due to full queue",
    })
    taskThrottleGauge = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: "worker", Subsystem: "task", Name: "throttle_active", Help: "Whether task admission throttle is active (1) or not (0)",
    })
    taskAdmitDelaySeconds = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "task", Name: "admit_delay_seconds_total", Help: "Total seconds of admission delay applied before scheduling tasks",
    })
    taskDurationHist = prometheus.NewHistogram(prometheus.HistogramOpts{
        Namespace: "worker", Subsystem: "task", Name: "duration_seconds", Help: "Task execution duration in seconds",
        Buckets:   prometheus.ExponentialBuckets(0.05, 2, 12), // ~50ms .. 102s
    })

    submittedSnap int64
)

// 数据库相关指标
var (
    dbSizeGauge = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: "worker", Subsystem: "db", Name: "size_bytes", Help: "SQLite DB file size in bytes",
    })
    dbOutboxPendingGauge = prometheus.NewGauge(prometheus.GaugeOpts{
        Namespace: "worker", Subsystem: "db", Name: "outbox_pending", Help: "Pending outbox events",
    })

    // SFTP 相关指标
    sftpBytesCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "sftp", Name: "bytes_total", Help: "SFTP transferred bytes",
    }, []string{"direction"})
    sftpOpsCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "sftp", Name: "ops_total", Help: "SFTP operations count",
    }, []string{"direction"})
    sftpDurationHist = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Namespace: "worker", Subsystem: "sftp", Name: "duration_seconds", Help: "SFTP transfer duration seconds",
        Buckets:   prometheus.ExponentialBuckets(0.05, 2, 12),
    }, []string{"direction"})
    sftpErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "sftp", Name: "errors_total", Help: "SFTP errors by direction",
    }, []string{"direction"})
    // Outbox 重放指标
    outboxReplayedCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "outbox", Name: "replayed_total", Help: "Total outbox events successfully replayed",
    })
    outboxFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "outbox", Name: "failed_total", Help: "Total outbox events failed to replay",
    })
    outboxRetryCounter = prometheus.NewCounter(prometheus.CounterOpts{
        Namespace: "worker", Subsystem: "outbox", Name: "retry_total", Help: "Total outbox retries scheduled",
    })
    outboxReplayBatchSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
        Namespace: "worker", Subsystem: "outbox", Name: "replay_batch_seconds", Help: "Duration of one outbox replay batch",
        Buckets:   prometheus.ExponentialBuckets(0.05, 2, 10),
    })
)

// RegisterTaskMetrics 供启动时注册（由 RegisterPrometheus 间接调用）
func RegisterTaskMetrics() {
    _ = prometheus.Register(taskQueueLenGauge)
    _ = prometheus.Register(taskResultQueueLenGauge)
    _ = prometheus.Register(taskRunningGauge)
    _ = prometheus.Register(taskMaxConcurrencyGauge)
    _ = prometheus.Register(taskSubmittedCounter)
    _ = prometheus.Register(taskCompletedCounter)
    _ = prometheus.Register(taskFailedCounter)
    _ = prometheus.Register(taskTimeoutCounter)
    _ = prometheus.Register(taskCancelledCounter)
    _ = prometheus.Register(taskRetriesCounter)
    _ = prometheus.Register(taskQueueRejectsCounter)
    _ = prometheus.Register(taskThrottleGauge)
    _ = prometheus.Register(taskAdmitDelaySeconds)
    _ = prometheus.Register(taskDurationHist)
    _ = prometheus.Register(dbSizeGauge)
    _ = prometheus.Register(dbOutboxPendingGauge)
    _ = prometheus.Register(sftpBytesCounter)
    _ = prometheus.Register(sftpOpsCounter)
    _ = prometheus.Register(sftpDurationHist)
    _ = prometheus.Register(sftpErrorCounter)
    _ = prometheus.Register(outboxReplayedCounter)
    _ = prometheus.Register(outboxFailedCounter)
    _ = prometheus.Register(outboxRetryCounter)
    _ = prometheus.Register(outboxReplayBatchSeconds)
}

// ------- 对外更新 API -------
func SetTaskQueueLength(n int)                 { taskQueueLenGauge.Set(float64(n)) }
func SetTaskResultQueueLength(n int)           { taskResultQueueLenGauge.Set(float64(n)) }
func SetTaskRunning(n int)                     { taskRunningGauge.Set(float64(n)) }
func SetTaskMaxConcurrency(n int)              { taskMaxConcurrencyGauge.Set(float64(n)) }
func IncTaskSubmitted()                        { taskSubmittedCounter.Inc(); atomic.AddInt64(&submittedSnap, 1) }
func IncTaskCompleted()                        { taskCompletedCounter.Inc() }
func IncTaskFailed()                           { taskFailedCounter.Inc() }
func IncTaskTimeout()                          { taskTimeoutCounter.Inc() }
func IncTaskCancelled()                        { taskCancelledCounter.Inc() }
func AddTaskRetries(n int)                     { if n > 0 { taskRetriesCounter.Add(float64(n)) } }
func ObserveTaskDurationSeconds(seconds float64) { taskDurationHist.Observe(seconds) }
func IncTaskQueueRejects()                       { taskQueueRejectsCounter.Inc() }
func SetTaskThrottleActive(active bool)          { if active { taskThrottleGauge.Set(1) } else { taskThrottleGauge.Set(0) } }
func AddTaskAdmitDelaySeconds(s float64)         { if s > 0 { taskAdmitDelaySeconds.Add(s) } }

// ------- DB 指标更新 API -------
func SetDBSizeBytes(n int64)        { dbSizeGauge.Set(float64(n)) }
func SetOutboxPending(n int64)      { dbOutboxPendingGauge.Set(float64(n)) }

// ------- SFTP 指标 API -------
func AddSFTPBytes(dir string, n int64)          { sftpBytesCounter.WithLabelValues(dir).Add(float64(n)) }
func IncSFTPOps(dir string)                      { sftpOpsCounter.WithLabelValues(dir).Inc() }
func ObserveSFTPSeconds(dir string, s float64)   { sftpDurationHist.WithLabelValues(dir).Observe(s) }
func IncSFTPError(dir string)                    { sftpErrorCounter.WithLabelValues(dir).Inc() }

// ------- Outbox 指标 API -------
func AddOutboxReplayed(n int)              { if n > 0 { outboxReplayedCounter.Add(float64(n)) } }
func AddOutboxFailed(n int)                { if n > 0 { outboxFailedCounter.Add(float64(n)) } }
func AddOutboxRetry(n int)                 { if n > 0 { outboxRetryCounter.Add(float64(n)) } }
func ObserveOutboxReplayBatchSeconds(s float64) { outboxReplayBatchSeconds.Observe(s) }
