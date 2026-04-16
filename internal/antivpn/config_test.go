package antivpn

import (
	"net/netip"
	"testing"
)

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
