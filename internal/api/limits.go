package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// IPRateLimiter implements per-IP rate limiting using token bucket algorithm
type IPRateLimiter struct {
	limits map[string]*tokenBucket
	mu     sync.RWMutex
	rate   time.Duration // Refill rate
	burst  int           // Burst capacity
}

// tokenBucket implements a simple token bucket
type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
	capacity   float64
}

// newIPRateLimiter creates a new IP-based rate limiter
func newIPRateLimiter(rate time.Duration, burst int) *IPRateLimiter {
	return &IPRateLimiter{
		limits: make(map[string]*tokenBucket),
		rate:   rate,
		burst:  burst,
	}
}

// allow checks if a request is allowed for the given IP
func (l *IPRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	bucket, exists := l.limits[ip]
	if !exists {
		bucket = &tokenBucket{
			tokens:     float64(l.burst),
			lastRefill: now,
			capacity:   float64(l.burst),
		}
		l.limits[ip] = bucket
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(bucket.lastRefill)
	refills := elapsed.Nanoseconds() / l.rate.Nanoseconds()
	if refills > 0 {
		bucket.tokens = min(bucket.capacity, bucket.tokens+float64(refills))
		bucket.lastRefill = now
	}

	// Check if we have tokens
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true
	}

	return false
}

// rateLimitMiddleware creates a Gin middleware for rate limiting
func rateLimitMiddleware(limiter *IPRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		if !limiter.allow(ip) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"message":     "Too many requests. Please try again later.",
				"retry_after": limiter.rate.String(),
			})
			return
		}

		c.Next()
	}
}

// bodyLimitMiddleware limits the size of request bodies
func bodyLimitMiddleware(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip for requests without body (GET, OPTIONS, etc.)
		if c.Request.ContentLength == 0 && c.Request.Method == http.MethodGet {
			c.Next()
			return
		}

		// Use MaxBytesReader to limit body size
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)

		// Try to parse the body, but ignore errors for GET requests
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			if err := c.Request.ParseMultipartForm(maxSize); err != nil {
				// Check if it's a size limit error
				if err.Error() == "http: request body too large" {
					c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
						"error":    "request body too large",
						"message":  "Request body exceeds maximum allowed size.",
						"max_size": maxSize,
					})
					return
				}
			}
		}

		c.Next()
	}
}
