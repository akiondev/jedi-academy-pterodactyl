package antivpn

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// rotatingFile is a thread-safe io.WriteCloser that rotates the underlying
// file when its size exceeds maxBytes. On rotation, the current file is
// renamed to "<path>.<timestamp>" and gzip-compressed; older gzipped archives
// beyond keepCount are pruned.
//
// Design notes:
//   - Rotation happens synchronously inside Write so the caller observes the
//     boundary deterministically. Compression also runs synchronously but the
//     gzipped archive is small relative to the rotation threshold so this is
//     bounded and rare.
//   - Errors during rotation never cause Write to fail; the writer logs via
//     onError and continues writing to a freshly opened file. If the writer
//     becomes permanently unusable (e.g. disk full), Write disables the
//     writer and returns the original error to the caller exactly once.
//   - "rotate-on-open" optionally rotates any pre-existing non-empty file at
//     construction time. This gives every supervisor restart its own archived
//     audit / live-mirror history without requiring an external logrotate.
type rotatingFile struct {
	mu        sync.Mutex
	path      string
	maxBytes  int64
	keepCount int
	file      *os.File
	size      int64
	disabled  bool
	onError   func(error)
}

// newRotatingFile opens (or creates) the file at path. If rotateOnOpen is
// true and the existing file is non-empty, it is rotated and gzipped before a
// fresh empty file is opened. keepCount is the maximum number of gzipped
// archive files to retain (older archives are pruned). maxBytes is the soft
// rotation threshold; zero disables size-based rotation. onError is invoked
// for non-fatal rotation/compression errors and may be nil.
func newRotatingFile(path string, maxBytes int64, keepCount int, rotateOnOpen bool, onError func(error)) (*rotatingFile, error) {
	if path == "" {
		return nil, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create rotating file directory: %w", err)
	}

	if keepCount < 0 {
		keepCount = 0
	}
	if maxBytes < 0 {
		maxBytes = 0
	}
	if onError == nil {
		onError = func(error) {}
	}

	rf := &rotatingFile{
		path:      path,
		maxBytes:  maxBytes,
		keepCount: keepCount,
		onError:   onError,
	}

	if rotateOnOpen {
		if stat, err := os.Stat(path); err == nil && stat.Size() > 0 {
			if rotErr := rf.archiveCurrent(); rotErr != nil {
				onError(fmt.Errorf("rotate %s on open: %w", path, rotErr))
			}
		}
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open rotating file: %w", err)
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat rotating file: %w", err)
	}
	rf.file = file
	rf.size = stat.Size()
	return rf, nil
}

// Write appends p to the underlying file, rotating first if the new size
// would exceed maxBytes. It is safe for concurrent use.
func (rf *rotatingFile) Write(p []byte) (int, error) {
	if rf == nil {
		return len(p), nil
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.disabled || rf.file == nil {
		return 0, fs.ErrClosed
	}

	if rf.maxBytes > 0 && rf.size+int64(len(p)) > rf.maxBytes && rf.size > 0 {
		if err := rf.rotateLocked(); err != nil {
			rf.onError(fmt.Errorf("rotate %s: %w", rf.path, err))
			// Continue writing to whatever file we managed to reopen; if
			// rotateLocked could not reopen the file, disabled is set.
		}
	}

	if rf.disabled || rf.file == nil {
		return 0, fs.ErrClosed
	}

	n, err := rf.file.Write(p)
	rf.size += int64(n)
	if err != nil {
		rf.disabled = true
		return n, err
	}
	return n, nil
}

// Close flushes and releases the underlying file handle. It is safe to call
// multiple times.
func (rf *rotatingFile) Close() error {
	if rf == nil {
		return nil
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.file == nil {
		return nil
	}
	err := rf.file.Close()
	rf.file = nil
	return err
}

// rotateLocked must be called with rf.mu held. It closes the current file,
// archives + gzips it, prunes old archives, then reopens a fresh file.
func (rf *rotatingFile) rotateLocked() error {
	if rf.file != nil {
		_ = rf.file.Close()
		rf.file = nil
	}

	if err := rf.archiveCurrent(); err != nil {
		// Best-effort: try to reopen the file even if archiving failed so
		// the writer keeps functioning. Do not return early.
		rf.onError(fmt.Errorf("archive %s: %w", rf.path, err))
	}

	file, err := os.OpenFile(rf.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		rf.disabled = true
		return fmt.Errorf("reopen %s after rotation: %w", rf.path, err)
	}
	rf.file = file
	rf.size = 0
	return nil
}

// archiveCurrent renames the active file to a uniquely-timestamped archive
// path and gzips the archive. On success it also prunes old archives down to
// keepCount. No-op if the file does not exist.
func (rf *rotatingFile) archiveCurrent() error {
	if _, err := os.Stat(rf.path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	archivePath := rf.uniqueArchivePath(time.Now().UTC())
	if err := os.Rename(rf.path, archivePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	gzPath := archivePath + ".gz"
	if err := gzipFileAtomic(archivePath, gzPath); err != nil {
		// Leave the uncompressed archive in place so the data is not lost.
		rf.onError(fmt.Errorf("gzip %s: %w", archivePath, err))
	}

	rf.pruneOldArchives()
	return nil
}

// uniqueArchivePath returns a filename of the form "<path>.<utc-timestamp>"
// that does not collide with an existing file (compressed or otherwise).
func (rf *rotatingFile) uniqueArchivePath(now time.Time) string {
	base := fmt.Sprintf("%s.%s", rf.path, now.Format("20060102T150405Z"))
	candidate := base
	for suffix := 1; suffix < 1000; suffix++ {
		_, err1 := os.Stat(candidate)
		_, err2 := os.Stat(candidate + ".gz")
		if errors.Is(err1, fs.ErrNotExist) && errors.Is(err2, fs.ErrNotExist) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
	return candidate
}

// pruneOldArchives removes the oldest gzipped archives beyond keepCount.
// Archive filenames are matched by prefix "<basename>." and suffix ".gz".
// Sort order is by file modification time, oldest first.
func (rf *rotatingFile) pruneOldArchives() {
	if rf.keepCount <= 0 {
		// keepCount == 0 means "do not retain any archives"; remove every
		// gzipped sibling.
	}

	dir := filepath.Dir(rf.path)
	base := filepath.Base(rf.path)
	prefix := base + "."

	entries, err := os.ReadDir(dir)
	if err != nil {
		rf.onError(fmt.Errorf("list archives in %s: %w", dir, err))
		return
	}

	type archive struct {
		path    string
		modTime time.Time
	}
	archives := make([]archive, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".gz") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		archives = append(archives, archive{
			path:    filepath.Join(dir, name),
			modTime: info.ModTime(),
		})
	}

	if len(archives) <= rf.keepCount {
		return
	}

	sort.Slice(archives, func(i, j int) bool {
		return archives[i].modTime.Before(archives[j].modTime)
	})

	excess := len(archives) - rf.keepCount
	for i := 0; i < excess; i++ {
		if err := os.Remove(archives[i].path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			rf.onError(fmt.Errorf("prune archive %s: %w", archives[i].path, err))
		}
	}
}

// gzipFileAtomic streams source through gzip into destPath using a ".tmp"
// staging file so an aborted compression cannot leave a half-written archive
// in place. On success the source file is removed.
func gzipFileAtomic(source, destPath string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	tmpPath := destPath + ".tmp"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, in); err != nil {
		gz.Close()
		out.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := gz.Close(); err != nil {
		out.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Remove(source)
}
