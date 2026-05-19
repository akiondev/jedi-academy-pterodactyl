package antivpn

import (
	"io/fs"
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
//     mirror writes until the rotating writer recovers (e.g. on the next
//     rotation cycle).
//   - The underlying file is rotated by size with gzip compression and a
//     bounded archive history; on supervisor startup the previous run's file
//     is also archived so each session has its own retrievable history.
type liveFeedWriter struct {
	rf       *rotatingFile
	disabled bool
}

func newLiveFeedWriter(path string, maxBytes int64, keepArchives int, rotateOnOpen bool, onError func(error)) (*liveFeedWriter, error) {
	rf, err := newRotatingFile(path, maxBytes, keepArchives, rotateOnOpen, onError)
	if err != nil {
		return nil, err
	}
	if rf == nil {
		return nil, nil
	}
	return &liveFeedWriter{rf: rf}, nil
}

// WriteLine appends a single line (with a trailing newline) to the live mirror
// file. It returns any underlying error so callers may log it once, but it
// never panics and is safe to call concurrently from the stdout/stderr
// scanners.
func (w *liveFeedWriter) WriteLine(line string) error {
	if w == nil || w.rf == nil {
		return nil
	}
	if w.disabled {
		return nil
	}

	payload := []byte(line)
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		payload = append(payload, '\n')
	}

	if _, err := w.rf.Write(payload); err != nil {
		// Permanent failure: stop trying to mirror so we cannot wedge the
		// supervisor's hot path. The error surfaces to the caller exactly
		// once via the supervisor's liveFeedErrOnce gate.
		w.disabled = true
		if err == fs.ErrClosed {
			return nil
		}
		return err
	}
	return nil
}

// Close flushes and releases the underlying file handle. It is safe to call
// multiple times.
func (w *liveFeedWriter) Close() error {
	if w == nil || w.rf == nil {
		return nil
	}
	return w.rf.Close()
}
