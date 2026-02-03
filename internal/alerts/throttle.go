package alerts

import (
	"sync"
	"time"
)

// Throttler implements token bucket rate limiting
type Throttler struct {
	rate       float64 // tokens per second
	bucketSize float64 // max tokens
	tokens     float64 // current tokens
	lastUpdate time.Time
	mu         sync.Mutex
}

// NewThrottler creates a new throttler
func NewThrottler(ratePerMinute int, bucketSize int) *Throttler {
	if ratePerMinute <= 0 {
		ratePerMinute = 30 // default 30 per minute
	}
	if bucketSize <= 0 {
		bucketSize = ratePerMinute
	}

	ratePerSecond := float64(ratePerMinute) / 60.0

	return &Throttler{
		rate:       ratePerSecond,
		bucketSize: float64(bucketSize),
		tokens:     float64(bucketSize),
		lastUpdate: time.Now(),
	}
}

// Allow checks if an action is allowed
func (t *Throttler) Allow() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.refill()

	if t.tokens >= 1 {
		t.tokens--
		return true
	}
	return false
}

// AllowN checks if n actions are allowed
func (t *Throttler) AllowN(n int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.refill()

	if t.tokens >= float64(n) {
		t.tokens -= float64(n)
		return true
	}
	return false
}

// GetRetryAfter returns the time until the next token is available
func (t *Throttler) GetRetryAfter() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tokens >= 1 {
		return 0
	}

	needed := 1 - t.tokens
	seconds := needed / t.rate
	return time.Duration(seconds * float64(time.Second))
}

// refill adds tokens based on elapsed time
func (t *Throttler) refill() {
	now := time.Now()
	elapsed := now.Sub(t.lastUpdate).Seconds()
	t.lastUpdate = now

	t.tokens += t.rate * elapsed
	if t.tokens > t.bucketSize {
		t.tokens = t.bucketSize
	}
}

// GetTokens returns the current number of tokens
func (t *Throttler) GetTokens() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.refill()
	return t.tokens
}

// Reset resets the throttler to full tokens
func (t *Throttler) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.tokens = t.bucketSize
	t.lastUpdate = time.Now()
}
