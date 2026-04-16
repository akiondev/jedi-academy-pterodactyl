package antivpn

import (
	"net/netip"
	"testing"
)

func TestEvaluateDecisionAllowsWhenNoProvidersSucceed(t *testing.T) {
	cfg := Config{
		Enabled:        true,
		Mode:           ModeBlock,
		ScoreThreshold: 90,
	}

	decision := EvaluateDecision(netip.MustParseAddr("198.51.100.10"), cfg, []ProviderResult{
		{Provider: "proxycheck.io", Error: "timeout"},
		{Provider: "ipapi.is", Error: "http 500"},
	})

	if decision.Blocked {
		t.Fatalf("expected decision to stay allowed when all providers fail")
	}
	if !decision.Allowed {
		t.Fatalf("expected decision to remain allowed")
	}
	if !decision.Degraded {
		t.Fatalf("expected degraded=true when providers fail")
	}
}

func TestEvaluateDecisionRequiresConfidenceForBlock(t *testing.T) {
	cfg := Config{
		Enabled:        true,
		Mode:           ModeBlock,
		ScoreThreshold: 90,
	}

	decision := EvaluateDecision(netip.MustParseAddr("198.51.100.11"), cfg, []ProviderResult{
		{
			Provider: "proxycheck.io",
			Success:  true,
			Signals: []Signal{
				{Provider: "proxycheck.io", Strength: "medium", Weight: 55, Reason: "hosting-backed"},
			},
		},
	})

	if decision.Blocked {
		t.Fatalf("expected single medium signal below threshold to stay allowed")
	}
	if decision.WouldBlock {
		t.Fatalf("expected decision to avoid would-block for insufficient confidence")
	}
}

func TestEvaluateDecisionBlocksOnStrongSignalAtThreshold(t *testing.T) {
	cfg := Config{
		Enabled:        true,
		Mode:           ModeBlock,
		ScoreThreshold: 90,
	}

	decision := EvaluateDecision(netip.MustParseAddr("198.51.100.12"), cfg, []ProviderResult{
		{
			Provider: "proxycheck.io",
			Success:  true,
			Signals: []Signal{
				{Provider: "proxycheck.io", Strength: "strong", Weight: 80, Reason: "vpn"},
				{Provider: "proxycheck.io", Strength: "medium", Weight: 15, Reason: "risk"},
			},
		},
	})

	if !decision.Blocked {
		t.Fatalf("expected decision to block when a strong signal clears threshold")
	}
	if decision.Score != 95 {
		t.Fatalf("expected score 95, got %d", decision.Score)
	}
}

func TestEvaluateDecisionLogOnlyStillMarksWouldBlock(t *testing.T) {
	cfg := Config{
		Enabled:        true,
		Mode:           ModeLogOnly,
		ScoreThreshold: 90,
	}

	decision := EvaluateDecision(netip.MustParseAddr("198.51.100.13"), cfg, []ProviderResult{
		{
			Provider: "proxycheck.io",
			Success:  true,
			Signals: []Signal{
				{Provider: "proxycheck.io", Strength: "medium", Weight: 55, Reason: "hosting-backed"},
			},
		},
		{
			Provider: "IPHub",
			Success:  true,
			Signals: []Signal{
				{Provider: "IPHub", Strength: "medium", Weight: 40, Reason: "non-residential"},
			},
		},
	})

	if decision.Blocked {
		t.Fatalf("expected log-only mode to avoid hard blocking")
	}
	if !decision.WouldBlock {
		t.Fatalf("expected decision to be marked would-block in log-only mode")
	}
}

func TestProviderStatusSummary(t *testing.T) {
	summary := ProviderStatusSummary([]ProviderResult{
		{Provider: "proxycheck.io", Success: true, Summary: "vpn=service", Signals: []Signal{{Provider: "proxycheck.io", Weight: 80}}},
		{Provider: "IPHub", Error: "http 429", Summary: "lookup failed"},
	})

	if len(summary) != 2 {
		t.Fatalf("expected 2 provider summaries, got %d", len(summary))
	}
	if summary[0] != "proxycheck.io=signal (vpn=service)" {
		t.Fatalf("unexpected first provider summary: %q", summary[0])
	}
	if summary[1] != "IPHub=error:http 429 (lookup failed)" {
		t.Fatalf("unexpected second provider summary: %q", summary[1])
	}
}
