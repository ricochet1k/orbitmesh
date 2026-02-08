package pty

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"time"
)

const ptyLogMaxFrameSize = 16 * 1024 * 1024

var ErrPTYLogCorrupt = errors.New("pty log frame corrupt")

type PTYLogFrame struct {
	Offset    int64
	Direction byte
	Timestamp time.Time
	Payload   []byte
}

type PTYLogDiagnostics struct {
	Frames        int
	Bytes         int64
	PartialFrame  bool
	PartialOffset int64
	CorruptFrames int
	CorruptOffset int64
}

func ReplayPTYLog(path string, startOffset int64, handler func(frame PTYLogFrame) error) (int64, PTYLogDiagnostics, error) {
	var diag PTYLogDiagnostics
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return startOffset, diag, nil
		}
		return startOffset, diag, err
	}
	defer file.Close()

	if startOffset < 0 {
		startOffset = 0
	}
	if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
		return startOffset, diag, err
	}

	reader := bufio.NewReader(file)
	offset := startOffset
	lastGoodOffset := startOffset

	for {
		length, n, err := readUvarint(reader)
		if err != nil {
			if errors.Is(err, io.EOF) && n == 0 {
				break
			}
			if errors.Is(err, ErrPTYLogCorrupt) {
				diag.CorruptFrames++
				diag.CorruptOffset = lastGoodOffset
				return lastGoodOffset, diag, ErrPTYLogCorrupt
			}
			diag.PartialFrame = true
			diag.PartialOffset = offset
			return lastGoodOffset, diag, nil
		}
		offset += int64(n)
		if length == 0 || length > ptyLogMaxFrameSize {
			diag.CorruptFrames++
			diag.CorruptOffset = lastGoodOffset
			return lastGoodOffset, diag, ErrPTYLogCorrupt
		}

		header := make([]byte, 1+8)
		if _, err := io.ReadFull(reader, header); err != nil {
			diag.PartialFrame = true
			diag.PartialOffset = offset
			return lastGoodOffset, diag, nil
		}
		offset += int64(len(header))

		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			diag.PartialFrame = true
			diag.PartialOffset = offset
			return lastGoodOffset, diag, nil
		}
		offset += int64(length)

		frame := PTYLogFrame{
			Offset:    lastGoodOffset,
			Direction: header[0],
			Timestamp: time.Unix(0, int64(binary.LittleEndian.Uint64(header[1:]))),
			Payload:   payload,
		}
		if handler != nil {
			if err := handler(frame); err != nil {
				return offset, diag, err
			}
		}
		lastGoodOffset = offset
		diag.Frames++
		diag.Bytes += int64(len(payload))
	}

	return lastGoodOffset, diag, nil
}

func readUvarint(r io.ByteReader) (uint64, int, error) {
	var x uint64
	var s uint
	for i := 0; ; i++ {
		b, err := r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && i == 0 {
				return 0, 0, io.EOF
			}
			return 0, i, err
		}
		if b < 0x80 {
			if i > 9 || (i == 9 && b > 1) {
				return 0, i + 1, ErrPTYLogCorrupt
			}
			return x | uint64(b)<<s, i + 1, nil
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
}
