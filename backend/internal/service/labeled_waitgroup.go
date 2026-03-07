package service

import (
	"sort"
	"sync"
)

// labeledWaitGroup tracks in-flight goroutines with human-readable labels.
// It is intended for shutdown orchestration and diagnostics.
type labeledWaitGroup struct {
	mu     sync.Mutex
	cond   *sync.Cond
	nextID uint64
	active map[uint64]string
}

func newLabeledWaitGroup() *labeledWaitGroup {
	wg := &labeledWaitGroup{
		active: make(map[uint64]string),
	}
	wg.cond = sync.NewCond(&wg.mu)
	return wg
}

func (wg *labeledWaitGroup) Go(title string, fn func()) {
	wg.mu.Lock()
	id := wg.nextID
	wg.nextID++
	wg.active[id] = title
	wg.mu.Unlock()

	go func() {
		defer func() {
			wg.mu.Lock()
			delete(wg.active, id)
			wg.cond.Broadcast()
			wg.mu.Unlock()
		}()
		fn()
	}()
}

func (wg *labeledWaitGroup) Wait() {
	wg.mu.Lock()
	defer wg.mu.Unlock()
	for len(wg.active) > 0 {
		wg.cond.Wait()
	}
}

func (wg *labeledWaitGroup) WaitingOn() []string {
	wg.mu.Lock()
	defer wg.mu.Unlock()
	labels := make([]string, 0, len(wg.active))
	for _, title := range wg.active {
		labels = append(labels, title)
	}
	sort.Strings(labels)
	return labels
}
