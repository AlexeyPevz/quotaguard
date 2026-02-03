package router

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreakerInitialState(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Second)

	if cb.State() != CircuitClosed {
		t.Errorf("expected initial state to be closed, got %v", cb.State())
	}
}

func TestCircuitBreakerSuccess(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Second)

	err := cb.Execute(context.Background(), func() error {
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if cb.State() != CircuitClosed {
		t.Errorf("expected state to remain closed after success, got %v", cb.State())
	}
}

func TestCircuitBreakerFailureOpensCircuit(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Second)

	// Record 2 failures - circuit should still be closed
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Errorf("expected state to be closed after 2 failures, got %v", cb.State())
	}

	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Errorf("expected state to be closed after 2 failures, got %v", cb.State())
	}

	// Record 3rd failure - circuit should open
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("expected state to be open after 3 failures, got %v", cb.State())
	}
}

func TestCircuitBreakerExecuteWithFailure(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Second)
	testErr := errors.New("test error")

	err := cb.Execute(context.Background(), func() error {
		return testErr
	})

	if err != testErr {
		t.Errorf("expected error to be testErr, got %v", err)
	}
}

func TestCircuitBreakerOpenStateRejectsCalls(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 10*time.Second)

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("expected circuit to be open, got %v", cb.State())
	}

	// Try to execute - should be rejected
	err := cb.Execute(context.Background(), func() error {
		return nil
	})

	var circuitErr *CircuitOpenError
	if !errors.As(err, &circuitErr) {
		t.Errorf("expected CircuitOpenError, got %v", err)
	}

	metrics := cb.GetMetrics()
	if metrics.RejectedCalls != 1 {
		t.Errorf("expected 1 rejected call, got %d", metrics.RejectedCalls)
	}
}

func TestCircuitBreakerHalfOpenTransition(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 50*time.Millisecond)

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("expected circuit to be open, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Try to execute - should transition to half-open
	err := cb.Execute(context.Background(), func() error {
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if cb.State() != CircuitHalfOpen {
		t.Errorf("expected state to be half-open, got %v", cb.State())
	}
}

func TestCircuitBreakerClosesOnSuccessInHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 50*time.Millisecond)

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Execute successful calls
	for i := 0; i < 3; i++ {
		err := cb.Execute(context.Background(), func() error {
			return nil
		})
		if err != nil {
			t.Errorf("unexpected error on call %d: %v", i, err)
		}
	}

	if cb.State() != CircuitClosed {
		t.Errorf("expected state to be closed after successful half-open calls, got %v", cb.State())
	}
}

func TestCircuitBreakerReopensOnFailureInHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 50*time.Millisecond)

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Execute a successful call to transition to half-open
	err := cb.Execute(context.Background(), func() error {
		return nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Execute a failure
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("expected state to be open after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreakerReset(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, time.Second)

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("expected circuit to be open, got %v", cb.State())
	}

	// Reset
	cb.Reset()

	if cb.State() != CircuitClosed {
		t.Errorf("expected state to be closed after reset, got %v", cb.State())
	}

	metrics := cb.GetMetrics()
	if metrics.Failures != 0 {
		t.Errorf("expected failures to be 0 after reset, got %d", metrics.Failures)
	}
}

func TestCircuitBreakerContextCancellation(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cb.Execute(ctx, func() error {
		return nil
	})

	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

func TestCircuitBreakerMetrics(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Second)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()

	metrics := cb.GetMetrics()

	if metrics.State != "open" {
		t.Errorf("expected state to be 'open', got %s", metrics.State)
	}

	if metrics.Failures != 3 {
		t.Errorf("expected failures to be 3, got %d", metrics.Failures)
	}

	if metrics.Successes != 1 {
		t.Errorf("expected successes to be 1, got %d", metrics.Successes)
	}

	if metrics.StateTransitions != 1 {
		t.Errorf("expected 1 state transition, got %d", metrics.StateTransitions)
	}
}

func TestCircuitBreakerStateTransitions(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 50*time.Millisecond)

	initialTransitions := cb.GetMetrics().StateTransitions

	// Open circuit
	cb.RecordFailure()
	cb.RecordFailure()
	transitionsAfterOpen := cb.GetMetrics().StateTransitions
	if transitionsAfterOpen != initialTransitions+1 {
		t.Errorf("expected 1 state transition after opening, got %d", transitionsAfterOpen-initialTransitions)
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Transition to half-open via Execute
	if err := cb.Execute(context.Background(), func() error {
		return nil
	}); err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
	transitionsAfterHalfOpen := cb.GetMetrics().StateTransitions
	if transitionsAfterHalfOpen != initialTransitions+2 {
		t.Errorf("expected 1 state transition after half-open, got %d", transitionsAfterHalfOpen-initialTransitions)
	}
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	config := DefaultCircuitBreakerConfig()

	if config.FailureThreshold != 5 {
		t.Errorf("expected FailureThreshold to be 5, got %d", config.FailureThreshold)
	}

	if config.Timeout != 30*time.Second {
		t.Errorf("expected Timeout to be 30s, got %v", config.Timeout)
	}

	if config.HalfOpenLimit != 3 {
		t.Errorf("expected HalfOpenLimit to be 3, got %d", config.HalfOpenLimit)
	}
}

func TestCircuitBreakerConcurrency(t *testing.T) {
	cb := NewCircuitBreaker("test", 10, time.Second)

	// Run concurrent operations
	errCh := make(chan error, 100)
	for i := 0; i < 100; i++ {
		go func() {
			errCh <- cb.Execute(context.Background(), func() error { return nil })
		}()
	}

	for i := 0; i < 100; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("unexpected execute error: %v", err)
		}
	}

	// Should still be in closed state
	if cb.State() != CircuitClosed {
		t.Errorf("expected state to be closed after concurrent operations, got %v", cb.State())
	}
}

func TestCircuitBreakerErrorInterface(t *testing.T) {
	err := &CircuitOpenError{Name: "test-provider"}

	expected := "circuit breaker 'test-provider' is open"
	if err.Error() != expected {
		t.Errorf("expected error message '%s', got '%s'", expected, err.Error())
	}
}
