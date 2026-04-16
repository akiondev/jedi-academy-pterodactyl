package antivpn

import (
	"net/netip"
	"testing"
)

func TestLoadConfigDefaultsToBlockMode(t *testing.T) {
	t.Setenv("ANTI_VPN_MODE", "")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv returned error: %v", err)
	}

	if cfg.Mode != ModeBlock {
		t.Fatalf("expected default anti-vpn mode %q, got %q", ModeBlock, cfg.Mode)
	}
}

func TestParseAllowlistSupportsIPsAndCIDRs(t *testing.T) {
	allowlist, err := parseAllowlist("203.0.113.10, 198.51.100.0/24\n2001:db8::/32")
	if err != nil {
		t.Fatalf("parseAllowlist returned error: %v", err)
	}

	expected := []netip.Prefix{
		netip.MustParsePrefix("203.0.113.10/32"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("2001:db8::/32"),
	}

	if len(allowlist) != len(expected) {
		t.Fatalf("expected %d allowlist entries, got %d", len(expected), len(allowlist))
	}
	for index := range expected {
		if allowlist[index] != expected[index] {
			t.Fatalf("unexpected allowlist entry %d: got %s want %s", index, allowlist[index], expected[index])
		}
	}
}

func TestParseAllowlistRejectsInvalidInput(t *testing.T) {
	if _, err := parseAllowlist("198.51.100.0/99"); err == nil {
		t.Fatalf("expected invalid CIDR to fail")
	}
}

func TestParseBroadcastModeRejectsInvalidInput(t *testing.T) {
	if _, err := parseBroadcastMode("all"); err == nil {
		t.Fatalf("expected invalid broadcast mode to fail")
	}
}

func TestParseEnforcementModeRejectsInvalidInput(t *testing.T) {
	if _, err := parseEnforcementMode("ban"); err == nil {
		t.Fatalf("expected invalid enforcement mode to fail")
	}
}

func TestBuildProvidersOnlyAddsKeyedPremiumProviders(t *testing.T) {
	providers := buildProviders(Config{}, nil)
	if len(providers) != 2 {
		t.Fatalf("expected 2 default providers, got %d", len(providers))
	}

	cfg := Config{
		IPHubAPIKey:          "hub",
		VPNAPIIoAPIKey:       "vpnapi",
		IPQualityScoreAPIKey: "ipqs",
		IPLocateAPIKey:       "iplocate",
	}
	providers = buildProviders(cfg, nil)
	if len(providers) != 6 {
		t.Fatalf("expected 6 providers when all premium keys are configured, got %d", len(providers))
	}
}
