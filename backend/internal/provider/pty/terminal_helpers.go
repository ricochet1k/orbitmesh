package pty

import (
	"github.com/ricochet1k/orbitmesh/internal/terminal"
	"github.com/ricochet1k/termemu"
)

func snapshotFromTerminal(term termemu.Terminal) (terminal.Snapshot, bool) {
	if term == nil {
		return terminal.Snapshot{}, false
	}
	var snapshot terminal.Snapshot
	term.WithLock(func() {
		w, h := term.Size()
		if w <= 0 || h <= 0 {
			return
		}
		lines := make([]string, h)
		for y := 0; y < h; y++ {
			lines[y] = term.Line(y)
		}
		snapshot = terminal.Snapshot{Rows: h, Cols: w, Lines: lines}
	})
	if snapshot.Rows == 0 || snapshot.Cols == 0 {
		return terminal.Snapshot{}, false
	}
	return snapshot, true
}

func buildTerminalDiffFrom(term termemu.Terminal, region termemu.Region, reason termemu.ChangeReason) (terminal.Diff, bool) {
	if term == nil {
		return terminal.Diff{}, false
	}
	var diff terminal.Diff
	term.WithLock(func() {
		w, h := term.Size()
		if w <= 0 || h <= 0 {
			return
		}
		bounded := termemu.Region{X: 0, Y: 0, X2: w, Y2: h}
		clamped := region.Intersect(bounded)
		if clamped.Empty() {
			return
		}
		lines := make([]string, clamped.Y2-clamped.Y)
		for y := clamped.Y; y < clamped.Y2; y++ {
			lines[y-clamped.Y] = term.Line(y)
		}
		diff = terminal.Diff{
			Region: terminal.Region{X: clamped.X, Y: clamped.Y, X2: clamped.X2, Y2: clamped.Y2},
			Lines:  lines,
			Reason: changeReasonString(reason),
		}
	})
	if diff.Lines == nil {
		return terminal.Diff{}, false
	}
	return diff, true
}
