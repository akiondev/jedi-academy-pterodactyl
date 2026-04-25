package antivpn

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// jsonRuntimeConfig mirrors the subset of /home/container/config/jka-runtime.json
// that the Go supervisor cares about. Unknown keys are ignored so the
// shell layer remains the canonical schema owner.
type jsonRuntimeConfig struct {
	Server struct {
		LogFilename               string `json:"log_filename"`
		FsGame                    string `json:"fs_game"`
		PortFallback              int    `json:"port_fallback"`
		SyncManagedTaystJKPayload *bool  `json:"sync_managed_taystjk_payload"`
	} `json:"server"`
	Supervisor struct {
		Enabled                 *bool `json:"enabled"`
		DebugStartup            *bool `json:"debug_startup"`
		LiveOutputMirrorEnabled *bool `json:"live_output_mirror_enabled"`
	} `json:"supervisor"`
	AntiVPN struct {
		Enabled            *bool    `json:"enabled"`
		Mode               string   `json:"mode"`
		ScoreThreshold     *int     `json:"score_threshold"`
		Allowlist          []string `json:"allowlist"`
		TimeoutMS          *int     `json:"timeout_ms"`
		CacheTTL           string   `json:"cache_ttl"`
		CacheFlushInterval string   `json:"cache_flush_interval"`
		AuditLogPath       string   `json:"audit_log_path"`
		AuditAllow         *bool    `json:"audit_allow"`
		LogDecisions       *bool    `json:"log_decisions"`
		Providers          struct {
			ProxycheckAPIKey     string `json:"proxycheck_api_key"`
			IPApiIsAPIKey        string `json:"ipapiis_api_key"`
			IPHubAPIKey          string `json:"iphub_api_key"`
			VpnApiIoAPIKey       string `json:"vpnapi_io_api_key"`
			IPQualityScoreAPIKey string `json:"ipqualityscore_api_key"`
			IPLocateAPIKey       string `json:"iplocate_api_key"`
		} `json:"providers"`
		Broadcast struct {
			Mode          string `json:"mode"`
			Cooldown      string `json:"cooldown"`
			PassTemplate  string `json:"pass_template"`
			BlockTemplate string `json:"block_template"`
		} `json:"broadcast"`
		Enforcement struct {
			Mode        string `json:"mode"`
			KickCommand string `json:"kick_command"`
			BanCommand  string `json:"ban_command"`
		} `json:"enforcement"`
	} `json:"anti_vpn"`
	RconGuard struct {
		Enabled     *bool    `json:"enabled"`
		Action      string   `json:"action"`
		Broadcast   *bool    `json:"broadcast"`
		IgnoreHosts []string `json:"ignore_hosts"`
	} `json:"rcon_guard"`
	Addons struct {
		Enabled        *bool  `json:"enabled"`
		Directory      string `json:"directory"`
		Strict         *bool  `json:"strict"`
		TimeoutSeconds *int   `json:"timeout_seconds"`
		LogOutput      *bool  `json:"log_output"`
		EventBus       struct {
			Enabled    *bool  `json:"enabled"`
			BufferSize *int   `json:"buffer_size"`
			DropPolicy string `json:"drop_policy"`
		} `json:"event_bus"`
	} `json:"addons"`
}

// runtimeConfigPath returns the path to the JSON runtime config the
// supervisor consumes. The path can be overridden via
// JKA_RUNTIME_CONFIG_PATH for testing.
func runtimeConfigPath() string {
	if p := strings.TrimSpace(os.Getenv("JKA_RUNTIME_CONFIG_PATH")); p != "" {
		return p
	}
	return "/home/container/config/jka-runtime.json"
}

// loadRuntimeJSONConfig reads the JSON runtime config from
// runtimeConfigPath and returns the parsed structure. A missing file
// is reported as (nil, nil) so callers can fall back to env defaults
// without surfacing an error to the operator.
func loadRuntimeJSONConfig() (*jsonRuntimeConfig, error) {
	path := runtimeConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read runtime config %q: %w", path, err)
	}
	cfg := &jsonRuntimeConfig{}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse runtime config %q: %w", path, err)
	}
	return cfg, nil
}

// applyJSONOverrides applies non-empty values from the JSON runtime
// config on top of an env-derived Config. Empty strings, nil pointers,
// and zero-valued integers in the JSON are treated as "not set" and
// leave the env-derived value in place.
//
// The function never logs any of the provider API keys; callers
// inspect them only through the resulting Config struct.
func applyJSONOverrides(cfg Config, jsonCfg *jsonRuntimeConfig) (Config, error) {
	if jsonCfg == nil {
		return cfg, nil
	}

	// Anti-VPN top-level fields.
	if jsonCfg.AntiVPN.Enabled != nil {
		cfg.Enabled = *jsonCfg.AntiVPN.Enabled
	}
	if jsonCfg.AntiVPN.ScoreThreshold != nil && *jsonCfg.AntiVPN.ScoreThreshold > 0 {
		cfg.ScoreThreshold = *jsonCfg.AntiVPN.ScoreThreshold
	}
	if jsonCfg.AntiVPN.TimeoutMS != nil && *jsonCfg.AntiVPN.TimeoutMS > 0 {
		cfg.Timeout = time.Duration(*jsonCfg.AntiVPN.TimeoutMS) * time.Millisecond
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.CacheTTL); v != "" {
		if d, err := parseDurationOrSeconds(v); err == nil {
			cfg.CacheTTL = d
		}
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.CacheFlushInterval); v != "" {
		if d, err := parseDurationOrSeconds(v); err == nil {
			cfg.CacheFlushInterval = d
		}
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.AuditLogPath); v != "" {
		cfg.AuditLogPath = v
	}
	if jsonCfg.AntiVPN.AuditAllow != nil {
		cfg.AuditAllow = *jsonCfg.AntiVPN.AuditAllow
	}
	if jsonCfg.AntiVPN.LogDecisions != nil {
		cfg.LogDecisions = *jsonCfg.AntiVPN.LogDecisions
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Mode); v != "" {
		mode, err := parseMode(v)
		if err != nil {
			return cfg, err
		}
		cfg.Mode = mode
	}

	// Provider keys: the JSON value wins when non-empty, otherwise the
	// env-derived value is preserved.
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Providers.ProxycheckAPIKey); v != "" {
		cfg.ProxyCheckAPIKey = v
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Providers.IPApiIsAPIKey); v != "" {
		cfg.IPAPIISAPIKey = v
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Providers.IPHubAPIKey); v != "" {
		cfg.IPHubAPIKey = v
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Providers.VpnApiIoAPIKey); v != "" {
		cfg.VPNAPIIoAPIKey = v
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Providers.IPQualityScoreAPIKey); v != "" {
		cfg.IPQualityScoreAPIKey = v
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Providers.IPLocateAPIKey); v != "" {
		cfg.IPLocateAPIKey = v
	}

	// Allowlist: JSON list overrides env when present (even when
	// empty, an explicit empty list resets the allowlist).
	if jsonCfg.AntiVPN.Allowlist != nil {
		joined := strings.Join(jsonCfg.AntiVPN.Allowlist, ",")
		allowlist, err := parseAllowlist(joined)
		if err != nil {
			return cfg, err
		}
		cfg.Allowlist = allowlist
	}

	// Broadcast.
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Broadcast.Mode); v != "" {
		mode, err := parseBroadcastMode(v)
		if err != nil {
			return cfg, err
		}
		cfg.BroadcastMode = mode
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Broadcast.Cooldown); v != "" {
		if d, err := parseDurationOrSeconds(v); err == nil {
			cfg.BroadcastCooldown = d
		}
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Broadcast.PassTemplate); v != "" {
		cfg.BroadcastPassCommand = v
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Broadcast.BlockTemplate); v != "" {
		cfg.BroadcastBlockCommand = v
	}

	// Enforcement.
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Enforcement.Mode); v != "" {
		mode, err := parseEnforcementMode(v)
		if err != nil {
			return cfg, err
		}
		cfg.EnforcementMode = mode
	}
	if v := strings.TrimSpace(jsonCfg.AntiVPN.Enforcement.KickCommand); v != "" {
		cfg.KickCommand = v
	}
	cfg.BanCommand = strings.TrimSpace(jsonCfg.AntiVPN.Enforcement.BanCommand)

	// RCON guard.
	if jsonCfg.RconGuard.Enabled != nil {
		cfg.RconGuard.Enabled = *jsonCfg.RconGuard.Enabled
	}
	if v := strings.TrimSpace(jsonCfg.RconGuard.Action); v != "" {
		cfg.RconGuard.Action = strings.ToLower(v)
	}
	if jsonCfg.RconGuard.Broadcast != nil {
		cfg.RconGuard.Broadcast = *jsonCfg.RconGuard.Broadcast
	}
	if jsonCfg.RconGuard.IgnoreHosts != nil {
		cfg.RconGuard.IgnoreHosts = parseRconGuardIgnoreHosts(strings.Join(jsonCfg.RconGuard.IgnoreHosts, ","))
	}

	// Addons / event bus.
	if jsonCfg.Addons.EventBus.Enabled != nil {
		cfg.AddonRunner.Enabled = *jsonCfg.Addons.EventBus.Enabled
	}
	if jsonCfg.Addons.EventBus.BufferSize != nil && *jsonCfg.Addons.EventBus.BufferSize > 0 {
		cfg.AddonRunner.BufferSize = *jsonCfg.Addons.EventBus.BufferSize
	}
	if v := strings.TrimSpace(jsonCfg.Addons.EventBus.DropPolicy); v != "" {
		cfg.AddonRunner.DropPolicy = ParseEventDispatchPolicy(v)
	}

	// Supervisor live-output mirror flag.
	if jsonCfg.Supervisor.LiveOutputMirrorEnabled != nil {
		cfg.LiveOutputEnabled = *jsonCfg.Supervisor.LiveOutputMirrorEnabled
	}

	return cfg, nil
}

// parseDurationOrSeconds accepts either a Go duration literal (e.g.
// "6h", "2s") or a bare integer interpreted as seconds. The helper
// exists because the JSON schema documents both forms in the
// operator-facing example template.
func parseDurationOrSeconds(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if d, err := time.ParseDuration(value); err == nil {
		return d, nil
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}
	return 0, fmt.Errorf("parse duration %q: not a Go duration literal or integer seconds", value)
}
