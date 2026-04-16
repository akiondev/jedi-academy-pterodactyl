# Anti-VPN

## Purpose

This project includes an anti-VPN runtime component intended specifically for VPN, hosting-backed, and non-residential IP detection during Jedi Academy / TaystJK server operation on Pterodactyl.

It is intentionally scoped to anti-VPN behavior only:

- online API checks only
- no public proxy lists
- no Tor lists
- no giant offline blocklists
- no startup hard-fail if providers are unavailable

## Architecture

The anti-VPN feature is implemented as a compiled Go binary inside the runtime image:

- binary: `taystjk-antivpn`
- launch path: `scripts/entrypoint.sh`
- runtime model: supervisor around the dedicated server process
- signal source: stdout-first event capture with `server.log` fallback
- enforcement path: server stdin console commands
- optional public player-chat broadcast path: server `say` command templates
- audit path: dedicated anti-VPN audit log file

The supervisor mirrors the dedicated server output back to Pterodactyl, extracts player IPs from `ClientUserinfoChanged` events as close to the process as possible, queries providers in parallel, evaluates a weighted score, writes a structured audit trail, and optionally sends server console commands such as `addip` and `clientkick`.

## Supported providers

- `proxycheck.io`
- `ipapi.is`
- `IPQualityScore`
- `IPLocate`
- `IPHub`
- `vpnapi.io` (optional)

Providers are queried independently with:

- request timeouts
- small retries
- per-provider rate gating
- local cache with TTL

If one provider fails, the others still run. If every provider fails, the decision falls back to allow and logs the degraded state.

## Weighted scoring

The default model is intentionally conservative:

- strong VPN detections from `proxycheck.io`, `ipapi.is`, `IPQualityScore`, `IPLocate`, or `vpnapi.io` carry the most weight
- hosting or datacenter signals carry medium weight
- `IPHub` contributes medium or weak non-residential signals
- a block normally requires the threshold to be met and either:
  - at least one strong signal, or
  - agreement from at least two providers

This avoids blocking only because of one weak or ambiguous signal.

Default threshold: `90`

## Modes

- `off`
- `log-only`
- `block`

Behavior by mode:

- `off`: the Go supervisor is not used
- `log-only`: decisions are logged but no enforcement commands are sent
- `block`: decisions above threshold can send console commands back to the game server

## Cache

Default cache path:

`/home/container/.cache/taystjk-antivpn/cache.json`

Default TTL:

`6h`

Default flush interval:

`2s`

The cache is local to the server volume and survives container restarts as long as the volume persists.

The cache is persisted in the background so repeated joins do not trigger a full cache rewrite on every single decision.

## Audit logging

Default audit log path:

`/home/container/logs/anti-vpn-audit.log`

The audit log is separate from the regular game console and records:

- event source
- matched IP and slot
- weighted score and threshold
- provider status summary
- final allow / would-block / block action
- enforcement commands that were sent or failed

## Public broadcasts

Anti-VPN can optionally send public `say` messages to all players after each completed decision.

Recommended defaults:

- include the player name
- always include score and threshold
- keep summaries short and public-safe
- do not expose the player IP
- do not expose raw provider errors

Supported broadcast modes:

- `off`
- `block-only`
- `pass-and-block`

Default pass template:

`say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%`

Default block template:

`say [Anti-VPN] VPN BLOCKED: %PLAYER% triggered anti-VPN (%SCORE%/%THRESHOLD%). %SUMMARY%`

Supported placeholders:

- `%PLAYER%`
- `%SCORE%`
- `%THRESHOLD%`
- `%SUMMARY%`
- `%IP%`
- `%SLOT%`

Player names are sanitized before broadcast to strip color codes, control characters, and overlong values.

## Allowlist

`ANTI_VPN_ALLOWLIST` supports:

- individual IPs
- CIDR ranges
- comma-separated values
- whitespace-separated values

Example:

```text
203.0.113.10, 198.51.100.0/24
2001:db8::/32
```

Allowlisted addresses always bypass anti-VPN scoring.

## Egg variables

- `ANTI_VPN_ENABLED`
- `ANTI_VPN_MODE`
- `ANTI_VPN_CACHE_TTL`
- `ANTI_VPN_CACHE_FLUSH_INTERVAL`
- `ANTI_VPN_SCORE_THRESHOLD`
- `ANTI_VPN_ALLOWLIST`
- `ANTI_VPN_PROXYCHECK_API_KEY`
- `ANTI_VPN_IPAPIIS_API_KEY`
- `ANTI_VPN_IPHUB_API_KEY`
- `ANTI_VPN_VPNAPI_IO_API_KEY`
- `ANTI_VPN_IPQUALITYSCORE_API_KEY`
- `ANTI_VPN_IPLOCATE_API_KEY`
- `ANTI_VPN_TIMEOUT_MS`
- `ANTI_VPN_LOG_DECISIONS`
- `ANTI_VPN_AUDIT_LOG_PATH`
- `ANTI_VPN_BROADCAST_MODE`
- `ANTI_VPN_BROADCAST_COOLDOWN`
- `ANTI_VPN_BROADCAST_PASS_TEMPLATE`
- `ANTI_VPN_BROADCAST_BLOCK_TEMPLATE`
- `ANTI_VPN_BAN_COMMAND`
- `ANTI_VPN_KICK_COMMAND`

## Enforcement notes

Default commands:

- ban command: `addip %IP%`
- kick command: `clientkick %SLOT%`

These are exposed as variables because different mods or server builds can use slightly different admin command conventions.

## Operational notes

- Primary event capture happens from the live server stdout stream, with `server.log` used as a fallback path.
- Custom startup commands bypass the normal anti-VPN supervisor path and are logged as such by the entrypoint.
- Anonymous provider access is allowed for `proxycheck.io` and `ipapi.is`, but production deployments should still configure API keys there as well to avoid low shared limits.
- `IPQualityScore`, `IPLocate`, `IPHub`, and `vpnapi.io` are only active when their API keys are configured.
- Provider failures degrade the decision quality, but they do not fail server startup and they do not force a block by themselves.
- The dedicated audit log is the best place to review why a player was allowed, would have been blocked, or was actively blocked.
- Public broadcasts are rate-limited with a cooldown per slot and action to avoid repetitive spam from duplicate join events.

## Recommended defaults

- `ANTI_VPN_ENABLED=true`
- `ANTI_VPN_MODE=block`
- `ANTI_VPN_SCORE_THRESHOLD=90`
- `ANTI_VPN_CACHE_TTL=6h`
- `ANTI_VPN_CACHE_FLUSH_INTERVAL=2s`
- `ANTI_VPN_TIMEOUT_MS=1500`
- `ANTI_VPN_LOG_DECISIONS=true`
- `ANTI_VPN_AUDIT_LOG_PATH=/home/container/logs/anti-vpn-audit.log`
- `ANTI_VPN_BROADCAST_MODE=pass-and-block`
- `ANTI_VPN_BROADCAST_COOLDOWN=90s`

If you want to tune before hard enforcement, switch to `log-only` temporarily and review the anti-VPN audit log on your own playerbase.
