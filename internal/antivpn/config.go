package antivpn

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Enabled               bool
	Mode                  Mode
	BroadcastMode         BroadcastMode
	EnforcementMode       EnforcementMode
	CacheTTL              time.Duration
	CacheFlushInterval    time.Duration
	ScoreThreshold        int
	Allowlist             []netip.Prefix
	ProxyCheckAPIKey      string
	IPAPIISAPIKey         string
	IPHubAPIKey           string
	VPNAPIIoAPIKey        string
	IPQualityScoreAPIKey  string
	IPLocateAPIKey        string
	Timeout               time.Duration
	LogDecisions          bool
	CachePath             string
	AuditLogPath          string
	LogPath               string
	RetryCount            int
	ProviderMinInterval   time.Duration
	LogPollInterval       time.Duration
	BroadcastCooldown     time.Duration
	EventDedupeInterval   time.Duration
	BanCommand            string
	KickCommand           string
	BroadcastPassCommand  string
	BroadcastBlockCommand string
	// LiveOutputPath is the runtime-managed file the supervisor mirrors
	// every stdout/stderr line into. Addons consume this file via
	// `tail -F` (or any line-oriented reader) instead of tailing the
	// engine-written server.log. An empty value disables the mirror.
	LiveOutputPath string
	// LiveOutputMaxBytes is the soft size cap for the live mirror file.
	// When the file grows past this size the supervisor archives the
	// current file (gzip + timestamped suffix) and reopens a fresh one.
	// Zero or negative disables size-based rotation.
	LiveOutputMaxBytes int64
	// LiveOutputKeepArchives is the maximum number of gzipped archives
	// retained for the live mirror file. Older archives are pruned in
	// modification-time order.
	LiveOutputKeepArchives int
	// AuditLogMaxBytes is the soft size cap for anti-vpn-audit.log.
	// When the file grows past this size the supervisor archives the
	// current file (gzip + timestamped suffix) and reopens a fresh one.
	// Zero or negative disables size-based rotation.
	AuditLogMaxBytes int64
	// AuditLogKeepArchives is the maximum number of gzipped archives
	// retained for anti-vpn-audit.log. Older archives are pruned in
	// modification-time order.
	AuditLogKeepArchives int
	// RotateLogsOnStart, when true, archives any pre-existing non-empty
	// audit log and live mirror file at supervisor startup so each run
	// gets its own retrievable history file. Default true.
	RotateLogsOnStart bool
	// BroadcastEmissionSpacing is the minimum delay between successive
	// broadcast (`say`) commands written to the engine. JKA's per-frame
	// command buffer can truncate `say` payloads when several broadcasts
	// arrive in the same tick (observed in real game logs as
	// "VPN PASS: ... cleared checks (10/" with the rest cut off). Spacing
	// the emissions guarantees the engine processes one `say` per frame.
	// Zero disables spacing.
	BroadcastEmissionSpacing time.Duration
	// LogMonitorEnabled gates the legacy `server.log` tailing fallback.
	// In the new process-output-only architecture the supervisor reads
	// the dedicated server's stdout/stderr exactly once and parses events
	// from that single stream, so the file-based fallback is OFF by
	// default. Set ANTI_VPN_LOG_MONITOR_ENABLED=true to re-enable the
	// legacy debug fallback for environments where stdout capture is
	// unreliable.
	LogMonitorEnabled bool
	// LiveOutputEnabled gates the runtime-managed live mirror file. The
	// mirror is now off by default; addons consume parsed events from
	// the supervisor's event bus instead of tailing a file. Operators
	// that want the file as an explicit debug/export feature can set
	// JKA_LIVE_OUTPUT_MIRROR_ENABLED=true (legacy alias:
	// TAYSTJK_LIVE_OUTPUT_ENABLED).
	LiveOutputEnabled bool
	// AuditAllow controls whether plain `allow` decisions are written
	// to the audit log. Default false: only block / would-block /
	// degraded / error decisions are audited so a normal busy server
	// does not produce thousands of allow rows. Set
	// ANTI_VPN_AUDIT_ALLOW=true for forensic / debug runs.
	AuditAllow bool
	// RconGuard holds the configuration for the built-in RCON guard
	// module. The guard consumes `Bad rcon from ...` events parsed
	// directly from process stdout/stderr and uses the central
	// connection tracker to decide whether the source IP maps to a
	// currently connected slot.
	RconGuard RconGuardConfig
	// AddonRunner holds the configuration for the supervisor's
	// event-driven addon runner. The runner is the bridge between the
	// central event dispatcher and external addon child processes; it
	// receives parsed events and writes them as NDJSON to each addon's
	// stdin so addons no longer need to tail server.log or any
	// supervisor-managed mirror file.
	AddonRunner AddonRunnerConfig
}

// RconGuardConfig holds the configuration for the supervisor's built-in
// RCON guard module. The guard replaces the legacy
// 50-rcon-live-guard.py addon that used to tail the live-output mirror
// file. Because it consumes events from the same stream the supervisor
// already scans, it cannot loop on its own RCON commands and never
// lies about whether a player was actually kicked.
type RconGuardConfig struct {
	Enabled     bool
	Action      string
	Broadcast   bool
	IgnoreHosts []string
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		Enabled:              envBool("ANTI_VPN_ENABLED", false),
		CacheTTL:             envDuration("ANTI_VPN_CACHE_TTL", 6*time.Hour),
		CacheFlushInterval:   envDuration("ANTI_VPN_CACHE_FLUSH_INTERVAL", 2*time.Second),
		ScoreThreshold:       envInt("ANTI_VPN_SCORE_THRESHOLD", 90),
		ProxyCheckAPIKey:     strings.TrimSpace(os.Getenv("ANTI_VPN_PROXYCHECK_API_KEY")),
		IPAPIISAPIKey:        strings.TrimSpace(os.Getenv("ANTI_VPN_IPAPIIS_API_KEY")),
		IPHubAPIKey:          strings.TrimSpace(os.Getenv("ANTI_VPN_IPHUB_API_KEY")),
		VPNAPIIoAPIKey:       strings.TrimSpace(os.Getenv("ANTI_VPN_VPNAPI_IO_API_KEY")),
		IPQualityScoreAPIKey: strings.TrimSpace(os.Getenv("ANTI_VPN_IPQUALITYSCORE_API_KEY")),
		IPLocateAPIKey:       strings.TrimSpace(os.Getenv("ANTI_VPN_IPLOCATE_API_KEY")),
		Timeout:              envDurationOrMilliseconds("ANTI_VPN_TIMEOUT_MS", 1500*time.Millisecond),
		LogDecisions:         envBool("ANTI_VPN_LOG_DECISIONS", true),
		CachePath:            envString("ANTI_VPN_CACHE_PATH", "/home/container/.cache/taystjk-antivpn/cache.json"),
		AuditLogPath:         envString("ANTI_VPN_AUDIT_LOG_PATH", "/home/container/logs/anti-vpn-audit.log"),
		LogPath:              envString("ANTI_VPN_LOG_PATH", defaultLogPath()),
		RetryCount:           envInt("ANTI_VPN_RETRY_COUNT", 1),
		ProviderMinInterval:  envDuration("ANTI_VPN_PROVIDER_MIN_INTERVAL", 250*time.Millisecond),
		LogPollInterval:      envDuration("ANTI_VPN_LOG_POLL_INTERVAL", 750*time.Millisecond),
		BroadcastCooldown:    envDuration("ANTI_VPN_BROADCAST_COOLDOWN", 90*time.Second),
		EventDedupeInterval:  envDuration("ANTI_VPN_EVENT_DEDUPE_INTERVAL", 90*time.Second),
		BanCommand:           envString("ANTI_VPN_BAN_COMMAND", ""),
		KickCommand:          envString("ANTI_VPN_KICK_COMMAND", "clientkick %SLOT%"),
		BroadcastPassCommand: envString("ANTI_VPN_BROADCAST_PASS_TEMPLATE", `say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%`),
		BroadcastBlockCommand: envString("ANTI_VPN_BROADCAST_BLOCK_TEMPLATE", `say [Anti-VPN] VPN BLOCKED: %PLAYER% triggered anti-VPN (%SCORE%/%THRESHOLD%). %SUMMARY%`),
		LiveOutputPath:         envString("TAYSTJK_LIVE_OUTPUT_PATH", "/home/container/.runtime/live/server-output.log"),
		LiveOutputMaxBytes:     int64(envInt("TAYSTJK_LIVE_OUTPUT_MAX_BYTES", 10*1024*1024)),
		LiveOutputKeepArchives: envInt("TAYSTJK_LIVE_OUTPUT_KEEP_ARCHIVES", 5),
		AuditLogMaxBytes:       int64(envInt("ANTI_VPN_AUDIT_LOG_MAX_BYTES", 10*1024*1024)),
		AuditLogKeepArchives:   envInt("ANTI_VPN_AUDIT_LOG_KEEP_ARCHIVES", 7),
		RotateLogsOnStart:      envBool("ANTI_VPN_ROTATE_LOGS_ON_START", true),
		BroadcastEmissionSpacing: envDuration("ANTI_VPN_BROADCAST_EMISSION_SPACING", 350*time.Millisecond),
		// New process-output-only architecture: the legacy server.log
		// tailer and the live-output mirror are OFF by default. They
		// remain available as opt-in debug fallbacks so existing
		// deployments that intentionally rely on them can re-enable
		// them, but the supervisor never reads server.log nor mirrors
		// to a file unless explicitly told to.
		LogMonitorEnabled: envBool("ANTI_VPN_LOG_MONITOR_ENABLED", false),
		AuditAllow:        envBool("ANTI_VPN_AUDIT_ALLOW", false),
	}

	// JKA_LIVE_OUTPUT_MIRROR_ENABLED is the canonical env var for the
	// live-output mirror; the legacy TAYSTJK_LIVE_OUTPUT_ENABLED name is
	// accepted as a deprecated alias when the canonical variable is not
	// set.
	cfg.LiveOutputEnabled = envBoolWithFallback("JKA_LIVE_OUTPUT_MIRROR_ENABLED", "TAYSTJK_LIVE_OUTPUT_ENABLED", false)

	// Built-in RCON guard module. Replaces the legacy
	// 50-rcon-live-guard.py addon that tailed the live-output file.
	cfg.RconGuard = RconGuardConfig{
		Enabled:     envBool("RCON_GUARD_ENABLED", true),
		Action:      strings.ToLower(envString("RCON_GUARD_ACTION", "kick")),
		Broadcast:   envBool("RCON_GUARD_BROADCAST", true),
		IgnoreHosts: parseRconGuardIgnoreHosts(envString("RCON_GUARD_IGNORE_HOSTS", "127.0.0.1,::1,localhost")),
	}

	// Event-driven addon runner. Defaults are tuned so a fresh install
	// with no addon directory present is a no-op (Enabled=true but
	// Start() returns gracefully when the directory is missing).
	cfg.AddonRunner = AddonRunnerConfig{
		Enabled:    envBool("ADDON_EVENT_BUS_ENABLED", true),
		AddonsDir:  envString("ADDON_EVENT_ADDONS_DIR", "/home/container/addons/events"),
		BufferSize: envInt("ADDON_EVENT_BUS_BUFFER_SIZE", 1000),
		DropPolicy: ParseEventDispatchPolicy(envString("ADDON_EVENT_BUS_DROP_POLICY", "drop-oldest")),
	}

	mode, err := parseMode(envString("ANTI_VPN_MODE", string(ModeBlock)))
	if err != nil {
		return Config{}, err
	}
	cfg.Mode = mode

	enforcementMode, err := parseEnforcementMode(envString("ANTI_VPN_ENFORCEMENT_MODE", string(EnforcementKickOnly)))
	if err != nil {
		return Config{}, err
	}
	cfg.EnforcementMode = enforcementMode

	broadcastMode, err := parseBroadcastMode(envString("ANTI_VPN_BROADCAST_MODE", string(BroadcastBlockOnly)))
	if err != nil {
		return Config{}, err
	}
	cfg.BroadcastMode = broadcastMode

	allowlist, err := parseAllowlist(os.Getenv("ANTI_VPN_ALLOWLIST"))
	if err != nil {
		return Config{}, err
	}
	cfg.Allowlist = allowlist

	// Apply /home/container/config/jka-runtime.json on top of the
	// env-derived configuration. The JSON file is the canonical
	// source of truth in the manual-first egg model; env values
	// remain as a fallback for backwards compatibility.
	jsonCfg, err := loadRuntimeJSONConfig()
	if err != nil {
		return Config{}, err
	}
	cfg, err = applyJSONOverrides(cfg, jsonCfg)
	if err != nil {
		return Config{}, err
	}

	if cfg.ScoreThreshold < 1 || cfg.ScoreThreshold > 200 {
		return Config{}, fmt.Errorf("ANTI_VPN_SCORE_THRESHOLD must be between 1 and 200")
	}
	if cfg.CacheTTL <= 0 {
		return Config{}, fmt.Errorf("ANTI_VPN_CACHE_TTL must be greater than zero")
	}
	if cfg.CacheFlushInterval <= 0 {
		return Config{}, fmt.Errorf("ANTI_VPN_CACHE_FLUSH_INTERVAL must be greater than zero")
	}
	if cfg.Timeout <= 0 {
		return Config{}, fmt.Errorf("ANTI_VPN_TIMEOUT_MS must be greater than zero")
	}
	if cfg.BroadcastCooldown < 0 {
		return Config{}, fmt.Errorf("ANTI_VPN_BROADCAST_COOLDOWN must be zero or greater")
	}
	if cfg.EventDedupeInterval <= 0 {
		return Config{}, fmt.Errorf("ANTI_VPN_EVENT_DEDUPE_INTERVAL must be greater than zero")
	}
	if cfg.RetryCount < 0 || cfg.RetryCount > 5 {
		return Config{}, fmt.Errorf("ANTI_VPN_RETRY_COUNT must be between 0 and 5")
	}

	cfg.CachePath = filepath.Clean(cfg.CachePath)
	cfg.AuditLogPath = filepath.Clean(cfg.AuditLogPath)
	cfg.LogPath = filepath.Clean(cfg.LogPath)
	if strings.TrimSpace(cfg.LiveOutputPath) != "" {
		cfg.LiveOutputPath = filepath.Clean(cfg.LiveOutputPath)
	}
	if cfg.LiveOutputMaxBytes < 0 {
		cfg.LiveOutputMaxBytes = 0
	}
	if cfg.LiveOutputKeepArchives < 0 {
		cfg.LiveOutputKeepArchives = 0
	}
	if cfg.AuditLogMaxBytes < 0 {
		cfg.AuditLogMaxBytes = 0
	}
	if cfg.AuditLogKeepArchives < 0 {
		cfg.AuditLogKeepArchives = 0
	}
	if cfg.BroadcastEmissionSpacing < 0 {
		cfg.BroadcastEmissionSpacing = 0
	}

	return cfg, nil
}

func (c Config) EffectiveMode() Mode {
	if !c.Enabled || c.Mode == ModeOff {
		return ModeOff
	}
	return c.Mode
}

func (c Config) ProviderKeysConfigured() int {
	count := 0
	if c.ProxyCheckAPIKey != "" {
		count++
	}
	if c.IPAPIISAPIKey != "" {
		count++
	}
	if c.IPHubAPIKey != "" {
		count++
	}
	if c.VPNAPIIoAPIKey != "" {
		count++
	}
	if c.IPQualityScoreAPIKey != "" {
		count++
	}
	if c.IPLocateAPIKey != "" {
		count++
	}
	return count
}

func (c Config) IsAllowlisted(addr netip.Addr) bool {
	for _, prefix := range c.Allowlist {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func defaultLogPath() string {
	if runtimeLogPath := strings.TrimSpace(os.Getenv("TAYSTJK_ACTIVE_SERVER_LOG_PATH")); runtimeLogPath != "" {
		return filepath.Clean(runtimeLogPath)
	}
	mod := strings.TrimSpace(os.Getenv("FS_GAME_MOD"))
	if mod == "" || strings.EqualFold(mod, "base") {
		mod = "base"
	}
	return filepath.Clean(filepath.Join("/home/container", mod, "server.log"))
}

func parseMode(value string) (Mode, error) {
	switch Mode(strings.TrimSpace(strings.ToLower(value))) {
	case ModeOff:
		return ModeOff, nil
	case ModeLogOnly:
		return ModeLogOnly, nil
	case ModeBlock:
		return ModeBlock, nil
	default:
		return "", fmt.Errorf("ANTI_VPN_MODE must be one of off, log-only, block")
	}
}

func parseBroadcastMode(value string) (BroadcastMode, error) {
	switch BroadcastMode(strings.TrimSpace(strings.ToLower(value))) {
	case BroadcastOff:
		return BroadcastOff, nil
	case BroadcastBlockOnly:
		return BroadcastBlockOnly, nil
	case BroadcastPassAndBlock:
		return BroadcastPassAndBlock, nil
	default:
		return "", fmt.Errorf("ANTI_VPN_BROADCAST_MODE must be one of off, block-only, pass-and-block")
	}
}

func parseEnforcementMode(value string) (EnforcementMode, error) {
	switch EnforcementMode(strings.TrimSpace(strings.ToLower(value))) {
	case EnforcementKickOnly:
		return EnforcementKickOnly, nil
	case EnforcementBanAndKick:
		return EnforcementBanAndKick, nil
	case EnforcementBanOnly:
		return EnforcementBanOnly, nil
	case EnforcementCustom:
		return EnforcementCustom, nil
	default:
		return "", fmt.Errorf("ANTI_VPN_ENFORCEMENT_MODE must be one of kick-only, ban-and-kick, ban-only, custom")
	}
}

func parseAllowlist(value string) ([]netip.Prefix, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})

	prefixes := make([]netip.Prefix, 0, len(fields))
	for _, item := range fields {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			prefix, err := netip.ParsePrefix(item)
			if err != nil {
				return nil, fmt.Errorf("parse ANTI_VPN_ALLOWLIST prefix %q: %w", item, err)
			}
			prefixes = append(prefixes, prefix.Masked())
			continue
		}

		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("parse ANTI_VPN_ALLOWLIST IP %q: %w", item, err)
		}
		bits := 32
		if addr.Is6() {
			bits = 128
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr, bits))
	}

	return prefixes, nil
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// envBoolWithFallback returns the parsed boolean value of `primaryKey`. If
// `primaryKey` is unset, the value of `legacyKey` is consulted instead.
// Used to keep deprecated environment variable names working while the
// canonical name takes precedence when both are set.
func envBoolWithFallback(primaryKey, legacyKey string, fallback bool) bool {
	if value := strings.TrimSpace(os.Getenv(primaryKey)); value != "" {
		return envBool(primaryKey, fallback)
	}
	return envBool(legacyKey, fallback)
}

// parseRconGuardIgnoreHosts splits a comma/whitespace separated list of
// hostnames or IP literals into a normalised slice. Entries are
// lower-cased and trimmed; empty entries are dropped. The result is
// always non-nil but may be empty.
func parseRconGuardIgnoreHosts(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	out := make([]string, 0, len(fields))
	for _, item := range fields {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	if parsed, err := time.ParseDuration(value); err == nil {
		return parsed
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func envDurationOrMilliseconds(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	if parsed, err := strconv.Atoi(value); err == nil {
		return time.Duration(parsed) * time.Millisecond
	}
	if parsed, err := time.ParseDuration(value); err == nil {
		return parsed
	}
	return fallback
}
