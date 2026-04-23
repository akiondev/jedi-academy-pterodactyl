package antivpn

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// liveFeedWriter mirrors live server stdout/stderr lines into a runtime-managed
// "live output" file that addons can consume with `tail -F`.
//
// Design notes:
//   - A regular append-only file is used (not a FIFO/socket) so that multiple
//     addons can read the stream concurrently without blocking, without
//     consumer-disconnect handling, and without backpressure on the server
//     process.
//   - Writes never block the supervisor's stdout/stderr scanner: if the file
//     becomes unwritable we log the error once and silently drop subsequent
//     mirror writes until the file is reopened on the next rotation attempt.
//   - Size-based rotation keeps a single ".1" archive so the file cannot grow
//     unbounded, but the rotation is best-effort and never fatal.
type liveFeedWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	file     *os.File
	size     int64
	disabled bool
}

func newLiveFeedWriter(path string, maxBytes int64) (*liveFeedWriter, error) {
	if path == "" {
		return nil, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create live output directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open live output file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat live output file: %w", err)
	}

	return &liveFeedWriter{
		path:     path,
		maxBytes: maxBytes,
		file:     file,
		size:     stat.Size(),
	}, nil
}

// WriteLine appends a single line (with a trailing newline) to the live mirror
// file. It returns any underlying error so callers may log it once, but it
// never panics and is safe to call concurrently from the stdout/stderr
// scanners.
func (w *liveFeedWriter) WriteLine(line string) error {
	if w == nil {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.disabled || w.file == nil {
		return nil
	}

	payload := []byte(line)
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		payload = append(payload, '\n')
	}

	n, err := w.file.Write(payload)
	w.size += int64(n)
	if err != nil {
		// Permanent failure: stop trying to mirror so we cannot wedge the
		// supervisor's hot path. The file will be reopened on the next Close
		// + reopen cycle (e.g. on supervisor restart).
		w.disabled = true
		return err
	}

	if w.maxBytes > 0 && w.size >= w.maxBytes {
		if rotateErr := w.rotateLocked(); rotateErr != nil {
			return rotateErr
		}
	}

	return nil
}

func (w *liveFeedWriter) rotateLocked() error {
	if w.file == nil {
		return nil
	}

	if err := w.file.Close(); err != nil {
		// Continue with rotation; we already lost the previous handle.
		_ = err
	}
	w.file = nil

	archive := w.path + ".1"
	if err := os.Remove(archive); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Non-fatal: continue and try to rename.
		_ = err
	}
	if err := os.Rename(w.path, archive); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Best-effort: if we cannot rotate we still try to reopen the
		// existing file so the stream keeps flowing.
		_ = err
	}

	file, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		w.disabled = true
		return fmt.Errorf("reopen live output file after rotation: %w", err)
	}
	w.file = file
	w.size = 0
	return nil
}

// Close flushes and releases the underlying file handle. It is safe to call
// multiple times.
func (w *liveFeedWriter) Close() error {
	if w == nil {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
