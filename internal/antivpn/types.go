package antivpn

import (
	"context"
	"net/netip"
	"time"
)

type Mode string

const (
	ModeOff     Mode = "off"
	ModeLogOnly Mode = "log-only"
	ModeBlock   Mode = "block"
)

type BroadcastMode string

const (
	BroadcastOff          BroadcastMode = "off"
	BroadcastBlockOnly    BroadcastMode = "block-only"
	BroadcastPassAndBlock BroadcastMode = "pass-and-block"
)

type EnforcementMode string

const (
	EnforcementKickOnly  EnforcementMode = "kick-only"
	EnforcementBanAndKick EnforcementMode = "ban-and-kick"
	EnforcementBanOnly   EnforcementMode = "ban-only"
	EnforcementCustom    EnforcementMode = "custom"
)

type Signal struct {
	Provider string `json:"provider"`
	Category string `json:"category"`
	Strength string `json:"strength"`
	Reason   string `json:"reason"`
	Weight   int    `json:"weight"`
}

type ProviderResult struct {
	Provider   string   `json:"provider"`
	Success    bool     `json:"success"`
	Summary    string   `json:"summary"`
	Error      string   `json:"error,omitempty"`
	LatencyMS  int64    `json:"latency_ms,omitempty"`
	HTTPStatus int      `json:"http_status,omitempty"`
	Signals    []Signal `json:"signals,omitempty"`
}

type Decision struct {
	IP                 string           `json:"ip"`
	Mode               Mode             `json:"mode"`
	CheckedAt          time.Time        `json:"checked_at"`
	ExpiresAt          time.Time        `json:"expires_at"`
	FromCache          bool             `json:"from_cache"`
	Allowlisted        bool             `json:"allowlisted"`
	Allowed            bool             `json:"allowed"`
	Blocked            bool             `json:"blocked"`
	WouldBlock         bool             `json:"would_block"`
	Score              int              `json:"score"`
	Threshold          int              `json:"threshold"`
	Summary            string           `json:"summary"`
	Reasons            []string         `json:"reasons,omitempty"`
	ProviderCount      int              `json:"provider_count"`
	ProviderSuccesses  int              `json:"provider_successes"`
	DetectingProviders int              `json:"detecting_providers"`
	StrongSignals      int              `json:"strong_signals"`
	Degraded           bool             `json:"degraded"`
	Providers          []ProviderResult `json:"providers,omitempty"`
}

type Provider interface {
	Name() string
	Lookup(ctx context.Context, ip netip.Addr) ProviderResult
}
