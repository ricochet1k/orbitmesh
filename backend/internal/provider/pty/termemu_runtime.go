package pty

import (
	"encoding/binary"
	"io"
	"os"
	"sync"
	"time"

	"github.com/ricochet1k/termemu"
)

type syncCloser interface {
	io.Closer
	Sync() error
}

const terminalEventBufferSize = 256

type terminalEventKind int

const (
	terminalEventBell terminalEventKind = iota
	terminalEventRegionChanged
	terminalEventScrollLines
	terminalEventCursorMoved
	terminalEventStyleChanged
	terminalEventViewFlagChanged
	terminalEventViewIntChanged
	terminalEventViewStringChanged
)

type terminalEvent struct {
	kind       terminalEventKind
	region     termemu.Region
	reason     termemu.ChangeReason
	x          int
	y          int
	scrollY    int
	style      termemu.Style
	viewFlag   termemu.ViewFlag
	viewInt    termemu.ViewInt
	viewString termemu.ViewString
	boolValue  bool
	intValue   int
	textValue  string
}

type terminalFrontend struct {
	events chan<- terminalEvent
	done   <-chan struct{}
}

func newTerminalFrontend(events chan<- terminalEvent, done <-chan struct{}) *terminalFrontend {
	return &terminalFrontend{events: events, done: done}
}

func (f *terminalFrontend) Bell() {
	f.emit(terminalEvent{kind: terminalEventBell})
}

func (f *terminalFrontend) RegionChanged(r termemu.Region, reason termemu.ChangeReason) {
	f.emit(terminalEvent{kind: terminalEventRegionChanged, region: r, reason: reason})
}

func (f *terminalFrontend) ScrollLines(y int) {
	f.emit(terminalEvent{kind: terminalEventScrollLines, scrollY: y})
}

func (f *terminalFrontend) CursorMoved(x, y int) {
	f.emit(terminalEvent{kind: terminalEventCursorMoved, x: x, y: y})
}

func (f *terminalFrontend) StyleChanged(s termemu.Style) {
	f.emit(terminalEvent{kind: terminalEventStyleChanged, style: s})
}

func (f *terminalFrontend) ViewFlagChanged(flag termemu.ViewFlag, value bool) {
	f.emit(terminalEvent{kind: terminalEventViewFlagChanged, viewFlag: flag, boolValue: value})
}

func (f *terminalFrontend) ViewIntChanged(flag termemu.ViewInt, value int) {
	f.emit(terminalEvent{kind: terminalEventViewIntChanged, viewInt: flag, intValue: value})
}

func (f *terminalFrontend) ViewStringChanged(flag termemu.ViewString, value string) {
	f.emit(terminalEvent{kind: terminalEventViewStringChanged, viewString: flag, textValue: value})
}

func (f *terminalFrontend) emit(event terminalEvent) {
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

const ptyLogDirectionOut = byte(0)

type ptyLogWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func newPTYLogWriter(w io.Writer) *ptyLogWriter {
	return &ptyLogWriter{w: w}
}

func (l *ptyLogWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if l == nil || l.w == nil {
		return 0, io.ErrClosedPipe
	}

	var header [binary.MaxVarintLen64]byte
	encoded := binary.PutUvarint(header[:], uint64(len(p)))

	var tsBuf [8]byte
	binary.LittleEndian.PutUint64(tsBuf[:], uint64(time.Now().UnixNano()))

	frame := make([]byte, 0, encoded+1+len(tsBuf)+len(p))
	frame = append(frame, header[:encoded]...)
	frame = append(frame, ptyLogDirectionOut)
	frame = append(frame, tsBuf[:]...)
	frame = append(frame, p...)

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := l.w.Write(frame); err != nil {
		return 0, err
	}
	return len(p), nil
}

func openPTYLog(sessionID string) (*os.File, error) {
	return openSessionAppendFile(sessionID, ptyRawLogFile)
}
