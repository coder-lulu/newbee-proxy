package svc

import (
	"sync"
	"time"
)

// RateLimiter 速率限制器
type RateLimiter struct {
	rate     int       // 每秒允许的请求数
	capacity int       // 令牌桶容量
	tokens   int       // 当前令牌数
	lastTime time.Time // 上次更新时间
	mutex    sync.Mutex
}

// NewRateLimiter 创建速率限制器
func NewRateLimiter(requestsPerSecond int) *RateLimiter {
	return &RateLimiter{
		rate:     requestsPerSecond,
		capacity: requestsPerSecond,
		tokens:   requestsPerSecond,
		lastTime: time.Now(),
	}
}

// Allow 检查是否允许请求
func (rl *RateLimiter) Allow() bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastTime)

	// 添加令牌
	tokensToAdd := int(elapsed.Seconds() * float64(rl.rate))
	rl.tokens += tokensToAdd
	if rl.tokens > rl.capacity {
		rl.tokens = rl.capacity
	}

	rl.lastTime = now

	// 检查是否有可用令牌
	if rl.tokens > 0 {
		rl.tokens--
		return true
	}

	return false
}

// Wait 等待直到可以发送请求
func (rl *RateLimiter) Wait() {
	for !rl.Allow() {
		time.Sleep(time.Millisecond * 10)
	}
}

// GetStats 获取统计信息
func (rl *RateLimiter) GetStats() *RateLimiterStats {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	return &RateLimiterStats{
		Rate:     rl.rate,
		Capacity: rl.capacity,
		Tokens:   rl.tokens,
	}
}

// RateLimiterStats 速率限制器统计信息
type RateLimiterStats struct {
	Rate     int `json:"rate"`
	Capacity int `json:"capacity"`
	Tokens   int `json:"tokens"`
}
