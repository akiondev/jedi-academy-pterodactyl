package antivpn

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestBroadcastDecisionSuppressesCachedUserinfoDuringReinitBurst(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	supervisor := &Supervisor{
		cfg: Config{
			BroadcastMode:            BroadcastPassAndBlock,
			BroadcastCooldown:        90 * time.Second,
			BroadcastEmissionSpacing: 100 * time.Millisecond,
			BroadcastPassCommand:     `say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%`,
		},
		logger:        logger,
		broadcastSeen: make(map[string]time.Time),
	}

	// Simulate the engine emitting an InitGame line; this opens the
	// reinit-burst suppression window.
	supervisor.markReinitBurst()

	cachedDecision := Decision{
		IP:        "198.51.100.25",
		Allowed:   true,
		FromCache: true,
		Score:     10,
		Threshold: 90,
	}

	var stdin bytes.Buffer
	supervisor.broadcastDecision(&stdin, "0", "Player", cachedDecision, "userinfo")
	if stdin.Len() != 0 {
		t.Fatalf("expected cached userinfo broadcast to be suppressed during reinit burst, got: %q", stdin.String())
	}

	// A real ClientConnect during the same burst window must still broadcast
	// because that represents a genuine new player joining mid-restart.
	supervisor.broadcastDecision(&stdin, "1", "Newcomer", Decision{
		IP:        "203.0.113.7",
		Allowed:   true,
		FromCache: false,
		Score:     0,
		Threshold: 90,
	}, "connect")
	if stdin.Len() == 0 {
		t.Fatal("expected ClientConnect broadcast to be emitted even during reinit burst")
	}
}

func TestBroadcastDecisionEmitsBlockedDecisionEvenDuringReinitBurst(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	supervisor := &Supervisor{
		cfg: Config{
			BroadcastMode:         BroadcastPassAndBlock,
			BroadcastCooldown:     90 * time.Second,
			BroadcastBlockCommand: `say [Anti-VPN] VPN BLOCKED: %PLAYER% triggered anti-VPN (%SCORE%/%THRESHOLD%). %SUMMARY%`,
		},
		logger:        logger,
		broadcastSeen: make(map[string]time.Time),
	}

	supervisor.markReinitBurst()

	var stdin bytes.Buffer
	supervisor.broadcastDecision(&stdin, "0", "Bad", Decision{
		IP:        "198.51.100.99",
		Blocked:   true,
		FromCache: true,
		Score:     95,
		Threshold: 90,
	}, "userinfo")
	if stdin.Len() == 0 {
		t.Fatal("expected blocked decision to broadcast even during reinit burst")
	}
	if !strings.Contains(stdin.String(), "VPN BLOCKED") {
		t.Fatalf("expected blocked broadcast text, got: %q", stdin.String())
	}
}

func TestHandleLogLineMarksReinitBurstOnInitGameLine(t *testing.T) {
	supervisor := &Supervisor{
		cfg: Config{
			BroadcastEmissionSpacing: 100 * time.Millisecond,
		},
		seenEvents:      make(map[string]time.Time),
		connectionState: make(map[string]slotConnectionState),
	}

	if supervisor.inReinitBurst() {
		t.Fatal("expected supervisor to start outside reinit burst window")
	}

	supervisor.handleLogLine(nil, io.Discard, `2026-04-24 21:27:50 InitGame: \version\TaystJK`, "stdout")

	if !supervisor.inReinitBurst() {
		t.Fatal("expected InitGame line to open the reinit burst window")
	}
}
