package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// CircuitBreakerState represents the current state of the circuit breaker
type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateOpen
	StateHalfOpen
)

// CircuitBreaker wraps Redis client with circuit breaker pattern
type CircuitBreaker struct {
	client            redis.UniversalClient
	logger            *logrus.Logger
	state             CircuitBreakerState
	failureCount      int
	successCount      int
	lastFailureTime   time.Time
	mu                sync.RWMutex
	maxFailures       int           // Open circuit after N failures
	resetTimeout      time.Duration // Wait before trying half-open
	halfOpenSuccesses int           // Required successes to close circuit
}

// NewCircuitBreaker creates a new circuit breaker for Redis
func NewCircuitBreaker(client redis.UniversalClient, logger *logrus.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		client:            client,
		logger:            logger,
		state:             StateClosed,
		maxFailures:       5,                // Open after 5 consecutive failures
		resetTimeout:      10 * time.Second, // Try half-open after 10s
		halfOpenSuccesses: 3,                // Need 3 successes to close
	}
}

// Execute runs a Redis command with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	cb.mu.RLock()
	state := cb.state
	cb.mu.RUnlock()

	// If circuit is open, check if we should try half-open
	if state == StateOpen {
		cb.mu.Lock()
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.state = StateHalfOpen
			cb.successCount = 0
			cb.logger.Info("Circuit breaker: OPEN → HALF_OPEN (retry attempt)")
		} else {
			cb.mu.Unlock()
			return fmt.Errorf("circuit breaker is OPEN, refusing Redis call (Redis likely overloaded)")
		}
		cb.mu.Unlock()
	}

	// Execute the Redis command
	err := fn()

	// Update circuit breaker state based on result
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.onFailure(err)
		return err
	}

	cb.onSuccess()
	return nil
}

// onFailure handles a failed Redis operation
func (cb *CircuitBreaker) onFailure(err error) {
	cb.failureCount++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failureCount >= cb.maxFailures {
			cb.state = StateOpen
			cb.logger.WithFields(logrus.Fields{
				"failure_count": cb.failureCount,
				"error":         err.Error(),
			}).Error("Circuit breaker: CLOSED → OPEN (Redis overloaded)")
		}

	case StateHalfOpen:
		cb.state = StateOpen
		cb.failureCount = 0
		cb.logger.WithError(err).Error("Circuit breaker: HALF_OPEN → OPEN (Redis still unhealthy)")
	}
}

// onSuccess handles a successful Redis operation
func (cb *CircuitBreaker) onSuccess() {
	cb.successCount++

	switch cb.state {
	case StateClosed:
		// Reset failure count on success in closed state
		if cb.failureCount > 0 {
			cb.failureCount = 0
		}

	case StateHalfOpen:
		if cb.successCount >= cb.halfOpenSuccesses {
			cb.state = StateClosed
			cb.failureCount = 0
			cb.successCount = 0
			cb.logger.Info("Circuit breaker: HALF_OPEN → CLOSED (Redis recovered)")
		}
	}
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns current circuit breaker statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	stateStr := "CLOSED"
	switch cb.state {
	case StateOpen:
		stateStr = "OPEN"
	case StateHalfOpen:
		stateStr = "HALF_OPEN"
	}

	return map[string]interface{}{
		"state":         stateStr,
		"failure_count": cb.failureCount,
		"success_count": cb.successCount,
		"max_failures":  cb.maxFailures,
		"last_failure":  cb.lastFailureTime,
		"reset_timeout": cb.resetTimeout.String(),
	}
}
