//go:build !darwin

package storage

import (
	"io/fs"
	"time"
)

// fileBirthTime returns the best available creation time.
// On non-Darwin platforms birth time is not reliably accessible,
// so ModTime is returned as a fallback.
func fileBirthTime(info fs.FileInfo) time.Time {
	return info.ModTime()
}
