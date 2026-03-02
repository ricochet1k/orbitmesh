package conformance

import "testing"

func TestBudgetGuard_NoLimitsAllowsUsage(t *testing.T) {
	guard := BudgetGuard{}
	if err := guard.Enforce(Usage{USD: 10, Tokens: 10000}); err != nil {
		t.Fatalf("Enforce() error = %v, want nil", err)
	}
}

func TestBudgetGuard_RejectsUSDOverLimit(t *testing.T) {
	guard := BudgetGuard{MaxUSD: 1.0}
	if err := guard.Enforce(Usage{USD: 1.1}); err == nil {
		t.Fatal("expected USD limit error")
	}
}

func TestBudgetGuard_RejectsTokenOverLimit(t *testing.T) {
	guard := BudgetGuard{MaxTokens: 100}
	if err := guard.Enforce(Usage{Tokens: 101}); err == nil {
		t.Fatal("expected token limit error")
	}
}
