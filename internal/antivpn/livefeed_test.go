package antivpn

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveFeedWriterAppendsLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "live", "server-output.log")

	feed, err := newLiveFeedWriter(path, 0, 3, false, nil)
	if err != nil {
		t.Fatalf("newLiveFeedWriter: %v", err)
	}
	t.Cleanup(func() { _ = feed.Close() })

	if err := feed.WriteLine("ClientConnect: 0 [203.0.113.1] (GUID) \"Akion\""); err != nil {
		t.Fatalf("WriteLine 1: %v", err)
	}
	if err := feed.WriteLine("ChangeTeam: 0 [203.0.113.1] (GUID) \"Akion\" SPECTATOR -> RED"); err != nil {
		t.Fatalf("WriteLine 2: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read live file: %v", err)
	}

	got := string(contents)
	if !strings.Contains(got, "ClientConnect: 0 [203.0.113.1]") {
		t.Fatalf("expected first line to be mirrored, got %q", got)
	}
	if !strings.Contains(got, "ChangeTeam: 0 [203.0.113.1]") {
		t.Fatalf("expected second line to be mirrored, got %q", got)
	}
	if strings.Count(got, "\n") != 2 {
		t.Fatalf("expected exactly two newline-terminated lines, got %d in %q", strings.Count(got, "\n"), got)
	}
}

func TestLiveFeedWriterRotatesAtMaxBytesAndGzipsArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server-output.log")

	feed, err := newLiveFeedWriter(path, 32, 5, false, nil)
	if err != nil {
		t.Fatalf("newLiveFeedWriter: %v", err)
	}
	t.Cleanup(func() { _ = feed.Close() })

	// First long-ish line crosses the 32 byte threshold; the next write triggers
	// rotation before appending so the live file holds only post-rotation data.
	if err := feed.WriteLine("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); err != nil {
		t.Fatalf("WriteLine first: %v", err)
	}
	if err := feed.WriteLine("post-rotation-line"); err != nil {
		t.Fatalf("WriteLine second: %v", err)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rotated current file: %v", err)
	}
	if got := string(current); !strings.Contains(got, "post-rotation-line") {
		t.Fatalf("expected fresh file to contain post-rotation line, got %q", got)
	}
	if strings.Contains(string(current), "AAAA") {
		t.Fatalf("rotated current file unexpectedly contains pre-rotation payload: %q", string(current))
	}

	archive := findSingleGzArchive(t, dir, filepath.Base(path))
	contents := readGzip(t, archive)
	if !strings.Contains(contents, "AAAA") {
		t.Fatalf("rotation archive missing pre-rotation payload: %q", contents)
	}
}

func TestLiveFeedWriterRotateOnOpenArchivesPreviousRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server-output.log")

	if err := os.WriteFile(path, []byte("previous-run-payload\n"), 0o644); err != nil {
		t.Fatalf("seed previous file: %v", err)
	}

	feed, err := newLiveFeedWriter(path, 0, 3, true, nil)
	if err != nil {
		t.Fatalf("newLiveFeedWriter rotate-on-open: %v", err)
	}
	t.Cleanup(func() { _ = feed.Close() })

	if err := feed.WriteLine("fresh-run-line"); err != nil {
		t.Fatalf("WriteLine post-archive: %v", err)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fresh file: %v", err)
	}
	if string(current) != "fresh-run-line\n" {
		t.Fatalf("expected fresh file to contain only post-startup data, got %q", string(current))
	}

	archive := findSingleGzArchive(t, dir, filepath.Base(path))
	if !strings.Contains(readGzip(t, archive), "previous-run-payload") {
		t.Fatalf("expected previous-run payload in archive %s", archive)
	}
}

func TestLiveFeedWriterEmptyPathDisabled(t *testing.T) {
	feed, err := newLiveFeedWriter("", 0, 0, false, nil)
	if err != nil {
		t.Fatalf("newLiveFeedWriter empty path: %v", err)
	}
	if feed != nil {
		t.Fatalf("expected nil feed when path is empty, got %#v", feed)
	}

	// WriteLine and Close on a nil receiver must be a no-op.
	if err := feed.WriteLine("anything"); err != nil {
		t.Fatalf("WriteLine on nil feed should be no-op, got %v", err)
	}
	if err := feed.Close(); err != nil {
		t.Fatalf("Close on nil feed should be no-op, got %v", err)
	}
}

func findSingleGzArchive(t *testing.T, dir, baseName string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read archive dir: %v", err)
	}
	prefix := baseName + "."
	matches := make([]string, 0, 1)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".gz") {
			matches = append(matches, filepath.Join(dir, name))
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one gzipped archive for %s, got %v", baseName, matches)
	}
	return matches[0]
}

func readGzip(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	defer gz.Close()
	data, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	return string(data)
}
