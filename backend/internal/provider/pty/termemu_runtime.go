package pty

import (
	"encoding/binary"
	"io"
	"os"
	"sync"
	"time"
)

type syncCloser interface {
	io.Closer
	Sync() error
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
