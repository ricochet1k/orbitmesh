package acp

import (
	"context"
	"testing"
	"time"
)

func TestWithACPStartTimeout_AddsDefaultDeadlineWhenMissing(t *testing.T) {
	ctx, cancel := withACPStartTimeout(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline to be added")
	}
	remaining := time.Until(deadline)
	if remaining <= acpStartTimeout-2*time.Second || remaining > acpStartTimeout {
		t.Fatalf("deadline remaining = %v, want near %v", remaining, acpStartTimeout)
	}
}

func TestWithACPStartTimeout_PreservesCallerDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer parentCancel()

	ctx, cancel := withACPStartTimeout(parent)
	defer cancel()

	parentDeadline, _ := parent.Deadline()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected caller deadline to be preserved")
	}
	if !deadline.Equal(parentDeadline) {
		t.Fatalf("deadline = %v, want %v", deadline, parentDeadline)
	}
}
