package collector

import "time"

type RateLimitError struct {
	RetryAfter time.Duration
	Message    string
}

func (e *RateLimitError) Error() string {
	if e == nil {
		return "rate limit"
	}
	if e.Message != "" {
		return e.Message
	}
	return "rate limit exceeded"
}
