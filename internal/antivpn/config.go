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
	BanCommand            string
	KickCommand           string
	BroadcastPassCommand  string
	BroadcastBlockCommand string
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
		BanCommand:           envString("ANTI_VPN_BAN_COMMAND", "addip %IP%"),
		KickCommand:          envString("ANTI_VPN_KICK_COMMAND", "clientkick %SLOT%"),
		BroadcastPassCommand: envString("ANTI_VPN_BROADCAST_PASS_TEMPLATE", `say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%`),
		BroadcastBlockCommand: envString("ANTI_VPN_BROADCAST_BLOCK_TEMPLATE", `say [Anti-VPN] VPN BLOCKED: %PLAYER% triggered anti-VPN (%SCORE%/%THRESHOLD%). %SUMMARY%`),
	}

	mode, err := parseMode(envString("ANTI_VPN_MODE", string(ModeBlock)))
	if err != nil {
		return Config{}, err
	}
	cfg.Mode = mode

	broadcastMode, err := parseBroadcastMode(envString("ANTI_VPN_BROADCAST_MODE", string(BroadcastPassAndBlock)))
	if err != nil {
		return Config{}, err
	}
	cfg.BroadcastMode = broadcastMode

	allowlist, err := parseAllowlist(os.Getenv("ANTI_VPN_ALLOWLIST"))
	if err != nil {
		return Config{}, err
	}
	cfg.Allowlist = allowlist

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
	if cfg.RetryCount < 0 || cfg.RetryCount > 5 {
		return Config{}, fmt.Errorf("ANTI_VPN_RETRY_COUNT must be between 0 and 5")
	}

	cfg.CachePath = filepath.Clean(cfg.CachePath)
	cfg.AuditLogPath = filepath.Clean(cfg.AuditLogPath)
	cfg.LogPath = filepath.Clean(cfg.LogPath)

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
