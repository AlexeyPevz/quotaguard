package alerts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewThrottler(t *testing.T) {
	throttler := NewThrottler(60, 60)
	assert.NotNil(t, throttler)
	assert.Equal(t, 1.0, throttler.rate) // 60 per minute = 1 per second
	assert.Equal(t, float64(60), throttler.bucketSize)
	assert.Equal(t, float64(60), throttler.tokens)
}

func TestNewThrottlerDefaults(t *testing.T) {
	throttler := NewThrottler(0, 0)
	assert.NotNil(t, throttler)
	assert.Equal(t, 0.5, throttler.rate) // 30 per minute = 0.5 per second
	assert.Equal(t, float64(30), throttler.bucketSize)
}

func TestAllow(t *testing.T) {
	throttler := NewThrottler(60, 10)

	// Should allow first 10 requests
	for i := 0; i < 10; i++ {
		assert.True(t, throttler.Allow(), "Request %d should be allowed", i+1)
	}

	// 11th request should be denied
	assert.False(t, throttler.Allow())
}

func TestAllowN(t *testing.T) {
	throttler := NewThrottler(60, 10)

	// Should allow 5 requests at once
	assert.True(t, throttler.AllowN(5))

	// Should allow another 5
	assert.True(t, throttler.AllowN(5))

	// Should deny 1 more
	assert.False(t, throttler.AllowN(1))
}

func TestGetRetryAfter(t *testing.T) {
	throttler := NewThrottler(60, 1) // 1 token bucket

	// Initially should be 0
	assert.Equal(t, time.Duration(0), throttler.GetRetryAfter())

	// Use the token
	throttler.Allow()

	// Should need to wait about 1 second
	retryAfter := throttler.GetRetryAfter()
	assert.Greater(t, retryAfter, time.Duration(0))
	assert.Less(t, retryAfter, 2*time.Second)
}

func TestGetTokens(t *testing.T) {
	throttler := NewThrottler(60, 10)

	// Initially should have 10 tokens
	assert.InDelta(t, float64(10), throttler.GetTokens(), 0.01)

	// Use 3 tokens
	throttler.AllowN(3)
	assert.InDelta(t, float64(7), throttler.GetTokens(), 0.01)
}

func TestReset(t *testing.T) {
	throttler := NewThrottler(60, 10)

	// Use some tokens
	throttler.AllowN(5)
	assert.InDelta(t, float64(5), throttler.GetTokens(), 0.01)

	// Reset
	throttler.Reset()
	assert.InDelta(t, float64(10), throttler.GetTokens(), 0.01)
}

func TestRefill(t *testing.T) {
	throttler := NewThrottler(600, 10) // 10 per second

	// Use all tokens
	throttler.AllowN(10)
	assert.InDelta(t, float64(0), throttler.GetTokens(), 0.001)

	// Wait for refill
	time.Sleep(150 * time.Millisecond)

	// Should have some tokens back
	tokens := throttler.GetTokens()
	assert.Greater(t, tokens, float64(0))
}

func TestRefillCap(t *testing.T) {
	throttler := NewThrottler(60, 5)

	// Wait a long time
	time.Sleep(100 * time.Millisecond)

	// Should not exceed bucket size
	assert.LessOrEqual(t, throttler.GetTokens(), float64(5))
}

func TestThrottlerConcurrency(t *testing.T) {
	throttler := NewThrottler(60000, 1000) // High rate limit

	// Run concurrent allows
	done := make(chan int, 100)
	allowed := 0

	for i := 0; i < 100; i++ {
		go func() {
			if throttler.Allow() {
				done <- 1
			} else {
				done <- 0
			}
		}()
	}

	// Count results
	for i := 0; i < 100; i++ {
		allowed += <-done
	}

	// Should have allowed up to bucket size
	assert.LessOrEqual(t, allowed, 1000)
}
