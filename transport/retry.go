package transport

import (
	"fmt"
	"time"
)

// RetryPolicy defines the retry behavior for transient failures.
// RetryPolicy 定义瞬态失败的重试行为。
type RetryPolicy struct {
	MaxRetries int           // max retry attempts / 最大重试次数
	Delay      time.Duration // delay between retries / 重试间隔
}

// DefaultRetryPolicy returns a sensible default retry policy (3 retries, 1s delay).
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxRetries: 3, Delay: 1 * time.Second}
}

// RetryableTransport wraps a Transport with automatic reconnection and retry logic.
// RetryableTransport 包装 Transport，提供自动重连和重试逻辑。
type RetryableTransport struct {
	inner  *SFTPTransport
	policy RetryPolicy
}

// NewRetryableTransport creates a retryable wrapper around an SFTPTransport.
func NewRetryableTransport(inner *SFTPTransport, policy RetryPolicy) *RetryableTransport {
	if policy.MaxRetries <= 0 {
		policy.MaxRetries = 3
	}
	if policy.Delay <= 0 {
		policy.Delay = 1 * time.Second
	}
	return &RetryableTransport{inner: inner, policy: policy}
}

// withRetry executes fn with retry + reconnect on failure.
// withRetry 带重试+重连执行 fn。
func (r *RetryableTransport) withRetry(op string, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= r.policy.MaxRetries; attempt++ {
		if attempt > 0 {
			// Reconnect before retry
			if err := r.inner.Reconnect(); err != nil {
				lastErr = fmt.Errorf("%s: reconnect failed (attempt %d/%d): %w", op, attempt, r.policy.MaxRetries, err)
				time.Sleep(r.policy.Delay)
				continue
			}
		}
		if err := fn(); err != nil {
			lastErr = err
			// Check if connection is still alive; if not, next iteration will reconnect
			if !r.inner.IsConnected() {
				time.Sleep(r.policy.Delay)
				continue
			}
			// Connection alive but operation failed — might be a transient error
			if attempt < r.policy.MaxRetries {
				time.Sleep(r.policy.Delay)
				continue
			}
			return fmt.Errorf("%s: failed after %d attempts: %w", op, attempt+1, lastErr)
		}
		return nil
	}
	return fmt.Errorf("%s: exhausted %d retries: %w", op, r.policy.MaxRetries, lastErr)
}

// EnsureConnected checks the connection and reconnects if needed.
// EnsureConnected 检查连接状态，必要时重连。
func (r *RetryableTransport) EnsureConnected() error {
	if r.inner.IsConnected() {
		return nil
	}
	return r.inner.Reconnect()
}

// Inner returns the underlying SFTPTransport for direct access.
func (r *RetryableTransport) Inner() *SFTPTransport {
	return r.inner
}
