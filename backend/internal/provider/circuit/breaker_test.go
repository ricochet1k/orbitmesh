package circuit

import (
	"testing"
	"time"
)

func TestCircuitBreaker_RecordFailure(t *testing.T) {
	cb := NewBreaker(3, 100*time.Millisecond)

	// Record failures below threshold
	if cb.RecordFailure() {
		t.Error("should not enter cooldown on first failure")
	}

	if cb.FailureCount() != 1 {
		t.Errorf("expected failure count 1, got %d", cb.FailureCount())
	}

	if cb.RecordFailure() {
		t.Error("should not enter cooldown on second failure")
	}

	if cb.FailureCount() != 2 {
		t.Errorf("expected failure count 2, got %d", cb.FailureCount())
	}

	// Third failure should trigger cooldown
	if !cb.RecordFailure() {
		t.Error("should enter cooldown on third failure")
	}

	// Failure count should reset after cooldown
	if cb.FailureCount() != 0 {
		t.Errorf("expected failure count to reset to 0, got %d", cb.FailureCount())
	}

	// Should be in cooldown
	if !cb.IsInCooldown() {
		t.Error("should be in cooldown after threshold reached")
	}
}

func TestCircuitBreaker_Cooldown(t *testing.T) {
	cb := NewBreaker(1, 100*time.Millisecond)

	// Trigger cooldown
	cb.RecordFailure()

	if !cb.IsInCooldown() {
		t.Error("should be in cooldown")
	}

	remaining := cb.CooldownRemaining()
	if remaining <= 0 || remaining > 100*time.Millisecond {
		t.Errorf("unexpected cooldown remaining: %v", remaining)
	}

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)

	if cb.IsInCooldown() {
		t.Error("should not be in cooldown after expiry")
	}

	if cb.CooldownRemaining() != 0 {
		t.Errorf("expected 0 cooldown remaining, got %v", cb.CooldownRemaining())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewBreaker(3, 100*time.Millisecond)

	// Record some failures
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.FailureCount() != 2 {
		t.Errorf("expected failure count 2, got %d", cb.FailureCount())
	}

	// Reset
	cb.Reset()

	if cb.FailureCount() != 0 {
		t.Errorf("expected failure count 0 after reset, got %d", cb.FailureCount())
	}

	if cb.IsInCooldown() {
		t.Error("should not be in cooldown after reset")
	}
}
