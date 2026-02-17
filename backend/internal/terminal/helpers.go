package terminal

import (
	"github.com/ricochet1k/termemu"
)

func SnapshotFromTerminal(term termemu.Terminal) (Snapshot, bool) {
	if term == nil {
		return Snapshot{}, false
	}
	var snapshot Snapshot
	term.WithLock(func() {
		w, h := term.Size()
		if w <= 0 || h <= 0 {
			return
		}
		lines := make([]string, h)
		for y := 0; y < h; y++ {
			lines[y] = term.Line(y)
		}
		snapshot = Snapshot{Rows: h, Cols: w, Lines: lines}
	})
	if snapshot.Rows == 0 || snapshot.Cols == 0 {
		return Snapshot{}, false
	}
	return snapshot, true
}

func BuildDiffFrom(term termemu.Terminal, region termemu.Region, reason termemu.ChangeReason) (Diff, bool) {
	if term == nil {
		return Diff{}, false
	}
	var diff Diff
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
		diff = Diff{
			Region: Region{X: clamped.X, Y: clamped.Y, X2: clamped.X2, Y2: clamped.Y2},
			Lines:  lines,
			Reason: ChangeReasonString(reason),
		}
	})
	if diff.Lines == nil {
		return Diff{}, false
	}
	return diff, true
}

func ChangeReasonString(reason termemu.ChangeReason) string {
	switch reason {
	case termemu.CRText:
		return "text"
	case termemu.CRClear:
		return "clear"
	case termemu.CRScroll:
		return "scroll"
	case termemu.CRScreenSwitch:
		return "screen_switch"
	case termemu.CRRedraw:
		return "redraw"
	default:
		return "unknown"
	}
}
