package antivpn

import (
	"bytes"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// newClassifierSupervisor builds a minimal Supervisor wired with the
// fields required to exercise classifyConnect + broadcastDecision in
// isolation (no engine, no providers, no event bus).
func newClassifierSupervisor(t *testing.T) *Supervisor {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Supervisor{
		cfg: Config{
			BroadcastMode:         BroadcastPassAndBlock,
			BroadcastCooldown:     0,
			BroadcastPassCommand:  `say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%`,
			BroadcastBlockCommand: `say [Anti-VPN] VPN BLOCKED: %PLAYER% triggered anti-VPN (%SCORE%/%THRESHOLD%). %SUMMARY%`,
		},
		logger:          logger,
		seenEvents:      make(map[string]time.Time),
		broadcastSeen:   make(map[string]time.Time),
		connectionState: make(map[string]slotConnectionState),
	}
}

// simulateConnect mimics the relevant slice of handleLogLine for a
// ClientConnect line: classify against prior state, then store the new
// state. Returns the classification used by broadcastDecision.
func simulateConnect(s *Supervisor, slot string, addr netip.Addr, name string) string {
	kind := s.classifyConnect(slot, addr)
	s.storeConnectionState(slot, addr, name)
	return kind
}

// passDecision returns a fresh Decision representing an allow result so
// that broadcast cooldown does not silently mask a missing suppression.
func passDecision(ip string) Decision {
	return Decision{
		IP:        ip,
		Allowed:   true,
		Score:     5,
		Threshold: 90,
	}
}

func emitPassBroadcast(t *testing.T, s *Supervisor, slot, name string, addr netip.Addr, kind string) string {
	t.Helper()
	// Reset cooldown so consecutive PASS broadcasts in the same test do
	// not get swallowed by shouldBroadcast's IP+action dedupe window.
	s.broadcastMu.Lock()
	s.broadcastSeen = make(map[string]time.Time)
	s.broadcastMu.Unlock()

	var stdin bytes.Buffer
	s.broadcastDecision(&stdin, slot, name, passDecision(addr.String()), kind)
	return stdin.String()
}

// --- Test A: real join broadcasts PASS ---------------------------------

func TestReinitConnect_A_RealJoinBroadcastsPass(t *testing.T) {
	s := newClassifierSupervisor(t)
	addrA := netip.MustParseAddr("90.144.88.223")

	kind := simulateConnect(s, "0", addrA, "sargasso")
	if kind != connectKindReal {
		t.Fatalf("expected real_connect for first join, got %q", kind)
	}
	out := emitPassBroadcast(t, s, "0", "sargasso", addrA, kind)
	if !strings.Contains(out, "VPN PASS") {
		t.Fatalf("expected VPN PASS broadcast, got: %q", out)
	}
}

// --- Test B: reinit duplicate suppresses second PASS -------------------

func TestReinitConnect_B_DuplicateAfterReinitIsSuppressed(t *testing.T) {
	s := newClassifierSupervisor(t)
	addrA := netip.MustParseAddr("90.144.88.223")

	// Real first join.
	kind := simulateConnect(s, "0", addrA, "sargasso")
	if kind != connectKindReal {
		t.Fatalf("expected real_connect for first join, got %q", kind)
	}
	if out := emitPassBroadcast(t, s, "0", "sargasso", addrA, kind); !strings.Contains(out, "VPN PASS") {
		t.Fatalf("expected first PASS, got: %q", out)
	}

	// Game VM reinit: ShutdownGame, InitGame:, ClientConnect for same
	// slot+IP, ClientBegin. Drive these through handleLogLine so the
	// reinit-burst window opens via the real code path.
	s.handleLogLine(nil, io.Discard, `ShutdownGame`, "stdout")
	s.handleLogLine(nil, io.Discard, `InitGame: \version\TaystJK`, "stdout")
	if !s.inReinitBurst() {
		t.Fatal("expected reinit burst window to be open after InitGame")
	}

	kind = simulateConnect(s, "0", addrA, "sargasso")
	if kind != connectKindReinit {
		t.Fatalf("expected reinit_connect for re-emitted ClientConnect, got %q", kind)
	}
	if out := emitPassBroadcast(t, s, "0", "sargasso", addrA, kind); out != "" {
		t.Fatalf("expected NO second PASS for reinit_connect, got: %q", out)
	}
}

// --- Test C: same reinit window, second slot+IP is real -----------------

func TestReinitConnect_C_DifferentSlotInBurstIsRealConnect(t *testing.T) {
	s := newClassifierSupervisor(t)
	addrA := netip.MustParseAddr("90.144.88.223")
	addrB := netip.MustParseAddr("203.0.113.7")

	kind := simulateConnect(s, "0", addrA, "sargasso")
	emitPassBroadcast(t, s, "0", "sargasso", addrA, kind)

	s.handleLogLine(nil, io.Discard, `ShutdownGame`, "stdout")
	s.handleLogLine(nil, io.Discard, `InitGame: \version\TaystJK`, "stdout")

	kind = simulateConnect(s, "0", addrA, "sargasso")
	if kind != connectKindReinit {
		t.Fatalf("expected reinit_connect for slot 0 same IP, got %q", kind)
	}
	if out := emitPassBroadcast(t, s, "0", "sargasso", addrA, kind); out != "" {
		t.Fatalf("expected suppression for reinit_connect, got: %q", out)
	}

	// Slot 1 has no prior state: even though we are still inside the
	// reinit burst window, this is a brand-new join.
	if !s.inReinitBurst() {
		t.Fatal("expected reinit burst still open within window")
	}
	kind = simulateConnect(s, "1", addrB, "newcomer")
	if kind != connectKindReal {
		t.Fatalf("expected real_connect for fresh slot 1 in burst, got %q", kind)
	}
	if out := emitPassBroadcast(t, s, "1", "newcomer", addrB, kind); !strings.Contains(out, "VPN PASS") {
		t.Fatalf("expected PASS broadcast for new slot 1 join, got: %q", out)
	}
}

// --- Test D: real reconnect after disconnect broadcasts PASS -----------

func TestReinitConnect_D_RealReconnectAfterDisconnectBroadcasts(t *testing.T) {
	s := newClassifierSupervisor(t)
	addrA := netip.MustParseAddr("90.144.88.223")

	kind := simulateConnect(s, "0", addrA, "sargasso")
	emitPassBroadcast(t, s, "0", "sargasso", addrA, kind)

	// Real ClientDisconnect clears the slot.
	s.handleLogLine(nil, io.Discard, `ClientDisconnect: 0 [90.144.88.223:29070] (GUID) "sargasso"`, "stdout")
	if _, ok := s.lookupConnectionState("0"); ok {
		t.Fatal("expected slot 0 state cleared on disconnect")
	}

	// Reinit window opens after the disconnect.
	s.handleLogLine(nil, io.Discard, `ShutdownGame`, "stdout")
	s.handleLogLine(nil, io.Discard, `InitGame: \version\TaystJK`, "stdout")

	kind = simulateConnect(s, "0", addrA, "sargasso")
	if kind != connectKindReal {
		t.Fatalf("expected real_connect after disconnect+reinit, got %q", kind)
	}
	if out := emitPassBroadcast(t, s, "0", "sargasso", addrA, kind); !strings.Contains(out, "VPN PASS") {
		t.Fatalf("expected PASS for real reconnect after disconnect, got: %q", out)
	}
}

// --- Test E: same slot but different IP is real_connect ----------------

func TestReinitConnect_E_SameSlotDifferentIPIsRealConnect(t *testing.T) {
	s := newClassifierSupervisor(t)
	addrA := netip.MustParseAddr("90.144.88.223")
	addrB := netip.MustParseAddr("198.51.100.25")

	kind := simulateConnect(s, "0", addrA, "sargasso")
	emitPassBroadcast(t, s, "0", "sargasso", addrA, kind)

	s.handleLogLine(nil, io.Discard, `ShutdownGame`, "stdout")
	s.handleLogLine(nil, io.Discard, `InitGame: \version\TaystJK`, "stdout")

	kind = simulateConnect(s, "0", addrB, "sargasso")
	if kind != connectKindReal {
		t.Fatalf("expected real_connect when slot 0 reused with different IP, got %q", kind)
	}
	if out := emitPassBroadcast(t, s, "0", "sargasso", addrB, kind); !strings.Contains(out, "VPN PASS") {
		t.Fatalf("expected PASS for slot reuse with different IP, got: %q", out)
	}
}

// --- Test F: server startup InitGame with no previous state ------------

func TestReinitConnect_F_StartupInitGameWithNoPriorStateIsRealConnect(t *testing.T) {
	s := newClassifierSupervisor(t)
	addrA := netip.MustParseAddr("90.144.88.223")

	// Fresh server start: no connectionState entries yet.
	s.handleLogLine(nil, io.Discard, `------ Server Initialization ------`, "stdout")
	s.handleLogLine(nil, io.Discard, `InitGame: \version\TaystJK`, "stdout")
	if !s.inReinitBurst() {
		t.Fatal("expected init burst window open at startup")
	}

	kind := simulateConnect(s, "0", addrA, "sargasso")
	if kind != connectKindReal {
		t.Fatalf("expected real_connect on first-ever join even within startup InitGame burst, got %q", kind)
	}
	if out := emitPassBroadcast(t, s, "0", "sargasso", addrA, kind); !strings.Contains(out, "VPN PASS") {
		t.Fatalf("expected PASS for first-ever join, got: %q", out)
	}
}

// --- BLOCKED still broadcasts even for reinit_connect ------------------

func TestReinitConnect_BlockedStillBroadcastsAndEnforcesEvenForReinitConnect(t *testing.T) {
	s := newClassifierSupervisor(t)
	addrA := netip.MustParseAddr("90.144.88.223")

	// Establish prior state then enter reinit burst window.
	simulateConnect(s, "0", addrA, "sargasso")
	s.handleLogLine(nil, io.Discard, `ShutdownGame`, "stdout")
	s.handleLogLine(nil, io.Discard, `InitGame: \version\TaystJK`, "stdout")
	kind := simulateConnect(s, "0", addrA, "sargasso")
	if kind != connectKindReinit {
		t.Fatalf("expected reinit_connect, got %q", kind)
	}

	var stdin bytes.Buffer
	s.broadcastDecision(&stdin, "0", "sargasso", Decision{
		IP:        addrA.String(),
		Blocked:   true,
		Score:     99,
		Threshold: 90,
	}, kind)
	if !strings.Contains(stdin.String(), "VPN BLOCKED") {
		t.Fatalf("expected BLOCKED broadcast even for reinit_connect, got: %q", stdin.String())
	}
}

// --- connectAuditExtras carries connect_kind and suppression flag ------

func TestConnectAuditExtras(t *testing.T) {
	if got := connectAuditExtras(triggerKindUserinfo, false); got != nil {
		t.Fatalf("expected no extras for userinfo trigger, got %v", got)
	}

	got := connectAuditExtras(connectKindReal, false)
	if len(got) != 2 || got[0] != "connect_kind" || got[1] != connectKindReal {
		t.Fatalf("expected real_connect kind extras, got %v", got)
	}

	got = connectAuditExtras(connectKindReinit, true)
	if len(got) != 4 {
		t.Fatalf("expected 4 extras for suppressed reinit_connect, got %v", got)
	}
	if got[0] != "connect_kind" || got[1] != connectKindReinit {
		t.Fatalf("expected connect_kind=reinit_connect, got %v", got)
	}
	if got[2] != "pass_broadcast_suppressed" || got[3] != true {
		t.Fatalf("expected pass_broadcast_suppressed=true, got %v", got)
	}
}
