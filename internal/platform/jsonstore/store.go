// Package jsonstore persists JSON payloads atomically. Rapid saves are
// coalesced over a debounce interval and every disk write is serialized by a
// single writer lock, so concurrent callers never interleave temp files or
// rename over each other.
package jsonstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fileforge-desktop/internal/platform/fsx"
)

const (
	// DefaultDebounce is the default coalescing window for Store.Save.
	DefaultDebounce = 500 * time.Millisecond

	defaultFileMode = 0o644
	defaultDirMode  = 0o755
)

var (
	// ErrClosed is returned by Save and SaveNow after Close.
	ErrClosed = errors.New("jsonstore: store is closed")

	errNoPath = errors.New("jsonstore: store path is empty")
)

// Options configures a Store.
type Options struct {
	// Debounce is the coalescing window for Save. Zero uses DefaultDebounce.
	Debounce time.Duration
	// FileMode is the persisted file permission. Zero uses 0o644.
	FileMode os.FileMode
	// DirMode is the permission used when creating the parent directory. Zero
	// uses 0o755.
	DirMode os.FileMode
	// TempPattern is the temporary file pattern passed to os.CreateTemp. Empty
	// derives one from the destination name.
	TempPattern string
	// OnError receives asynchronous write failures from debounced saves.
	// Synchronous calls (SaveNow, Flush, Close) return their errors directly.
	OnError func(error)
}

// Store is an atomic, debounced JSON file writer.
type Store struct {
	path string
	opts Options

	mu         sync.Mutex
	pending    []byte
	hasPending bool
	timer      *time.Timer
	generation uint64
	closed     bool

	writeMu sync.Mutex
}

// New creates a store for path. No file is read or written until the first
// save.
func New(path string, opts Options) *Store {
	path = strings.TrimSpace(path)

	return &Store{
		path: path,
		opts: normalizeOptions(path, opts),
	}
}

func normalizeOptions(path string, opts Options) Options {
	if opts.Debounce <= 0 {
		opts.Debounce = DefaultDebounce
	}
	if opts.FileMode == 0 {
		opts.FileMode = defaultFileMode
	}
	if opts.DirMode == 0 {
		opts.DirMode = defaultDirMode
	}
	if opts.TempPattern == "" {
		base := filepath.Base(path)
		if base == "" || base == "." || base == string(filepath.Separator) {
			opts.TempPattern = ".fileforge-*.tmp"
		} else {
			opts.TempPattern = ".fileforge-" + base + "-*.tmp"
		}
	}

	return opts
}

// Load reads path and unmarshals it into target. A missing file is reported
// with an error that satisfies errors.Is(err, os.ErrNotExist).
func Load(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("jsonstore: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("jsonstore: decode %s: %w", path, err)
	}

	return nil
}

// Save queues payload and schedules a write after the debounce interval. It
// copies payload, so callers may reuse their buffer immediately. Errors from
// the asynchronous write are reported through Options.OnError.
func (s *Store) Save(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrClosed
	}
	if s.path == "" {
		return errNoPath
	}

	s.pending = append([]byte(nil), payload...)
	s.hasPending = true
	s.generation++
	generation := s.generation
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.opts.Debounce, func() {
		s.writePending(generation)
	})

	return nil
}

// SaveNow cancels any pending debounced write and writes payload immediately.
func (s *Store) SaveNow(payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	s.generation++
	if s.timer != nil {
		s.timer.Stop()
	}
	s.pending = nil
	s.hasPending = false
	s.mu.Unlock()

	return s.writeLocked(payload)
}

// Flush waits for any in-flight write and writes the pending payload. It is a
// no-op when nothing is pending and the store is already closed.
func (s *Store) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.generation++
	if s.timer != nil {
		s.timer.Stop()
	}
	payload := s.pending
	hasPending := s.hasPending
	s.pending = nil
	s.hasPending = false
	s.mu.Unlock()

	if !hasPending {
		return nil
	}

	return s.writeLocked(payload)
}

// Close stops the debounce timer and writes the pending payload. It is
// idempotent: closing an already closed store returns nil. If ctx is already
// cancelled the store stays open so the caller can retry.
func (s *Store) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.generation++
	if s.timer != nil {
		s.timer.Stop()
	}
	payload := s.pending
	hasPending := s.hasPending
	s.pending = nil
	s.hasPending = false
	s.mu.Unlock()

	if !hasPending {
		return nil
	}

	return s.writeLocked(payload)
}

func (s *Store) writePending(generation uint64) {
	s.writeMu.Lock()
	err := s.writePendingLocked(generation)
	s.writeMu.Unlock()

	if err != nil {
		s.reportError(err)
	}
}

// writePendingLocked consumes the pending payload for generation and writes
// it. The caller holds writeMu.
func (s *Store) writePendingLocked(generation uint64) error {
	s.mu.Lock()
	if s.closed || generation != s.generation || !s.hasPending {
		s.mu.Unlock()
		return nil
	}
	payload := s.pending
	s.pending = nil
	s.hasPending = false
	s.mu.Unlock()

	return s.writeLocked(payload)
}

// writeLocked writes payload atomically. The caller holds writeMu, so disk I/O
// never blocks Save, and Save never blocks disk I/O.
func (s *Store) writeLocked(payload []byte) error {
	if s.path == "" {
		return errNoPath
	}
	if err := fsx.EnsureDir(filepath.Dir(s.path), s.opts.DirMode); err != nil {
		return err
	}

	return fsx.WriteAtomicWithPattern(s.path, payload, s.opts.FileMode, s.opts.TempPattern)
}

func (s *Store) reportError(err error) {
	if err == nil || s.opts.OnError == nil {
		return
	}
	s.opts.OnError(err)
}
