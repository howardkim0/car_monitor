// Package applog is a small, best-effort app/error log — separate from
// the reading log in internal/storage, which needs day-based rotation for
// time-based analysis (see DESIGN.md section 6). App logs don't need
// that; they just need to not grow unbounded, so this uses a single
// size-capped file instead, with one kept prior file on rollover.
//
// "Best-effort" is deliberate throughout: a logging failure must never
// crash or block the app it's attached to, so writes and rotation here
// are attempted and silently degrade (a single write may be lost)
// rather than surfaced as errors a caller has to handle. Degradation is
// scoped to the failing operation, not permanent: e.g. a failed rotation
// reopens the same file rather than leaving logging dead until restart.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MaxSizeBytes is the size cap before the current log file rotates
// aside. Named per DESIGN.md section 6's requirement.
const MaxSizeBytes = 10 * 1024 * 1024

const fileName = "app.log"
const priorSuffix = ".1"

// Logger appends timestamped lines to a size-capped file.
type Logger struct {
	mu      sync.Mutex
	dir     string
	file    *os.File
	maxSize int64
}

// Open opens (creating if necessary) the app log in dir.
func Open(dir string) (*Logger, error) {
	return openWithMaxSize(dir, MaxSizeBytes)
}

// openWithMaxSize is Open with an injectable cap, so tests can exercise
// rotation without writing MaxSizeBytes of real data.
func openWithMaxSize(dir string, maxSize int64) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create app log dir %q: %w", dir, err)
	}

	l := &Logger{dir: dir, maxSize: maxSize}
	if err := l.openCurrentFile(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Logger) currentPath() string { return filepath.Join(l.dir, fileName) }
func (l *Logger) priorPath() string   { return filepath.Join(l.dir, fileName+priorSuffix) }

func (l *Logger) openCurrentFile() error {
	f, err := os.OpenFile(l.currentPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open app log %q: %w", l.currentPath(), err)
	}
	l.file = f
	return nil
}

// Errorf writes a single ERROR-level line.
func (l *Logger) Errorf(format string, args ...any) { l.writef("ERROR", format, args...) }

// Debugf writes a single DEBUG-level line.
func (l *Logger) Debugf(format string, args ...any) { l.writef("DEBUG", format, args...) }

func (l *Logger) writef(level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.rotateIfNeeded()

	line := fmt.Sprintf("%s %s %s\n", time.Now().UTC().Format(time.RFC3339Nano), level, fmt.Sprintf(format, args...))
	_, _ = l.file.WriteString(line)
	_ = l.file.Sync()
}

// rotateIfNeeded renames the current file aside (keeping exactly one
// prior file — any older one is discarded) and starts a fresh one, once
// the current file has reached maxSize.
func (l *Logger) rotateIfNeeded() {
	info, err := l.file.Stat()
	if err != nil || info.Size() < l.maxSize {
		return
	}

	_ = l.file.Close() // best-effort; this handle is being replaced regardless
	_ = os.Remove(l.priorPath())
	if err := os.Rename(l.currentPath(), l.priorPath()); err != nil {
		// Rotation failed, but the current file was never actually
		// renamed away — reopen the same path so logging keeps working
		// (just without having rotated this time) instead of leaving
		// l.file pointing at the handle just closed above, which would
		// silently break every future write for the rest of the process.
		_ = l.openCurrentFile()
		return
	}
	_ = l.openCurrentFile()
}

// Close closes the underlying file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}
