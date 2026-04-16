package antivpn

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	cfg       Config
	logger    *slog.Logger
	cache     *Cache
	providers []Provider
	flights   flightGroup
}

func NewEngine(cfg Config, logger *slog.Logger) (*Engine, error) {
	cache, err := NewCache(cfg.CachePath, cfg.CacheFlushInterval, logger)
	if err != nil {
		return nil, err
	}

	return &Engine{
		cfg:       cfg,
		logger:    logger,
		cache:     cache,
		providers: buildProviders(cfg, logger),
	}, nil
}

func (e *Engine) Close() error {
	if e == nil || e.cache == nil {
		return nil
	}
	return e.cache.Close()
}

func (e *Engine) CheckIP(ctx context.Context, ip netip.Addr) (Decision, error) {
	now := time.Now().UTC()
	mode := e.cfg.EffectiveMode()

	if !ip.IsValid() {
		return Decision{}, fmt.Errorf("invalid IP address")
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return Decision{
			IP:        ip.String(),
			Mode:      mode,
			CheckedAt: now,
			Allowed:   true,
			Summary:   "allowed because the address is local/private",
		}, nil
	}
	if e.cfg.IsAllowlisted(ip) {
		return Decision{
			IP:          ip.String(),
			Mode:        mode,
			CheckedAt:   now,
			Allowlisted: true,
			Allowed:     true,
			Summary:     "allowed because the address matched the anti-vpn allowlist",
		}, nil
	}
	if decision, ok := e.cache.Get(ip.String()); ok {
		return decision, nil
	}

	return e.flights.Do(ip.String(), func() (Decision, error) {
		if decision, ok := e.cache.Get(ip.String()); ok {
			return decision, nil
		}

		results := e.queryProviders(ctx, ip)
		decision := EvaluateDecision(ip, e.cfg, results)
		decision.ExpiresAt = decision.CheckedAt.Add(e.cfg.CacheTTL)
		e.cache.Set(decision)
		return decision, nil
	})
}

func (e *Engine) queryProviders(ctx context.Context, ip netip.Addr) []ProviderResult {
	results := make([]ProviderResult, len(e.providers))
	var wg sync.WaitGroup

	for index, provider := range e.providers {
		wg.Add(1)
		go func(i int, current Provider) {
			defer wg.Done()
			results[i] = current.Lookup(ctx, ip)
		}(index, provider)
	}

	wg.Wait()
	return results
}

func EvaluateDecision(ip netip.Addr, cfg Config, results []ProviderResult) Decision {
	mode := cfg.EffectiveMode()
	decision := Decision{
		IP:            ip.String(),
		Mode:          mode,
		CheckedAt:     time.Now().UTC(),
		Threshold:     cfg.ScoreThreshold,
		ProviderCount: len(results),
		Providers:     results,
		Allowed:       true,
	}

	reasons := make([]string, 0, 8)
	score := 0
	successes := 0
	detectingProviders := 0
	strongSignals := 0

	for _, result := range results {
		if result.Success {
			successes++
		}
		if len(result.Signals) > 0 {
			detectingProviders++
		}
		if result.Error != "" {
			decision.Degraded = true
		}
		for _, signal := range result.Signals {
			score += signal.Weight
			reasons = append(reasons, fmt.Sprintf("%s (+%d): %s", signal.Provider, signal.Weight, signal.Reason))
			if signal.Strength == "strong" {
				strongSignals++
			}
		}
	}

	decision.Score = score
	decision.Reasons = reasons
	decision.ProviderSuccesses = successes
	decision.DetectingProviders = detectingProviders
	decision.StrongSignals = strongSignals
	if successes < len(results) {
		decision.Degraded = true
	}

	if successes == 0 {
		decision.Summary = "allowed because no provider returned usable data"
		return decision
	}
	if detectingProviders == 0 {
		decision.Summary = "allowed because no provider reported VPN or hosting-backed signals"
		return decision
	}

	blockCandidate := score >= cfg.ScoreThreshold && (strongSignals > 0 || detectingProviders >= 2)
	decision.WouldBlock = blockCandidate

	switch mode {
	case ModeBlock:
		decision.Blocked = blockCandidate
		decision.Allowed = !blockCandidate
	case ModeLogOnly:
		decision.Blocked = false
		decision.Allowed = true
	default:
		decision.Blocked = false
		decision.Allowed = true
	}

	if decision.Blocked {
		decision.Summary = fmt.Sprintf("blocked because score %d met threshold %d", score, cfg.ScoreThreshold)
		return decision
	}
	if mode == ModeLogOnly && blockCandidate {
		decision.Summary = fmt.Sprintf("log-only mode: score %d met threshold %d", score, cfg.ScoreThreshold)
		return decision
	}

	detail := "below threshold"
	if !blockCandidate {
		detail = "insufficient confidence"
	}
	decision.Summary = fmt.Sprintf("allowed because score %d/%d remained %s", score, cfg.ScoreThreshold, detail)
	return decision
}

type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

type flightCall struct {
	wg       sync.WaitGroup
	decision Decision
	err      error
}

func (g *flightGroup) Do(key string, fn func() (Decision, error)) (Decision, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]*flightCall)
	}
	if existing, ok := g.calls[key]; ok {
		g.mu.Unlock()
		existing.wg.Wait()
		return existing.decision, existing.err
	}

	call := &flightCall{}
	call.wg.Add(1)
	g.calls[key] = call
	g.mu.Unlock()

	call.decision, call.err = fn()
	call.wg.Done()

	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()

	return call.decision, call.err
}

func DecisionLogFields(decision Decision) []any {
	reasons := decision.Reasons
	if len(reasons) == 0 {
		reasons = []string{"no actionable signals"}
	}
	return []any{
		"ip", decision.IP,
		"mode", decision.Mode,
		"from_cache", decision.FromCache,
		"allowed", decision.Allowed,
		"blocked", decision.Blocked,
		"would_block", decision.WouldBlock,
		"score", decision.Score,
		"threshold", decision.Threshold,
		"providers_ok", decision.ProviderSuccesses,
		"providers_total", decision.ProviderCount,
		"provider_statuses", strings.Join(ProviderStatusSummary(decision.Providers), "; "),
		"summary", decision.Summary,
		"reasons", strings.Join(reasons, "; "),
	}
}

func ProviderStatusSummary(results []ProviderResult) []string {
	summaries := make([]string, 0, len(results))

	for _, result := range results {
		status := "no-signal"
		switch {
		case result.Error != "":
			status = "error:" + result.Error
		case !result.Success:
			status = "unusable"
		case len(result.Signals) > 0:
			status = "signal"
		}

		if result.Summary != "" {
			status += " (" + result.Summary + ")"
		}
		summaries = append(summaries, result.Provider+"="+status)
	}

	return summaries
}
