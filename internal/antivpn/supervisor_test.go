package antivpn

import (
	"io"
	"log/slog"
	"net/netip"
	"path/filepath"
	"testing"
	"time"
)

func TestParseClientUserinfoChangedFromTimestampedLine(t *testing.T) {
	line := `2026-04-16 17:28:09 ClientUserinfoChanged: 3 n\Player\t\0\ip\198.51.100.25:29070\cl_guid\abc123`

	slot, addr, ok := parseClientUserinfoChanged(line)
	if !ok {
		t.Fatalf("expected parser to match ClientUserinfoChanged line")
	}
	if slot != "3" {
		t.Fatalf("expected slot 3, got %q", slot)
	}
	if addr != netip.MustParseAddr("198.51.100.25") {
		t.Fatalf("expected parsed IP 198.51.100.25, got %s", addr)
	}
}

func TestParseServerIPFieldSupportsPortSuffix(t *testing.T) {
	addr, err := parseServerIPField("203.0.113.44:29070")
	if err != nil {
		t.Fatalf("parseServerIPField returned error: %v", err)
	}
	if addr != netip.MustParseAddr("203.0.113.44") {
		t.Fatalf("unexpected parsed address: %s", addr)
	}
}

func TestCachePersistsOnClose(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cache, err := NewCache(cachePath, 25*time.Millisecond, logger)
	if err != nil {
		t.Fatalf("NewCache returned error: %v", err)
	}

	cache.Set(Decision{
		IP:        "198.51.100.40",
		CheckedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
		Allowed:   true,
		Summary:   "cached",
	})

	if err := cache.Close(); err != nil {
		t.Fatalf("cache.Close returned error: %v", err)
	}

	reloaded, err := NewCache(cachePath, 25*time.Millisecond, logger)
	if err != nil {
		t.Fatalf("reloading cache returned error: %v", err)
	}
	defer func() { _ = reloaded.Close() }()

	decision, ok := reloaded.Get("198.51.100.40")
	if !ok {
		t.Fatalf("expected cached decision to be reloaded from disk")
	}
	if decision.IP != "198.51.100.40" {
		t.Fatalf("unexpected cached IP: %s", decision.IP)
	}
}
