package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"sync"
)

type lineWriter struct {
	mu sync.Mutex
	w  io.Writer
	r  *transcriptRecorder
}

func newLineWriter(w io.Writer, recorder *transcriptRecorder) *lineWriter {
	return &lineWriter{w: w, r: recorder}
}

func (lw *lineWriter) WriteLine(line []byte) error {
	trimmed := trimLine(line)
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if len(trimmed) == 0 {
		return nil
	}
	if _, err := lw.w.Write(append(append([]byte{}, trimmed...), '\n')); err != nil {
		return err
	}
	lw.r.Record(dirProviderToOrbitMesh, trimmed)
	return nil
}

func readLines(ctxDone <-chan struct{}, in io.Reader, onLine func([]byte) error) error {
	scanner := bufio.NewScanner(in)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctxDone:
			return nil
		default:
		}
		line := append([]byte(nil), trimLine(scanner.Bytes())...)
		if len(line) == 0 {
			continue
		}
		if err := onLine(line); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func trimLine(b []byte) []byte {
	return bytes.TrimRight(b, "\r\n")
}

func marshalJSON(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return trimLine(b), nil
}
