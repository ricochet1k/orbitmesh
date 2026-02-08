package pty

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/terminal"
	"github.com/ricochet1k/termemu"
)

type replayReaderBackend struct {
	r        *bytes.Reader
	done     chan struct{}
	doneOnce sync.Once
}

func newReplayReaderBackend(data []byte) *replayReaderBackend {
	return &replayReaderBackend{
		r:    bytes.NewReader(data),
		done: make(chan struct{}),
	}
}

func (b *replayReaderBackend) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if errors.Is(err, io.EOF) {
		b.signalDone()
	}
	return n, err
}

func (b *replayReaderBackend) Write(p []byte) (int, error) {
	return len(p), nil
}

func (b *replayReaderBackend) SetSize(w, h int) error {
	return nil
}

func (b *replayReaderBackend) signalDone() {
	b.doneOnce.Do(func() {
		close(b.done)
	})
}

func (b *replayReaderBackend) waitDone(timeout time.Duration) {
	select {
	case <-b.done:
		return
	case <-time.After(timeout):
		b.signalDone()
	}
}

func ReplayActivityFromPTYLog(path string, startOffset int64, extractor *ScreenDiffExtractor) (int64, PTYLogDiagnostics, error) {
	if extractor == nil {
		return startOffset, PTYLogDiagnostics{}, errors.New("missing extractor")
	}
	var payload bytes.Buffer
	offset, diag, err := ReplayPTYLog(path, startOffset, func(frame PTYLogFrame) error {
		if frame.Direction != ptyLogDirectionOut {
			return nil
		}
		_, _ = payload.Write(frame.Payload)
		return nil
	})

	backend := newReplayReaderBackend(payload.Bytes())
	events := make(chan terminalEvent, terminalEventBufferSize)
	frontend := newTerminalFrontend(events, backend.done)
	term := termemu.NewWithMode(frontend, backend, termemu.TextReadModeRune)
	if term == nil {
		return startOffset, PTYLogDiagnostics{}, errors.New("failed to initialize termemu terminal")
	}
	_ = term.Resize(80, 24)

	var hadUpdates atomic.Bool
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		for {
			select {
			case <-backend.done:
				for {
					select {
					case event := <-events:
						if handleReplayEvent(term, extractor, event) {
							hadUpdates.Store(true)
						}
					default:
						return
					}
				}
			case event := <-events:
				if handleReplayEvent(term, extractor, event) {
					hadUpdates.Store(true)
				}
			}
		}
	}()

	backend.waitDone(500 * time.Millisecond)
	<-errCh
	if !hadUpdates.Load() {
		if snapshot, ok := snapshotFromTerminal(term); ok {
			_ = extractor.HandleUpdate(terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &snapshot})
		}
	}
	if err != nil {
		return offset, diag, err
	}
	return offset, diag, nil
}

func handleReplayEvent(term termemu.Terminal, extractor *ScreenDiffExtractor, event terminalEvent) bool {
	switch event.kind {
	case terminalEventScrollLines:
		if snapshot, ok := snapshotFromTerminal(term); ok {
			_ = extractor.HandleUpdate(terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &snapshot})
			return true
		}
	case terminalEventRegionChanged:
		if diff, ok := buildTerminalDiffFrom(term, event.region, event.reason); ok {
			_ = extractor.HandleUpdate(terminal.Update{Kind: terminal.UpdateDiff, Diff: &diff})
			return true
		}
	}
	return false
}
