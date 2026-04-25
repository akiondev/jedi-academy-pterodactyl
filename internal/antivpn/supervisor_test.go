package antivpn

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseClientUserinfoChangedFromTimestampedLine(t *testing.T) {
	line := `2026-04-16 17:28:09 ClientUserinfoChanged: 3 n\Player\t\0\ip\198.51.100.25:29070\cl_guid\abc123`

	slot, addr, player, ok := parseClientUserinfoChanged(line)
	if !ok {
		t.Fatalf("expected parser to match ClientUserinfoChanged line")
	}
	if slot != "3" {
		t.Fatalf("expected slot 3, got %q", slot)
	}
	if addr != netip.MustParseAddr("198.51.100.25") {
		t.Fatalf("expected parsed IP 198.51.100.25, got %s", addr)
	}
	if player != "Player" {
		t.Fatalf("expected parsed player name Player, got %q", player)
	}
}

func TestParseClientConnectFromTimestampedLine(t *testing.T) {
	line := `2026-01-17 22:16:15 ClientConnect: 0 [83.249.104.192] (3ADCC69C97BCC62079B59FF5161ED65D) "Akion"`

	slot, addr, player, ok := parseClientConnect(line)
	if !ok {
		t.Fatalf("expected parser to match ClientConnect line")
	}
	if slot != "0" {
		t.Fatalf("expected slot 0, got %q", slot)
	}
	if addr != netip.MustParseAddr("83.249.104.192") {
		t.Fatalf("expected parsed IP 83.249.104.192, got %s", addr)
	}
	if player != "Akion" {
		t.Fatalf("expected parsed player name Akion, got %q", player)
	}
}

func TestParseClientConnectSupportsBracketedPortSuffix(t *testing.T) {
	line := `2026-01-17 22:16:29 ClientConnect: 0 [83.249.104.192:29070] (3ADCC69C97BCC62079B59FF5161ED65D) "Akion"`

	slot, addr, player, ok := parseClientConnect(line)
	if !ok {
		t.Fatalf("expected parser to match ClientConnect line with port")
	}
	if slot != "0" {
		t.Fatalf("expected slot 0, got %q", slot)
	}
	if addr != netip.MustParseAddr("83.249.104.192") {
		t.Fatalf("expected parsed IP 83.249.104.192, got %s", addr)
	}
	if player != "Akion" {
		t.Fatalf("expected parsed player name Akion, got %q", player)
	}
}

func TestParseClientConnectNormalizesTrailingColorReset(t *testing.T) {
	line := `2026-01-17 22:16:29 ClientConnect: 0 [83.249.104.192:29070] (3ADCC69C97BCC62079B59FF5161ED65D) "SamplePlayer^7"`

	_, _, player, ok := parseClientConnect(line)
	if !ok {
		t.Fatalf("expected parser to match ClientConnect line with trailing color reset")
	}
	if player != "SamplePlayer" {
		t.Fatalf("expected parsed player name SamplePlayer, got %q", player)
	}
}

func TestParseClientConnectFromANSIWrappedLine(t *testing.T) {
	line := "\x1b[32m2026-01-17 22:16:15 ClientConnect: 0 [83.249.104.192] (GUID) \"Akion\"\x1b[0m"
	line = normalizeLogLineForParsing(line)

	slot, addr, player, ok := parseClientConnect(line)
	if !ok {
		t.Fatalf("expected parser to match ANSI-wrapped ClientConnect line")
	}
	if slot != "0" {
		t.Fatalf("expected slot 0, got %q", slot)
	}
	if addr != netip.MustParseAddr("83.249.104.192") {
		t.Fatalf("expected parsed IP 83.249.104.192, got %s", addr)
	}
	if player != "Akion" {
		t.Fatalf("expected parsed player name Akion, got %q", player)
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

func TestParseClientUserinfoChangedFieldsWithoutIPStillReturnsName(t *testing.T) {
	line := `2026-01-17 22:16:15 ClientUserinfoChanged: 0 n\Akion\t\3\model\jeditrainer/blue`

	slot, addr, player, hasAddr, ok := parseClientUserinfoChangedFields(line)
	if !ok {
		t.Fatalf("expected parser to match ClientUserinfoChanged line without ip")
	}
	if slot != "0" {
		t.Fatalf("expected slot 0, got %q", slot)
	}
	if hasAddr {
		t.Fatalf("expected no parsed IP, got %s", addr)
	}
	if player != "Akion" {
		t.Fatalf("expected parsed player name Akion, got %q", player)
	}
}

func TestParseClientUserinfoChangedFieldsRejectsNoChangeLines(t *testing.T) {
	line := `2026-01-17 22:16:21 ClientUserinfoChanged: 0 <no change>`

	if _, _, _, _, ok := parseClientUserinfoChangedFields(line); ok {
		t.Fatal("expected parser to ignore <no change> userinfo line")
	}
}

func TestParseClientUserinfoChangedFieldsDoesNotMatchChatPayload(t *testing.T) {
	line := `say: Player: ClientUserinfoChanged: 0 n\Fake\ip\198.51.100.25:29070`

	if _, _, _, _, ok := parseClientUserinfoChangedFields(line); ok {
		t.Fatal("expected parser to reject chat line that only contains event text")
	}
}

func TestExtractUserinfoValueMatchesWholeKeysOnly(t *testing.T) {
	line := `name\Wrong\n\RightPlayer\ip\198.51.100.25:29070`

	player := extractUserinfoValue(line, "n")
	if player != "RightPlayer" {
		t.Fatalf("expected exact n key to resolve to RightPlayer, got %q", player)
	}
}

func TestExtractUserinfoValueSupportsLeadingSlash(t *testing.T) {
	line := `\n\RightPlayer\ip\198.51.100.25:29070`

	player := extractUserinfoValue(line, "n")
	if player != "RightPlayer" {
		t.Fatalf("expected leading-slash userinfo to resolve to RightPlayer, got %q", player)
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

func TestSanitizePlayerNameStripsFormatting(t *testing.T) {
	name := sanitizePlayerName("^1Cool^7Player\x00")
	if name != "CoolPlayer" {
		t.Fatalf("unexpected sanitized player name: %q", name)
	}
}

func TestSanitizePlayerNameForConsoleCommandStripsCommandSeparators(t *testing.T) {
	name := sanitizePlayerNameForConsoleCommand(`^1Bob^7;clientkick 0 "$whoami"` + "\n")
	if strings.ContainsAny(name, `;"'\$`+"\r\n") {
		t.Fatalf("console-safe player name still contains command-breaking characters: %q", name)
	}
	if name != "Bobclientkick 0 whoami" {
		t.Fatalf("unexpected console-safe player name: %q", name)
	}
}

func TestPublicDecisionSummaryUsesPublicSafeText(t *testing.T) {
	blocked := publicDecisionSummary(Decision{
		Blocked:            true,
		StrongSignals:      1,
		DetectingProviders: 1,
	})
	if blocked != "High-confidence VPN or non-residential signal detected." {
		t.Fatalf("unexpected blocked summary: %q", blocked)
	}

	passed := publicDecisionSummary(Decision{
		Allowed:       true,
		Degraded:      true,
		ProviderSuccesses: 1,
	})
	if passed != "Allowed with partial provider coverage." {
		t.Fatalf("unexpected pass summary: %q", passed)
	}
}

func TestFillCommandTemplateSupportsBroadcastPlaceholders(t *testing.T) {
	command := fillCommandTemplate(
		"say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%",
		commandTemplateData{
			Player:    "Player",
			Score:     10,
			Threshold: 90,
			Summary:   "No provider reported a VPN or hosting signal.",
			Slot:      "3",
			IP:        "198.51.100.25",
		},
	)

	expected := "say [Anti-VPN] VPN PASS: Player cleared checks (10/90). No provider reported a VPN or hosting signal."
	if command != expected {
		t.Fatalf("unexpected rendered broadcast command: %q", command)
	}
}

func TestBroadcastDecisionSanitizesPlayerNameBeforeWritingCommand(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	supervisor := &Supervisor{
		cfg: Config{
			BroadcastMode:        BroadcastPassAndBlock,
			BroadcastPassCommand: `say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%`,
		},
		logger: logger,
	}

	var stdin bytes.Buffer
	supervisor.broadcastDecision(&stdin, "3", `^1Bob^7;clientkick 0 "oops"`, Decision{
		Allowed:           true,
		Score:             10,
		Threshold:         90,
		ProviderSuccesses: 1,
	}, "connect")

	command := strings.TrimSpace(stdin.String())
	if command == "" {
		t.Fatal("expected broadcast command to be written")
	}
	if strings.Contains(command, "clientkick") && strings.Contains(command, ";") {
		t.Fatalf("broadcast command still contains injectable separator: %q", command)
	}
	if strings.ContainsAny(command, "\"'`$") {
		t.Fatalf("broadcast command still contains unsafe quote-like characters: %q", command)
	}
	if !strings.Contains(command, "Bobclientkick 0 oops") {
		t.Fatalf("expected sanitized player name to remain in broadcast command, got %q", command)
	}
}

func TestHandleLogLineClearsTrackedConnectionStateOnDisconnect(t *testing.T) {
	supervisor := &Supervisor{
		connectionState: map[string]slotConnectionState{
			"0": {
				Addr:       netip.MustParseAddr("83.249.104.192"),
				PlayerName: "Akion",
				SeenAt:     time.Now().UTC(),
			},
		},
	}

	supervisor.handleLogLine(context.Background(), io.Discard, `2026-01-17 22:16:29 ClientDisconnect: 0 [83.249.104.192:29070] (GUID) "Akion"`, "stdout")

	if _, ok := supervisor.lookupConnectionState("0"); ok {
		t.Fatal("expected tracked slot state to be cleared on disconnect")
	}
}

func TestClearConnectionStateClearsSeenEvents(t *testing.T) {
	supervisor := &Supervisor{
		connectionState: map[string]slotConnectionState{
			"0": {
				Addr:       netip.MustParseAddr("83.249.104.192"),
				PlayerName: "Player",
				SeenAt:     time.Now().UTC(),
			},
		},
		seenEvents: map[string]time.Time{
			"0|83.249.104.192": time.Now().UTC(),
		},
		broadcastSeen: map[string]time.Time{
			"83.249.104.192|allow": time.Now().UTC(),
			"83.249.104.192|block": time.Now().UTC(),
			"203.0.113.5|allow":    time.Now().UTC(),
		},
	}

	supervisor.clearConnectionState("0")

	if _, ok := supervisor.connectionState["0"]; ok {
		t.Fatal("expected connection state to be cleared on disconnect")
	}

	supervisor.seenMu.Lock()
	_, seenExists := supervisor.seenEvents["0|83.249.104.192"]
	supervisor.seenMu.Unlock()

	if seenExists {
		t.Fatal("expected seenEvents entry to be cleared on disconnect so rapid reconnects get a fresh check")
	}

	// Broadcast cooldown entries must survive disconnect so that rapid
	// kick-and-reconnect cycles cannot bypass the cooldown and spam the console.
	supervisor.broadcastMu.Lock()
	_, broadcastAllowExists := supervisor.broadcastSeen["83.249.104.192|allow"]
	_, broadcastBlockExists := supervisor.broadcastSeen["83.249.104.192|block"]
	_, unrelatedExists := supervisor.broadcastSeen["203.0.113.5|allow"]
	supervisor.broadcastMu.Unlock()

	if !broadcastAllowExists || !broadcastBlockExists {
		t.Fatal("expected broadcast cooldown entries to persist across disconnect")
	}
	if !unrelatedExists {
		t.Fatal("expected unrelated broadcast cooldown entries to remain")
	}
}

func TestBroadcastCooldownPersistsDespiteRapidReconnect(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	supervisor := &Supervisor{
		cfg: Config{
			BroadcastMode:        BroadcastPassAndBlock,
			BroadcastCooldown:    90 * time.Second,
			BroadcastPassCommand: `say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%`,
		},
		logger:        logger,
		broadcastSeen: make(map[string]time.Time),
	}

	decision := Decision{
		IP:        "198.51.100.25",
		Allowed:   true,
		Score:     10,
		Threshold: 90,
	}

	var stdin bytes.Buffer

	// First broadcast must be sent.
	supervisor.broadcastDecision(&stdin, "0", "Player", decision, "connect")
	if stdin.Len() == 0 {
		t.Fatal("expected first broadcast to be sent")
	}

	// Verify the broadcast cooldown entry was recorded.
	supervisor.broadcastMu.Lock()
	broadcastKeyExists := false
	for k := range supervisor.broadcastSeen {
		if strings.HasPrefix(k, "198.51.100.25|") {
			broadcastKeyExists = true
		}
	}
	supervisor.broadcastMu.Unlock()

	if !broadcastKeyExists {
		t.Fatal("expected broadcastSeen entry to exist after first broadcast")
	}

	// Second broadcast (simulating immediate reconnect) must be suppressed.
	stdin.Reset()
	supervisor.broadcastDecision(&stdin, "0", "Player", decision, "connect")
	if stdin.Len() != 0 {
		t.Fatalf("expected second broadcast within cooldown to be suppressed, got: %q", stdin.String())
	}

	// Broadcast on a different slot must also be suppressed (IP-level cooldown).
	stdin.Reset()
	supervisor.broadcastDecision(&stdin, "1", "Player", decision, "connect")
	if stdin.Len() != 0 {
		t.Fatalf("expected broadcast for different slot within cooldown to be suppressed, got: %q", stdin.String())
	}
}

func TestHandleLogLineSkipsCheckOnUserinfoWithoutIP(t *testing.T) {
	supervisor := &Supervisor{
		cfg: Config{EventDedupeInterval: 90 * time.Second},
		connectionState: map[string]slotConnectionState{
			"3": {
				Addr:       netip.MustParseAddr("198.51.100.25"),
				PlayerName: "OldName",
				SeenAt:     time.Now().UTC(),
			},
		},
		seenEvents: make(map[string]time.Time),
		checkSlots: make(chan struct{}, 8),
	}

	// Simulate a team/name change with no IP field — should not trigger a check.
	supervisor.handleLogLine(context.Background(), io.Discard,
		`2026-01-17 22:16:15 ClientUserinfoChanged: 3 n\NewName\t\3\model\jeditrainer/blue`,
		"stdout")

	supervisor.seenMu.Lock()
	seenCount := len(supervisor.seenEvents)
	supervisor.seenMu.Unlock()

	if seenCount != 0 {
		t.Fatalf("expected no seenEvents after a name/team-only userinfo change, got %d", seenCount)
	}

	state, ok := supervisor.lookupConnectionState("3")
	if !ok {
		t.Fatal("expected connection state to still exist after name-only userinfo change")
	}
	if state.PlayerName != "NewName" {
		t.Fatalf("expected player name updated to NewName, got %q", state.PlayerName)
	}
}

func TestHandleLogLineSkipsCheckOnUserinfoWithSameIP(t *testing.T) {
	supervisor := &Supervisor{
		cfg: Config{EventDedupeInterval: 90 * time.Second},
		connectionState: map[string]slotConnectionState{
			"3": {
				Addr:       netip.MustParseAddr("198.51.100.25"),
				PlayerName: "Player",
				SeenAt:     time.Now().UTC(),
			},
		},
		seenEvents: make(map[string]time.Time),
		checkSlots: make(chan struct{}, 8),
	}

	// Simulate a ClientUserinfoChanged with the same IP that the engine already tracks.
	// Should not call processConnectionEvent (seenEvents must remain empty and no panic
	// from a nil engine).
	supervisor.handleLogLine(context.Background(), io.Discard,
		`2026-04-16 17:28:09 ClientUserinfoChanged: 3 n\Player\t\3\ip\198.51.100.25:29070\cl_guid\abc123`,
		"stdout")

	supervisor.seenMu.Lock()
	seenCount := len(supervisor.seenEvents)
	supervisor.seenMu.Unlock()

	if seenCount != 0 {
		t.Fatalf("expected no seenEvents after userinfo change with unchanged IP, got %d", seenCount)
	}
}

func TestHandleLogLineParsesANSIWrappedClientConnect(t *testing.T) {
	supervisor := &Supervisor{
		cfg: Config{
			EventDedupeInterval: time.Hour,
		},
		connectionState: make(map[string]slotConnectionState),
		seenEvents: map[string]time.Time{
			"0|83.249.104.192": time.Now().UTC(),
		},
		checkSlots: make(chan struct{}, 1),
	}

	supervisor.handleLogLine(
		context.Background(),
		io.Discard,
		"\x1b[32m2026-01-17 22:16:15 ClientConnect: 0 [83.249.104.192] (GUID) \"Akion\"\x1b[0m",
		"stdout",
	)

	state, ok := supervisor.lookupConnectionState("0")
	if !ok {
		t.Fatal("expected ANSI-wrapped ClientConnect to update tracked connection state")
	}
	if state.Addr != netip.MustParseAddr("83.249.104.192") {
		t.Fatalf("unexpected tracked address after ANSI-wrapped connect: %s", state.Addr)
	}
	if state.PlayerName != "Akion" {
		t.Fatalf("unexpected tracked player name after ANSI-wrapped connect: %q", state.PlayerName)
	}
}

func TestAuditDecisionLogsAllowWhenAuditAllowEnabled(t *testing.T) {
dir := t.TempDir()
auditPath := filepath.Join(dir, "audit.log")
t.Setenv("JKA_RUNTIME_CONFIG_PATH", "/nonexistent/jka-runtime.json")
t.Setenv("ANTI_VPN_AUDIT_LOG_PATH", auditPath)
t.Setenv("ANTI_VPN_AUDIT_ALLOW", "true")
t.Setenv("ANTI_VPN_PROXYCHECK_API_KEY", "SUPER-SECRET-KEY-7777")

cfg, err := LoadConfigFromEnv()
if err != nil {
t.Fatalf("LoadConfigFromEnv: %v", err)
}
if !cfg.AuditAllow {
t.Fatalf("expected default AuditAllow=true")
}

supervisor, err := NewSupervisor(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
if err != nil {
t.Fatalf("NewSupervisor: %v", err)
}
t.Cleanup(func() { _ = supervisor.Close() })

supervisor.auditDecision("test", "0", netip.MustParseAddr("203.0.113.5"), Decision{
Allowed: true,
Score:   10,
Summary: "ok",
})

contents, err := os.ReadFile(auditPath)
if err != nil {
t.Fatalf("read audit log: %v", err)
}
body := string(contents)
if !strings.Contains(body, `"action":"allow"`) {
t.Fatalf("expected allow row in audit log, got: %s", body)
}
if !strings.Contains(body, `"ip":"203.0.113.5"`) {
t.Fatalf("expected ip in audit log, got: %s", body)
}
if strings.Contains(body, "SUPER-SECRET-KEY-7777") {
t.Fatalf("audit log must never contain provider API keys, got: %s", body)
}
}

func TestAuditDecisionSkipsAllowWhenAuditAllowDisabled(t *testing.T) {
dir := t.TempDir()
auditPath := filepath.Join(dir, "audit.log")
t.Setenv("JKA_RUNTIME_CONFIG_PATH", "/nonexistent/jka-runtime.json")
t.Setenv("ANTI_VPN_AUDIT_LOG_PATH", auditPath)
t.Setenv("ANTI_VPN_AUDIT_ALLOW", "false")

cfg, err := LoadConfigFromEnv()
if err != nil {
t.Fatalf("LoadConfigFromEnv: %v", err)
}

supervisor, err := NewSupervisor(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
if err != nil {
t.Fatalf("NewSupervisor: %v", err)
}
t.Cleanup(func() { _ = supervisor.Close() })

supervisor.auditDecision("test", "0", netip.MustParseAddr("203.0.113.6"), Decision{
Allowed: true,
Score:   5,
Summary: "ok",
})

contents, _ := os.ReadFile(auditPath)
if strings.Contains(string(contents), `"action":"allow"`) {
t.Fatalf("expected no allow row when AuditAllow=false, got: %s", string(contents))
}
}
