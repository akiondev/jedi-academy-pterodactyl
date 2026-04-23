package antivpn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveFeedWriterAppendsLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "live", "server-output.log")

	feed, err := newLiveFeedWriter(path, 0)
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

func TestLiveFeedWriterRotatesAtMaxBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server-output.log")

	feed, err := newLiveFeedWriter(path, 32)
	if err != nil {
		t.Fatalf("newLiveFeedWriter: %v", err)
	}
	t.Cleanup(func() { _ = feed.Close() })

	// First long-ish line crosses the 32 byte threshold and triggers rotation.
	if err := feed.WriteLine("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); err != nil {
		t.Fatalf("WriteLine first: %v", err)
	}
	if err := feed.WriteLine("post-rotation-line"); err != nil {
		t.Fatalf("WriteLine second: %v", err)
	}

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotation archive %s.1 to exist: %v", path, err)
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

	archive, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("read rotation archive: %v", err)
	}
	if !strings.Contains(string(archive), "AAAA") {
		t.Fatalf("rotation archive missing pre-rotation payload: %q", string(archive))
	}
}

func TestLiveFeedWriterEmptyPathDisabled(t *testing.T) {
	feed, err := newLiveFeedWriter("", 0)
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
