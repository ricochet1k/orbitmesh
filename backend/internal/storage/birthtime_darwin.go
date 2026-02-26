//go:build darwin

package storage

import (
	"io/fs"
	"syscall"
	"time"
)

// fileBirthTime returns the file's birth time on Darwin using Stat_t.Birthtimespec.
func fileBirthTime(info fs.FileInfo) time.Time {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec)
	}
	return info.ModTime()
}
