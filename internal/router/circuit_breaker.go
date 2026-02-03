package router

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/quotaguard/quotaguard/internal/config"
)

// CircuitState represents the state of the circuit breaker
type CircuitState int32

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	name            string
	state           atomic.Int32 // 0: closed, 1: open, 2: half-open
	failures        atomic.Int32
	successes       atomic.Int32
	lastFailure     atomic.Value // time.Time
	lastStateChange atomic.Value // time.Time
	threshold       int
	timeout         time.Duration
	halfOpenLimit   int

	// Metrics
	stateTransitions atomic.Int32
	rejectedCalls    atomic.Int32
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, threshold int, timeout time.Duration) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:          name,
		threshold:     threshold,
		timeout:       timeout,
		halfOpenLimit: 3,
	}
	cb.state.Store(int32(CircuitClosed))
	cb.lastStateChange.Store(time.Now())
	return cb
}

// Execute runs the function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	// Check if circuit is open
	if cb.state.Load() == int32(CircuitOpen) {
		// Check if timeout has passed
		if lastStateChange, ok := cb.lastStateChange.Load().(time.Time); ok {
			if time.Since(lastStateChange) > cb.timeout {
				// Transition to half-open
				if cb.state.CompareAndSwap(int32(CircuitOpen), int32(CircuitHalfOpen)) {
					cb.lastStateChange.Store(time.Now())
					cb.stateTransitions.Add(1)
				}
			}
		}
		// If still open, reject the call
		if cb.state.Load() == int32(CircuitOpen) {
			cb.rejectedCalls.Add(1)
			return &CircuitOpenError{Name: cb.name}
		}
	}

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Execute the function
	err := fn()

	if err != nil {
		cb.RecordFailure()
	} else {
		cb.RecordSuccess()
	}

	return err
}

// RecordSuccess records a successful call
func (cb *CircuitBreaker) RecordSuccess() {
	cb.successes.Add(1)

	// If in half-open state, check if we can close
	if cb.state.Load() == int32(CircuitHalfOpen) {
		if cb.successes.Load() >= int32(cb.halfOpenLimit) {
			cb.state.Store(int32(CircuitClosed))
			cb.lastStateChange.Store(time.Now())
			cb.failures.Store(0)
			cb.successes.Store(0)
			cb.stateTransitions.Add(1)
		}
	}
}

// RecordFailure records a failed call
func (cb *CircuitBreaker) RecordFailure() {
	cb.failures.Add(1)
	cb.lastFailure.Store(time.Now())

	// If in closed state and threshold reached, open the circuit
	if cb.state.Load() == int32(CircuitClosed) {
		if cb.failures.Load() >= int32(cb.threshold) {
			cb.state.Store(int32(CircuitOpen))
			cb.lastStateChange.Store(time.Now())
			cb.stateTransitions.Add(1)
		}
	}

	// If in half-open state, any failure opens the circuit
	if cb.state.Load() == int32(CircuitHalfOpen) {
		cb.state.Store(int32(CircuitOpen))
		cb.lastStateChange.Store(time.Now())
		cb.successes.Store(0)
		cb.stateTransitions.Add(1)
	}
}

// State returns the current state
func (cb *CircuitBreaker) State() CircuitState {
	return CircuitState(cb.state.Load())
}

// Reset resets the circuit breaker
func (cb *CircuitBreaker) Reset() {
	cb.state.Store(int32(CircuitClosed))
	cb.failures.Store(0)
	cb.successes.Store(0)
	cb.lastStateChange.Store(time.Now())
	cb.stateTransitions.Add(1)
}

// GetMetrics returns current metrics
func (cb *CircuitBreaker) GetMetrics() CircuitBreakerMetrics {
	return CircuitBreakerMetrics{
		State:            cb.State().String(),
		Failures:         int(cb.failures.Load()),
		Successes:        int(cb.successes.Load()),
		StateTransitions: int(cb.stateTransitions.Load()),
		RejectedCalls:    int(cb.rejectedCalls.Load()),
	}
}

// CircuitBreakerMetrics contains circuit breaker metrics
type CircuitBreakerMetrics struct {
	State            string
	Failures         int
	Successes        int
	StateTransitions int
	RejectedCalls    int
}

// CircuitOpenError is returned when the circuit is open
type CircuitOpenError struct {
	Name string
}

func (e *CircuitOpenError) Error() string {
	return "circuit breaker '" + e.Name + "' is open"
}

func (e *CircuitOpenError) Unwrap() error {
	return nil
}

// DefaultCircuitBreakerConfig returns default circuit breaker configuration
func DefaultCircuitBreakerConfig() config.CircuitBreakerConfig {
	return config.CircuitBreakerConfig{
		FailureThreshold: 5,
		Timeout:          30 * time.Second,
		HalfOpenLimit:    3,
	}
}
