package antivpn

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/netip"
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
	})

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
