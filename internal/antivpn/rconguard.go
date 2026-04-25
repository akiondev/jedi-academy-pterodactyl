package antivpn

import (
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
)

// BadRconEvent describes a single `Bad rcon from <ip>:<port>: <command>`
// line parsed from the dedicated server's stdout/stderr. Fields are
// best-effort: the engine sometimes omits the port suffix and the
// command body may be empty. The Host field preserves the original
// textual host (useful for matching against RCON_GUARD_IGNORE_HOSTS
// entries like `localhost`); IP is the parsed network address when the
// host is a numeric literal.
type BadRconEvent struct {
	Host    string
	IP      netip.Addr
	Port    int
	Command string
	Raw     string
}

// parseBadRcon extracts a BadRconEvent from a single normalised log
// line. The pattern lives in supervisor.go alongside the other client-
// event regexes so they share the optional timestamp prefix handling.
//
// The regex captures the host token as a single whitespace-delimited
// run; this function splits an optional `:port` suffix off in a way
// that is safe for IPv6 literals (which use `::` themselves) and for
// bracketed `[v6]:port` syntax.
func parseBadRcon(line string) (BadRconEvent, bool) {
	matches := badRconPattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		return BadRconEvent{}, false
	}

	hostToken := strings.TrimSpace(matches[1])
	command := strings.TrimSpace(matches[2])
	if hostToken == "" {
		return BadRconEvent{}, false
	}

	host, port := splitBadRconHostPort(hostToken)
	if host == "" {
		return BadRconEvent{}, false
	}

	event := BadRconEvent{
		Host:    host,
		Port:    port,
		Command: command,
		Raw:     line,
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		event.IP = addr
	}

	return event, true
}

// splitBadRconHostPort splits the host token of a `Bad rcon from ...`
// line into a host and an optional port. It supports four shapes:
//
//   - `1.2.3.4`              → host=1.2.3.4, port=0
//   - `1.2.3.4:29070`        → host=1.2.3.4, port=29070
//   - `[2001:db8::1]:29070`  → host=2001:db8::1, port=29070
//   - `2001:db8::1`          → host=2001:db8::1, port=0
//   - `localhost[:port]`     → host=localhost, optional port
//
// Bare IPv6 literals containing `::` are recognised by checking that
// the token contains more than one colon and is not bracketed; in that
// case the entire token is treated as the host.
func splitBadRconHostPort(token string) (string, int) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", 0
	}

	if strings.HasPrefix(token, "[") {
		closing := strings.Index(token, "]")
		if closing == -1 {
			return strings.Trim(token, "[]"), 0
		}
		host := token[1:closing]
		rest := token[closing+1:]
		if strings.HasPrefix(rest, ":") {
			if port, err := strconv.Atoi(rest[1:]); err == nil && port > 0 && port <= 65535 {
				return host, port
			}
		}
		return host, 0
	}

	colonCount := strings.Count(token, ":")
	if colonCount == 0 {
		return token, 0
	}
	if colonCount == 1 {
		host, portText, _ := strings.Cut(token, ":")
		if port, err := strconv.Atoi(portText); err == nil && port > 0 && port <= 65535 {
			return host, port
		}
		return token, 0
	}

	// More than one colon: either a bare IPv6 literal or a hostname
	// containing colons (rare in practice). Treat the whole token as
	// the host so we do not accidentally chop off the trailing IPv6
	// segment.
	return token, 0
}

// handleBadRcon is the supervisor-side dispatch for parsed bad_rcon
// events. It implements the built-in RCON guard module:
//
//   - Local / trusted RCON sources (RCON_GUARD_IGNORE_HOSTS) are
//     ignored entirely so internal automation that legitimately uses
//     RCON does not trigger a kick or broadcast.
//   - External bad RCON attempts are always logged with concise
//     warning context.
//   - When the source IP maps to a currently-connected slot via the
//     central connection tracker, the configured action (default
//     `kick`) is applied to that slot and an optional broadcast is
//     emitted. The supervisor never claims a player was kicked when
//     no slot was found.
//
// The guard is intentionally driven from the same parsed line stream
// as the anti-VPN module: there is exactly one owner of process
// stdout/stderr, so a single bad RCON line produces at most one guard
// decision, and the supervisor's own RCON write-back can never feed
// itself.
func (s *Supervisor) handleBadRcon(stdin io.Writer, source, _ string, event BadRconEvent) {
	cfg := s.cfg.RconGuard
	if !cfg.Enabled {
		return
	}

	if rconGuardIsIgnoredHost(cfg.IgnoreHosts, event.Host, event.IP) {
		if s.logger != nil {
			s.logger.Debug(
				"rcon guard ignoring trusted source",
				"event_source", source,
				"host", event.Host,
				"command", event.Command,
			)
		}
		return
	}

	slot, state, connected := s.lookupSlotByIP(event.IP)

	if !connected {
		if s.logger != nil {
			s.logger.Warn(
				"external bad RCON attempt; no connected slot found",
				"event_source", source,
				"host", event.Host,
				"port", event.Port,
				"command", event.Command,
			)
		}
		s.auditRconGuard(source, event, "", "", "no-connected-slot", nil)
		return
	}

	playerName := strings.TrimSpace(state.PlayerName)
	if s.logger != nil {
		s.logger.Warn(
			"bad RCON attempt from connected slot",
			"event_source", source,
			"host", event.Host,
			"port", event.Port,
			"command", event.Command,
			"slot", slot,
			"player", playerName,
		)
	}

	action := strings.ToLower(strings.TrimSpace(cfg.Action))
	if action == "" {
		action = "kick"
	}

	if action == "kick" {
		kickCmd := fmt.Sprintf("clientkick %s", slot)
		if _, err := fmt.Fprintln(stdin, kickCmd); err != nil {
			if s.logger != nil {
				s.logger.Warn(
					"rcon guard kick command failed",
					"slot", slot,
					"command", kickCmd,
					"error", err,
				)
			}
			s.auditRconGuard(source, event, slot, playerName, "kick-failed", err)
			return
		}
		s.auditRconGuard(source, event, slot, playerName, "kicked", nil)

		if cfg.Broadcast {
			summary := sanitizePlayerNameForConsoleCommand(playerName)
			if summary == "" {
				summary = "Slot " + slot
			}
			broadcastCmd := fmt.Sprintf("say [RCON Guard] %s was kicked for attempting to access server RCON.", summary)
			s.enqueueBroadcast(broadcastJob{
				stdin:   stdin,
				command: broadcastCmd,
				slot:    slot,
				player:  playerName,
				summary: "rcon-guard-kick",
			})
		}
		return
	}

	// Action is configured to something other than `kick` (e.g. `log`).
	s.auditRconGuard(source, event, slot, playerName, "logged", nil)
}

// auditRconGuard writes a structured row into the anti-VPN audit log
// when one is configured. Re-using the existing audit logger keeps
// operator-facing forensic data in one place; the row is tagged with
// `event=rcon_guard` so it can be filtered separately from anti-VPN
// decisions.
func (s *Supervisor) auditRconGuard(source string, event BadRconEvent, slot, playerName, status string, err error) {
	if s.auditLogger == nil {
		return
	}

	fields := []any{
		"event", "rcon_guard",
		"event_source", source,
		"host", event.Host,
		"port", event.Port,
		"command", event.Command,
		"slot", slot,
		"player", sanitizePlayerName(playerName),
		"status", status,
	}
	if err != nil {
		fields = append(fields, "error", err)
		s.auditLogger.Warn("anti-vpn audit", fields...)
		return
	}
	s.auditLogger.Info("anti-vpn audit", fields...)
}

func rconGuardIsIgnoredHost(ignored []string, host string, addr netip.Addr) bool {
	if len(ignored) == 0 {
		return false
	}
	hostLower := strings.ToLower(strings.TrimSpace(host))
	for _, entry := range ignored {
		if entry == "" {
			continue
		}
		if hostLower != "" && entry == hostLower {
			return true
		}
		if addr.IsValid() {
			if entryAddr, err := netip.ParseAddr(entry); err == nil && entryAddr == addr {
				return true
			}
		}
	}
	return false
}
