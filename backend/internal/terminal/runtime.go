package terminal

import "github.com/ricochet1k/termemu"

const EventBufferSize = 256

type EventKind int

const (
	EventBell EventKind = iota
	EventRegionChanged
	EventScrollLines
	EventCursorMoved
	EventStyleChanged
	EventViewFlagChanged
	EventViewIntChanged
	EventViewStringChanged
)

type Event struct {
	Kind       EventKind
	Region     termemu.Region
	Reason     termemu.ChangeReason
	X          int
	Y          int
	ScrollY    int
	Style      termemu.Style
	ViewFlag   termemu.ViewFlag
	ViewInt    termemu.ViewInt
	ViewString termemu.ViewString
	BoolValue  bool
	IntValue   int
	TextValue  string
}

type Frontend struct {
	events chan<- Event
	done   <-chan struct{}
}

func NewFrontend(events chan<- Event, done <-chan struct{}) *Frontend {
	return &Frontend{events: events, done: done}
}

func (f *Frontend) Bell() {
	f.emit(Event{Kind: EventBell})
}

func (f *Frontend) RegionChanged(r termemu.Region, reason termemu.ChangeReason) {
	f.emit(Event{Kind: EventRegionChanged, Region: r, Reason: reason})
}

func (f *Frontend) ScrollLines(y int) {
	f.emit(Event{Kind: EventScrollLines, ScrollY: y})
}

func (f *Frontend) CursorMoved(x, y int) {
	f.emit(Event{Kind: EventCursorMoved, X: x, Y: y})
}

func (f *Frontend) StyleChanged(s termemu.Style) {
	f.emit(Event{Kind: EventStyleChanged, Style: s})
}

func (f *Frontend) ViewFlagChanged(flag termemu.ViewFlag, value bool) {
	f.emit(Event{Kind: EventViewFlagChanged, ViewFlag: flag, BoolValue: value})
}

func (f *Frontend) ViewIntChanged(flag termemu.ViewInt, value int) {
	f.emit(Event{Kind: EventViewIntChanged, ViewInt: flag, IntValue: value})
}

func (f *Frontend) ViewStringChanged(flag termemu.ViewString, value string) {
	f.emit(Event{Kind: EventViewStringChanged, ViewString: flag, TextValue: value})
}

func (f *Frontend) emit(event Event) {
	if f == nil || f.events == nil {
		return
	}
	if f.done != nil {
		select {
		case <-f.done:
			return
		default:
		}
	}

	select {
	case f.events <- event:
	default:
	}
}
