package timeout

import (
	"context"
	"testing"
	"time"
)

func TestDefaultTimeoutConfig(t *testing.T) {
	config := DefaultTimeoutConfig()

	if config.DefaultTimeout != 60*time.Second {
		t.Errorf("Expected DefaultTimeout to be 60s, got %v", config.DefaultTimeout)
	}

	if config.ConnectTimeout != 30*time.Second {
		t.Errorf("Expected ConnectTimeout to be 30s, got %v", config.ConnectTimeout)
	}
}

func TestNewTimeoutController(t *testing.T) {
	// 测试使用默认配置
	ctrl := NewTimeoutController(nil)
	if ctrl.config == nil {
		t.Error("Expected config to be initialized")
	}

	// 测试使用自定义配置
	config := &TimeoutConfig{
		DefaultTimeout: 10 * time.Second,
		MinTimeout:     1 * time.Second,
		MaxTimeout:     30 * time.Second,
	}
	ctrl = NewTimeoutController(config)
	if ctrl.config.DefaultTimeout != 10*time.Second {
		t.Errorf("Expected DefaultTimeout to be 10s, got %v", ctrl.config.DefaultTimeout)
	}
}

func TestNormalizeTimeout(t *testing.T) {
	config := &TimeoutConfig{
		DefaultTimeout: 10 * time.Second,
		MinTimeout:     5 * time.Second,
		MaxTimeout:     60 * time.Second,
	}
	ctrl := NewTimeoutController(config)

	// 测试0值，应该返回默认值
	result := ctrl.NormalizeTimeout(0)
	if result != 10*time.Second {
		t.Errorf("Expected 10s for zero timeout, got %v", result)
	}

	// 测试小于最小值
	result = ctrl.NormalizeTimeout(2 * time.Second)
	if result != 5*time.Second {
		t.Errorf("Expected 5s for timeout below minimum, got %v", result)
	}

	// 测试大于最大值
	result = ctrl.NormalizeTimeout(120 * time.Second)
	if result != 60*time.Second {
		t.Errorf("Expected 60s for timeout above maximum, got %v", result)
	}

	// 测试正常值
	result = ctrl.NormalizeTimeout(30 * time.Second)
	if result != 30*time.Second {
		t.Errorf("Expected 30s for normal timeout, got %v", result)
	}
}

func TestCreateTimeoutContext(t *testing.T) {
	config := &TimeoutConfig{
		DefaultTimeout: 10 * time.Second,
		MinTimeout:     5 * time.Second,
		MaxTimeout:     60 * time.Second,
	}
	ctrl := NewTimeoutController(config)

	ctx, cancel := ctrl.CreateTimeoutContext(context.Background(), 30*time.Second)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Error("Expected context to have deadline")
	}

	// 验证deadline是否大致正确（允许1秒误差）
	expectedDeadline := time.Now().Add(30 * time.Second)
	if deadline.Before(expectedDeadline.Add(-1*time.Second)) || deadline.After(expectedDeadline.Add(1*time.Second)) {
		t.Errorf("Expected deadline around %v, got %v", expectedDeadline, deadline)
	}
}

func TestGetTimeoutByType(t *testing.T) {
	ctrl := NewTimeoutController(nil)

	// 测试各种超时类型
	testCases := []struct {
		timeoutType TimeoutType
		expected    time.Duration
	}{
		{TimeoutTypeConnect, 30 * time.Second},
		{TimeoutTypeRead, 30 * time.Second},
		{TimeoutTypeWrite, 30 * time.Second},
		{TimeoutTypeQuery, 60 * time.Second},
		{TimeoutTypeExec, 300 * time.Second},
		{TimeoutTypeSession, 2 * time.Hour},
		{TimeoutTypeIdle, 30 * time.Minute},
		{TimeoutTypeWebSocket, 30 * time.Second},
	}

	for _, tc := range testCases {
		result := ctrl.GetTimeoutByType(tc.timeoutType)
		if result != tc.expected {
			t.Errorf("Expected %v for type %s, got %v", tc.expected, tc.timeoutType, result)
		}
	}
}

func TestValidateTimeout(t *testing.T) {
	config := &TimeoutConfig{
		DefaultTimeout: 10 * time.Second,
		MinTimeout:     5 * time.Second,
		MaxTimeout:     60 * time.Second,
	}
	ctrl := NewTimeoutController(config)

	// 测试负数
	err := ctrl.ValidateTimeout(-1 * time.Second)
	if err == nil {
		t.Error("Expected error for negative timeout")
	}

	// 测试小于最小值
	err = ctrl.ValidateTimeout(2 * time.Second)
	if err == nil {
		t.Error("Expected error for timeout below minimum")
	}

	// 测试大于最大值
	err = ctrl.ValidateTimeout(120 * time.Second)
	if err == nil {
		t.Error("Expected error for timeout above maximum")
	}

	// 测试正常值
	err = ctrl.ValidateTimeout(30 * time.Second)
	if err != nil {
		t.Errorf("Expected no error for valid timeout, got %v", err)
	}

	// 测试0值（应该通过）
	err = ctrl.ValidateTimeout(0)
	if err != nil {
		t.Errorf("Expected no error for zero timeout, got %v", err)
	}
}

func TestWebSocketTimeouts(t *testing.T) {
	ctrl := NewTimeoutController(nil)

	timeouts := ctrl.GetWebSocketTimeouts()

	if timeouts.PingTimeout != 60*time.Second {
		t.Errorf("Expected PingTimeout to be 60s, got %v", timeouts.PingTimeout)
	}

	if timeouts.PongTimeout != 10*time.Second {
		t.Errorf("Expected PongTimeout to be 10s, got %v", timeouts.PongTimeout)
	}

	if timeouts.MessageTimeout != 30*time.Second {
		t.Errorf("Expected MessageTimeout to be 30s, got %v", timeouts.MessageTimeout)
	}

	if timeouts.HandshakeTimeout != 10*time.Second {
		t.Errorf("Expected HandshakeTimeout to be 10s, got %v", timeouts.HandshakeTimeout)
	}
}
