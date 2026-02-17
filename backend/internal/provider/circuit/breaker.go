package circuit

import (
	"sync"
	"time"
)

// Breaker implements a circuit breaker pattern for failure handling.
// After a threshold of failures, it enters a cooldown period where
// operations are blocked.
type Breaker struct {
	mu              sync.RWMutex
	threshold       int
	cooldownPeriod  time.Duration
	failureCount    int
	cooldownUntil   time.Time
}

// NewBreaker creates a new circuit breaker with the specified threshold and cooldown.
func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{
		threshold:      threshold,
		cooldownPeriod: cooldown,
	}
}

// RecordFailure records a failure. Returns true if the breaker should enter cooldown.
func (cb *Breaker) RecordFailure() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++

	if cb.failureCount >= cb.threshold {
		cb.cooldownUntil = time.Now().Add(cb.cooldownPeriod)
		cb.failureCount = 0 // Reset after entering cooldown
		return true
	}

	return false
}

// IsInCooldown returns true if the breaker is currently in cooldown.
func (cb *Breaker) IsInCooldown() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return time.Now().Before(cb.cooldownUntil)
}

// CooldownRemaining returns the time remaining in the cooldown period.
// Returns 0 if not in cooldown.
func (cb *Breaker) CooldownRemaining() time.Duration {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if time.Now().Before(cb.cooldownUntil) {
		return time.Until(cb.cooldownUntil)
	}

	return 0
}

// Reset resets the failure count and clears cooldown.
func (cb *Breaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount = 0
	cb.cooldownUntil = time.Time{}
}

// FailureCount returns the current failure count.
func (cb *Breaker) FailureCount() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return cb.failureCount
}
