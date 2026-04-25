package antivpn

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestParseBadRconWithIPv4Port(t *testing.T) {
	event, ok := parseBadRcon("Bad rcon from 90.144.88.223:29070: status")
	if !ok {
		t.Fatalf("expected parser to match line")
	}
	if event.Host != "90.144.88.223" {
		t.Fatalf("expected host 90.144.88.223, got %q", event.Host)
	}
	if event.Port != 29070 {
		t.Fatalf("expected port 29070, got %d", event.Port)
	}
	if event.Command != "status" {
		t.Fatalf("expected command status, got %q", event.Command)
	}
	if event.IP != netip.MustParseAddr("90.144.88.223") {
		t.Fatalf("expected parsed IP, got %s", event.IP)
	}
}

func TestParseBadRconWithTimestampAndIPv6(t *testing.T) {
	event, ok := parseBadRcon("2026-04-25 10:44:52 Bad rcon from ::1: serverstatus")
	if !ok {
		t.Fatalf("expected parser to match timestamped IPv6 line")
	}
	if event.Host != "::1" {
		t.Fatalf("expected host ::1, got %q", event.Host)
	}
	if event.Port != 0 {
		t.Fatalf("expected unset port, got %d", event.Port)
	}
	if event.Command != "serverstatus" {
		t.Fatalf("expected command serverstatus, got %q", event.Command)
	}
	if event.IP != netip.MustParseAddr("::1") {
		t.Fatalf("expected parsed loopback IPv6, got %s", event.IP)
	}
}

func TestParseBadRconRejectsUnrelatedLine(t *testing.T) {
	if _, ok := parseBadRcon("ClientConnect: 0 [10.0.0.1]"); ok {
		t.Fatalf("expected parser to reject unrelated line")
	}
}

func newTestSupervisor(t *testing.T, cfg Config) *Supervisor {
	t.Helper()
	if cfg.RconGuard.Action == "" {
		cfg.RconGuard.Action = "kick"
	}
	if cfg.RconGuard.IgnoreHosts == nil {
		cfg.RconGuard.IgnoreHosts = []string{}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Supervisor{
		cfg:             cfg,
		logger:          logger,
		seenEvents:      make(map[string]time.Time),
		broadcastSeen:   make(map[string]time.Time),
		connectionState: make(map[string]slotConnectionState),
		checkSlots:      make(chan struct{}, 8),
	}
}

func TestRconGuardIgnoresLocalhostByName(t *testing.T) {
	s := newTestSupervisor(t, Config{
		RconGuard: RconGuardConfig{
			Enabled:     true,
			Action:      "kick",
			Broadcast:   true,
			IgnoreHosts: []string{"127.0.0.1", "::1", "localhost"},
		},
	})

	var stdin bytes.Buffer
	event, _ := parseBadRcon("Bad rcon from localhost: status")
	s.handleBadRcon(&stdin, "stdout", event.Raw, event)

	if stdin.Len() != 0 {
		t.Fatalf("expected no commands written for localhost RCON, got %q", stdin.String())
	}
}

func TestRconGuardIgnoresLoopbackIP(t *testing.T) {
	s := newTestSupervisor(t, Config{
		RconGuard: RconGuardConfig{
			Enabled:     true,
			Action:      "kick",
			IgnoreHosts: []string{"127.0.0.1", "::1", "localhost"},
		},
	})

	var stdin bytes.Buffer
	event, _ := parseBadRcon("Bad rcon from 127.0.0.1:1234: status")
	s.handleBadRcon(&stdin, "stdout", event.Raw, event)

	if stdin.Len() != 0 {
		t.Fatalf("expected no commands written for loopback RCON, got %q", stdin.String())
	}
}

func TestRconGuardLogsOnlyWhenSourceIPNotConnected(t *testing.T) {
	s := newTestSupervisor(t, Config{
		RconGuard: RconGuardConfig{
			Enabled:     true,
			Action:      "kick",
			Broadcast:   true,
			IgnoreHosts: []string{"127.0.0.1"},
		},
	})

	var stdin bytes.Buffer
	event, _ := parseBadRcon("Bad rcon from 90.144.88.223:29070: status")
	s.handleBadRcon(&stdin, "stdout", event.Raw, event)

	if stdin.Len() != 0 {
		t.Fatalf("expected no kick / broadcast for unmapped IP, got %q", stdin.String())
	}
}

func TestRconGuardKicksConnectedSlotByIP(t *testing.T) {
	s := newTestSupervisor(t, Config{
		BroadcastEmissionSpacing: 0,
		RconGuard: RconGuardConfig{
			Enabled:     true,
			Action:      "kick",
			Broadcast:   false,
			IgnoreHosts: []string{"127.0.0.1"},
		},
	})

	s.storeConnectionState("3", netip.MustParseAddr("90.144.88.223"), "Akion")

	var stdin bytes.Buffer
	event, _ := parseBadRcon("Bad rcon from 90.144.88.223:29070: status")
	s.handleBadRcon(&stdin, "stdout", event.Raw, event)

	got := stdin.String()
	if !strings.Contains(got, "clientkick 3") {
		t.Fatalf("expected `clientkick 3`, got %q", got)
	}
}

func TestRconGuardDisabledIsNoOp(t *testing.T) {
	s := newTestSupervisor(t, Config{
		RconGuard: RconGuardConfig{
			Enabled:     false,
			Action:      "kick",
			IgnoreHosts: []string{"127.0.0.1"},
		},
	})
	s.storeConnectionState("3", netip.MustParseAddr("90.144.88.223"), "Akion")

	var stdin bytes.Buffer
	event, _ := parseBadRcon("Bad rcon from 90.144.88.223:29070: status")
	s.handleBadRcon(&stdin, "stdout", event.Raw, event)

	if stdin.Len() != 0 {
		t.Fatalf("expected disabled guard to be a no-op, got %q", stdin.String())
	}
}

func TestRconGuardLookupSlotByIPPicksMostRecent(t *testing.T) {
	s := newTestSupervisor(t, Config{})
	older := time.Now().UTC().Add(-time.Minute)

	s.connectionState["1"] = slotConnectionState{
		Addr:       netip.MustParseAddr("198.51.100.10"),
		PlayerName: "OldSession",
		SeenAt:     older,
	}
	s.storeConnectionState("4", netip.MustParseAddr("198.51.100.10"), "NewSession")

	slot, state, ok := s.lookupSlotByIP(netip.MustParseAddr("198.51.100.10"))
	if !ok {
		t.Fatalf("expected lookup to succeed")
	}
	if slot != "4" {
		t.Fatalf("expected most recent slot 4, got %q", slot)
	}
	if state.PlayerName != "NewSession" {
		t.Fatalf("expected most recent player name, got %q", state.PlayerName)
	}
}

func TestSupervisorAuditDecisionSuppressesAllowByDefault(t *testing.T) {
	var buf bytes.Buffer
	auditLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	s := newTestSupervisor(t, Config{AuditAllow: false})
	s.auditLogger = auditLogger

	s.auditDecision("stdout", "1", netip.MustParseAddr("85.223.11.238"), Decision{
		IP:        "85.223.11.238",
		Score:     10,
		Threshold: 90,
		FromCache: true,
	})

	if buf.Len() != 0 {
		t.Fatalf("expected allow decision to be suppressed, got %s", buf.String())
	}
}

func TestSupervisorAuditDecisionEmitsBlock(t *testing.T) {
	var buf bytes.Buffer
	auditLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	s := newTestSupervisor(t, Config{AuditAllow: false})
	s.auditLogger = auditLogger

	s.auditDecision("stdout", "1", netip.MustParseAddr("85.223.11.238"), Decision{
		IP:        "85.223.11.238",
		Score:     200,
		Threshold: 90,
		Blocked:   true,
	})

	if !strings.Contains(buf.String(), `"action":"block"`) {
		t.Fatalf("expected block decision to be audited, got %s", buf.String())
	}
}

func TestSupervisorAuditDecisionEmitsAllowWhenEnabled(t *testing.T) {
	var buf bytes.Buffer
	auditLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	s := newTestSupervisor(t, Config{AuditAllow: true})
	s.auditLogger = auditLogger

	s.auditDecision("stdout", "1", netip.MustParseAddr("85.223.11.238"), Decision{
		IP:        "85.223.11.238",
		Score:     10,
		Threshold: 90,
	})

	if !strings.Contains(buf.String(), `"action":"allow"`) {
		t.Fatalf("expected allow decision to be audited when AuditAllow=true, got %s", buf.String())
	}
}

func TestHandleLogLineDispatchesBadRconWithoutTouchingClientState(t *testing.T) {
	s := newTestSupervisor(t, Config{
		RconGuard: RconGuardConfig{
			Enabled:     true,
			Action:      "kick",
			IgnoreHosts: []string{"127.0.0.1"},
		},
	})
	s.storeConnectionState("3", netip.MustParseAddr("90.144.88.223"), "Akion")

	var stdin bytes.Buffer
	s.handleLogLine(context.Background(), &stdin, "Bad rcon from 90.144.88.223:29070: status", "stdout")

	if !strings.Contains(stdin.String(), "clientkick 3") {
		t.Fatalf("expected handleLogLine to dispatch bad_rcon and kick connected slot, got %q", stdin.String())
	}
}
