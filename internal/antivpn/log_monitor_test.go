package antivpn

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNextLogOffsetSteadyState(t *testing.T) {
	offset, action, skipped := nextLogOffset(1000, 1500, 1<<20)
	if offset != 1000 {
		t.Fatalf("expected offset to remain 1000 when within cap, got %d", offset)
	}
	if action != logOffsetActionNone {
		t.Fatalf("expected logOffsetActionNone, got %d", action)
	}
	if skipped != 0 {
		t.Fatalf("expected skipped=0, got %d", skipped)
	}
}

func TestNextLogOffsetReattachOnShrink(t *testing.T) {
	// Simulates the regression that produced 10000s of replayed audit
	// events: server.log shrank (rotation, truncate, or replacement) so
	// previousOffset is now past the new EOF. Historical replay must be
	// avoided; the tailer must re-attach to the current end-of-file.
	offset, action, skipped := nextLogOffset(50_000, 200, 1<<20)
	if offset != 200 {
		t.Fatalf("expected re-attach to new size 200, got %d", offset)
	}
	if action != logOffsetActionReattachAfterShrink {
		t.Fatalf("expected reattach action, got %d", action)
	}
	if skipped != 0 {
		t.Fatalf("expected skipped=0 (we don't count skipped bytes for shrink), got %d", skipped)
	}
}

func TestNextLogOffsetSkipsBacklogBeyondCap(t *testing.T) {
	// A genuine append larger than the per-poll cap must skip the leading
	// bytes rather than enqueue a million events into handleLogLine in
	// one iteration.
	cap := int64(1 << 20)
	previous := int64(0)
	current := int64(5 << 20) // 5 MB unread
	offset, action, skipped := nextLogOffset(previous, current, cap)
	if offset != current-cap {
		t.Fatalf("expected offset = current-cap (%d), got %d", current-cap, offset)
	}
	if action != logOffsetActionSkipBacklog {
		t.Fatalf("expected skip-backlog action, got %d", action)
	}
	if skipped != current-previous-cap {
		t.Fatalf("expected skipped = %d, got %d", current-previous-cap, skipped)
	}
}

func TestNextLogOffsetCapDisabledWhenNonPositive(t *testing.T) {
	offset, action, skipped := nextLogOffset(0, 50<<20, 0)
	if offset != 0 {
		t.Fatalf("expected offset to remain 0 when cap is disabled, got %d", offset)
	}
	if action != logOffsetActionNone {
		t.Fatalf("expected no action when cap is disabled, got %d", action)
	}
	if skipped != 0 {
		t.Fatalf("expected skipped=0, got %d", skipped)
	}
}

// TestMonitorLogFileDoesNotReplayHistoryAfterTruncate exercises the full
// monitorLogFile loop end-to-end against a real temp file. It writes a
// large block of historical ClientConnect events, lets the tailer attach
// (so offset moves to EOF without seeing them), then truncates the file and
// writes ONE genuinely-new ClientConnect line. The tailer must NOT replay
// the historical events; only the post-truncate line should be observed.
//
// This is the regression test for the audit/broadcast storm reported by
// operators where 10000s of cached "VPN PASS" decisions were emitted after
// a map_restart on a server with active session history.
func TestMonitorLogFileDoesNotReplayHistoryAfterTruncate(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "server.log")

	// Pre-populate with historical ClientConnect lines that the tailer
	// must NEVER replay. Use distinct IPs so a replay would be obvious in
	// the captured connection state.
	historical := "" +
		"2026-04-25 01:18:50 ClientConnect: 1 [85.223.11.238] (X) \"old1\"\n" +
		"2026-04-25 01:18:51 ClientConnect: 2 [176.63.23.220] (X) \"old2\"\n" +
		"2026-04-25 01:18:52 ClientConnect: 3 [213.181.126.70] (X) \"old3\"\n"
	if err := os.WriteFile(logPath, []byte(historical), 0o644); err != nil {
		t.Fatalf("seed historical log: %v", err)
	}

	supervisor := &Supervisor{
		cfg: Config{
			LogPath:             logPath,
			LogPollInterval:     20 * time.Millisecond,
			EventDedupeInterval: 90 * time.Second,
			ScoreThreshold:      90,
			CacheTTL:            time.Hour,
			Mode:                ModeLogOnly,
		},
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		seenEvents:      make(map[string]time.Time),
		broadcastSeen:   make(map[string]time.Time),
		connectionState: make(map[string]slotConnectionState),
		checkSlots:      make(chan struct{}, 8),
	}
	// Provide a real engine with no providers so processConnectionEvent's
	// goroutine resolves quickly without panicking on a nil engine.
	engineCfg := supervisor.cfg
	engineCfg.CachePath = ""
	eng, err := NewEngine(engineCfg, supervisor.logger)
	if err != nil {
		t.Fatalf("build test engine: %v", err)
	}
	supervisor.engine = eng
	t.Cleanup(func() { _ = eng.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		supervisor.monitorLogFile(ctx, io.Discard)
	}()

	// Give the monitor enough polls to attach to current EOF.
	time.Sleep(120 * time.Millisecond)

	// At this point the tailer is attached at the end of the historical
	// block. None of those slots should have entered the connection
	// state, since handleLogLine was never called for them.
	if _, ok := supervisor.lookupConnectionState("1"); ok {
		t.Fatal("did not expect historical slot 1 to be processed at attach time")
	}

	// Truncate the file (simulates rotation/truncate observed in
	// production server.log handling) and write ONE fresh event. Use a
	// separate IP so a replay would land different state in the map.
	if err := os.Truncate(logPath, 0); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	// Wait long enough for the monitor to observe the shrink (size=0)
	// and re-attach to the new EOF (offset=0). Without this wait, the
	// fresh append below could land before the monitor notices the
	// truncation, causing the entire post-truncate window to be
	// (correctly) skipped together with the historical data.
	time.Sleep(120 * time.Millisecond)

	fresh := "2026-04-25 01:19:30 ClientConnect: 0 [203.0.113.42] (X) \"newcomer\"\n"
	// Re-open with O_APPEND to mimic engine append behaviour after truncate.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := f.WriteString(fresh); err != nil {
		t.Fatalf("append fresh: %v", err)
	}
	f.Close()

	// Wait long enough for at least a couple of poll iterations.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := supervisor.lookupConnectionState("0"); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The fresh event must have been observed (it triggered storeConnectionState
	// via processConnectionEvent's parseClientConnect path).
	if state, ok := supervisor.lookupConnectionState("0"); !ok {
		t.Fatal("expected fresh post-truncate ClientConnect to be processed")
	} else if state.Addr.String() != "203.0.113.42" {
		t.Fatalf("expected fresh IP 203.0.113.42 in slot 0, got %s", state.Addr)
	}

	// Crucially: the historical slots must STILL be absent. A regression
	// of the offset=0 replay would have populated all three of them.
	for _, slot := range []string{"1", "2", "3"} {
		if state, ok := supervisor.lookupConnectionState(slot); ok {
			t.Fatalf("historical slot %s must not have been replayed after truncate; got addr=%s", slot, state.Addr)
		}
	}
}
