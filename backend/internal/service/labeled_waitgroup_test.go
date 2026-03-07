package service

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestLabeledWaitGroup_WaitingOnAndWait(t *testing.T) {
	wg := newLabeledWaitGroup()
	release := make(chan struct{})
	var ran atomic.Int32

	wg.Go("session:s1", func() {
		ran.Add(1)
		<-release
	})

	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		labels := wg.WaitingOn()
		if len(labels) == 1 && labels[0] == "session:s1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected waiting label, got %v", labels)
		}
		time.Sleep(5 * time.Millisecond)
	}

	close(release)
	wg.Wait()

	if got := ran.Load(); got != 1 {
		t.Fatalf("run count = %d, want 1", got)
	}
	if labels := wg.WaitingOn(); len(labels) != 0 {
		t.Fatalf("expected no waiting labels, got %v", labels)
	}
}
