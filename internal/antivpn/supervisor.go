package antivpn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	logTimestampPrefixPattern = `(?:\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}|\d+:\d{2})`
	ansiEscapePattern         = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	clientConnectPattern = regexp.MustCompile(
		`^(?:\s*` + logTimestampPrefixPattern + `\s+)?ClientConnect:\s+(\d+)\s+\[([^\]]+)\](?:\s+\([^)]+\))?(?:\s+"([^"]*)")?\s*$`,
	)
	clientDisconnectPattern = regexp.MustCompile(
		`^(?:\s*` + logTimestampPrefixPattern + `\s+)?ClientDisconnect:\s+(\d+)(?:\s+\[[^\]]+\])?(?:\s+\([^)]+\))?(?:\s+"[^"]*")?\s*$`,
	)
	clientUserinfoChangedPattern = regexp.MustCompile(
		`^(?:\s*` + logTimestampPrefixPattern + `\s+)?ClientUserinfoChanged:\s+(\d+)\s+(.*)$`,
	)
	// initGameBurstPattern matches log lines that signal the start of a
	// fresh game init / map restart on the dedicated server. When the
	// engine emits one of these, it follows up by re-issuing
	// ClientUserinfoChanged for every connected client. We use this signal
	// to suppress duplicate "VPN PASS" broadcasts during the burst that
	// follows.
	initGameBurstPattern = regexp.MustCompile(
		`^(?:\s*` + logTimestampPrefixPattern + `\s+)?(InitGame:|ShutdownGame|------ Server Initialization ------|exec\s+server\.cfg)`,
	)
	// badRconPattern matches lines emitted by the JKA engine when an
	// external client sends an RCON request with the wrong password,
	// e.g. `Bad rcon from 90.144.88.223:29070: status`. The supervisor
	// parses these directly from process stdout/stderr and dispatches
	// them to the built-in RCON guard module so we can map the source
	// IP back to a connected slot via the central connection tracker
	// instead of issuing our own `status` RCON query (which would loop
	// the supervisor's own output back into the parser).
	//
	// The host token is captured as a single whitespace-delimited
	// run; the optional `:port` suffix is split out by parseBadRcon
	// using engine-specific knowledge so IPv6 literals (which contain
	// colons themselves) parse correctly.
	badRconPattern = regexp.MustCompile(
		`^(?:\s*` + logTimestampPrefixPattern + `\s+)?Bad\s+rcon\s+from\s+(\S+):\s*(.*)$`,
	)
)

type slotConnectionState struct {
	Addr       netip.Addr
	PlayerName string
	SeenAt     time.Time
}

type Supervisor struct {
	cfg              Config
	logger           *slog.Logger
	auditLogger      *slog.Logger
	auditCloser      io.Closer
	engine           *Engine
	commandMu        sync.Mutex
	seenMu           sync.Mutex
	seenEvents       map[string]time.Time
	broadcastMu      sync.Mutex
	broadcastSeen    map[string]time.Time
	connectionMu     sync.Mutex
	connectionState  map[string]slotConnectionState
	checkSlots       chan struct{}
	liveFeed         *liveFeedWriter
	liveFeedErrOnce  sync.Once
	reinitMu         sync.Mutex
	reinitBurstUntil time.Time
	broadcastQueue   chan broadcastJob
	broadcastWorker  sync.Once
}

// broadcastJob is the unit of work pushed onto the supervisor's serialised
// broadcast emission queue. The queue worker drains jobs one at a time and
// sleeps cfg.BroadcastEmissionSpacing between them so that JKA's per-frame
// command buffer cannot truncate `say` payloads when multiple broadcasts are
// produced in the same tick.
type broadcastJob struct {
	stdin   io.Writer
	command string
	slot    string
	player  string
	summary string
}

type synchronizedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w synchronizedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func NewSupervisor(cfg Config, logger *slog.Logger) (*Supervisor, error) {
	engine, err := NewEngine(cfg, logger)
	if err != nil {
		return nil, err
	}

	auditLogger, auditCloser, auditErr := openAuditLogger(cfg, logger)
	if auditErr != nil && logger != nil {
		logger.Warn("anti-vpn audit log unavailable, continuing without file audit output", "path", cfg.AuditLogPath, "error", auditErr)
	}

	liveFeed, liveFeedErr := newLiveFeedWriter(
		cfg.LiveOutputPath,
		cfg.LiveOutputMaxBytes,
		cfg.LiveOutputKeepArchives,
		cfg.RotateLogsOnStart,
		func(err error) {
			if logger != nil {
				logger.Warn("anti-vpn live output rotation issue", "path", cfg.LiveOutputPath, "error", err)
			}
		},
	)
	if liveFeedErr != nil && logger != nil && cfg.LiveOutputEnabled {
		logger.Warn("anti-vpn live output mirror unavailable, continuing without addon live feed", "path", cfg.LiveOutputPath, "error", liveFeedErr)
	}
	// In the new process-output-only architecture the live mirror file
	// is opt-in and disabled by default. When it is disabled we drop the
	// writer entirely so scanOutput cannot accidentally produce a file
	// that addons might tail.
	if !cfg.LiveOutputEnabled {
		if liveFeed != nil {
			_ = liveFeed.Close()
		}
		liveFeed = nil
	}

	return &Supervisor{
		cfg:             cfg,
		logger:          logger,
		auditLogger:     auditLogger,
		auditCloser:     auditCloser,
		engine:          engine,
		seenEvents:      make(map[string]time.Time),
		broadcastSeen:   make(map[string]time.Time),
		connectionState: make(map[string]slotConnectionState),
		checkSlots:      make(chan struct{}, 8),
		liveFeed:        liveFeed,
		broadcastQueue:  make(chan broadcastJob, 64),
	}, nil
}

func (s *Supervisor) Close() error {
	errs := make([]error, 0, 3)

	if s.engine != nil {
		if err := s.engine.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.auditCloser != nil {
		if err := s.auditCloser.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.liveFeed != nil {
		if err := s.liveFeed.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (s *Supervisor) Run(ctx context.Context, serverCommand []string) error {
	if len(serverCommand) == 0 {
		return fmt.Errorf("no server command provided to supervisor")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if err := s.Close(); err != nil && s.logger != nil {
			s.logger.Warn("anti-vpn supervisor shutdown cleanup failed", "error", err)
		}
	}()

	command := exec.CommandContext(runCtx, serverCommand[0], serverCommand[1:]...)
	command.Dir = "/home/container"
	command.Env = os.Environ()

	stdin, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("create server stdin pipe: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create server stdout pipe: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return fmt.Errorf("create server stderr pipe: %w", err)
	}

	if err := command.Start(); err != nil {
		return fmt.Errorf("start server command: %w", err)
	}

	serverInput := synchronizedWriter{
		mu: &s.commandMu,
		w:  stdin,
	}

	s.logger.Info(
		"anti-vpn supervisor active",
		"mode", s.cfg.EffectiveMode(),
		"capture_mode", "process-output-only",
		"log_path", s.cfg.LogPath,
		"log_monitor_enabled", s.cfg.LogMonitorEnabled,
		"audit_log_path", s.cfg.AuditLogPath,
		"audit_allow", s.cfg.AuditAllow,
		"score_threshold", s.cfg.ScoreThreshold,
		"cache_ttl", s.cfg.CacheTTL.String(),
		"cache_flush_interval", s.cfg.CacheFlushInterval.String(),
		"live_output_path", s.cfg.LiveOutputPath,
		"live_output_enabled", s.liveFeed != nil,
		"rcon_guard_enabled", s.cfg.RconGuard.Enabled,
	)

	go s.scanOutput(runCtx, stdout, os.Stdout, "stdout", true, serverInput)
	go s.scanOutput(runCtx, stderr, os.Stderr, "stderr", true, serverInput)
	go s.forwardConsoleInput(runCtx, serverInput)
	// The legacy server.log tailer is OFF by default. The supervisor's
	// stdout/stderr scanner is the single owner/reader of the dedicated
	// server's process output and parses every event exactly once. The
	// file-based fallback remains available as an opt-in debug hook for
	// environments where stdout capture is unreliable, but is never
	// part of the default runtime path because re-reading the same
	// events from a file produces duplicate decisions, replay storms
	// after rotation, and console flooding (see internal/antivpn for
	// the full incident write-up).
	if s.cfg.LogMonitorEnabled {
		go s.monitorLogFile(runCtx, serverInput)
	}
	go s.runBroadcastWorker(runCtx)

	err = command.Wait()
	cancel()
	return err
}

func openAuditLogger(cfg Config, logger *slog.Logger) (*slog.Logger, io.Closer, error) {
	path := strings.TrimSpace(cfg.AuditLogPath)
	if path == "" {
		return nil, nil, nil
	}

	onError := func(err error) {
		if logger != nil {
			logger.Warn("anti-vpn audit log rotation issue", "path", path, "error", err)
		}
	}

	rf, err := newRotatingFile(path, cfg.AuditLogMaxBytes, cfg.AuditLogKeepArchives, cfg.RotateLogsOnStart, onError)
	if err != nil {
		return nil, nil, err
	}
	if rf == nil {
		return nil, nil, nil
	}

	auditLogger := slog.New(slog.NewJSONHandler(rf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	return auditLogger, rf, nil
}

func (s *Supervisor) scanOutput(ctx context.Context, stream io.Reader, destination io.Writer, source string, inspect bool, stdin io.Writer) {
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if _, err := fmt.Fprintln(destination, line); err != nil {
			s.logger.Warn("anti-vpn stream mirror failed", "source", source, "error", err)
			return
		}

		// Mirror every captured line into the runtime-managed live output
		// file so that addons can subscribe to live server events without
		// having to scrape Pterodactyl console output or race the engine's
		// own server.log writes.
		if s.liveFeed != nil {
			if err := s.liveFeed.WriteLine(line); err != nil {
				s.liveFeedErrOnce.Do(func() {
					if s.logger != nil {
						s.logger.Warn("anti-vpn live output mirror write failed; disabling live feed for this run", "source", source, "path", s.cfg.LiveOutputPath, "error", err)
					}
				})
			}
		}

		if inspect {
			s.handleLogLine(ctx, stdin, line, source)
		}
	}

	if err := scanner.Err(); err != nil {
		s.logger.Warn("anti-vpn stream scanner failed", "source", source, "error", err)
	}
}

func (s *Supervisor) forwardConsoleInput(ctx context.Context, stdin io.Writer) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 4*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if s.handleBuiltinConsoleCommand(ctx, line) {
			continue
		}

		if _, err := fmt.Fprintln(stdin, line); err != nil {
			s.logger.Warn("anti-vpn stdin forwarding failed", "error", err)
			return
		}
	}

	if err := scanner.Err(); err != nil {
		s.logger.Warn("anti-vpn stdin scanner failed", "error", err)
	}
}

func (s *Supervisor) handleBuiltinConsoleCommand(ctx context.Context, line string) bool {
	command := strings.TrimSpace(line)
	if command == "" {
		return false
	}

	switch strings.ToLower(command) {
	case "checkserverstatus", "rcon checkserverstatus":
		s.runCheckServerStatus(ctx)
		return true
	default:
		return false
	}
}

func (s *Supervisor) runCheckServerStatus(ctx context.Context) {
	commandPath := "/home/container/bin/checkserverstatus"

	if _, err := os.Stat(commandPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(os.Stdout, "[helper:checkserverstatus] checkserverstatus is not available in /home/container/bin")
			fmt.Fprintln(os.Stdout, "[helper:checkserverstatus] Confirm that ADDON_CHECKSERVERSTATUS_ENABLED=true, /home/container/addons/defaults/30-checkserverstatus.sh exists, and the managed runtime startup path completed normally")
			return
		}
		s.logger.Warn("checkserverstatus availability check failed", "path", commandPath, "error", err)
		fmt.Fprintln(os.Stdout, "[helper:checkserverstatus] Failed to inspect checkserverstatus command availability")
		return
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	command := exec.CommandContext(cmdCtx, commandPath)
	command.Dir = "/home/container"
	command.Env = os.Environ()
	output, err := command.CombinedOutput()

	if len(output) > 0 {
		if _, writeErr := os.Stdout.Write(output); writeErr != nil && s.logger != nil {
			s.logger.Warn("checkserverstatus console output mirror failed", "error", writeErr)
		}
		if output[len(output)-1] != '\n' {
			fmt.Fprintln(os.Stdout)
		}
	}

	if err == nil {
		return
	}

	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		fmt.Fprintln(os.Stdout, "[helper:checkserverstatus] checkserverstatus timed out after 10s")
		return
	}

	if s.logger != nil {
		s.logger.Warn("checkserverstatus execution failed", "error", err)
	}
	fmt.Fprintln(os.Stdout, "[helper:checkserverstatus] checkserverstatus failed")
}

// logMonitorMaxBytesPerPoll caps how many bytes the log-file fallback tail
// will read in a single poll iteration. The supervisor's stdout/stderr
// scanner is the authoritative source for live ClientConnect /
// ClientUserinfoChanged / ClientDisconnect events; the log-file tailer
// exists only as a defensive fallback for environments where stdout capture
// is unreliable. Capping per-poll reads ensures a sudden large append (or a
// future regression that mis-handles offsets) cannot replay an arbitrarily
// large historical window in one burst, which previously produced
// console-flooding audit/broadcast storms (>10000 decisions/second) that
// starved legitimate connection processing on the JKA engine.
const logMonitorMaxBytesPerPoll int64 = 1 * 1024 * 1024

// logOffsetAction enumerates how the log-tail offset was adjusted on a poll
// iteration. It is consumed by monitorLogFile to decide what (if anything)
// to log.
type logOffsetAction int

const (
	logOffsetActionNone logOffsetAction = iota
	// logOffsetActionReattachAfterShrink indicates the file shrank below the
	// previous offset (rotated, truncated, or replaced). The new offset is
	// the current end-of-file; no replay is performed.
	logOffsetActionReattachAfterShrink
	// logOffsetActionSkipBacklog indicates the unread window exceeded the
	// per-poll cap and the offset was advanced to within the cap, skipping
	// the leading bytes.
	logOffsetActionSkipBacklog
)

// nextLogOffset computes the next read offset for the log-file fallback
// tailer given the previous offset, the current file size and the per-poll
// cap. It returns the new offset, an action describing the adjustment (for
// logging) and the number of bytes skipped (only meaningful for the backlog
// cap case).
//
// The function is intentionally pure so the offset-recovery logic can be
// unit-tested independently of file system / goroutine plumbing.
func nextLogOffset(previousOffset, currentSize, maxBytesPerPoll int64) (int64, logOffsetAction, int64) {
	if currentSize < previousOffset {
		return currentSize, logOffsetActionReattachAfterShrink, 0
	}
	if maxBytesPerPoll > 0 && currentSize-previousOffset > maxBytesPerPoll {
		skipped := currentSize - previousOffset - maxBytesPerPoll
		return currentSize - maxBytesPerPoll, logOffsetActionSkipBacklog, skipped
	}
	return previousOffset, logOffsetActionNone, 0
}

func (s *Supervisor) monitorLogFile(ctx context.Context, stdin io.Writer) {
	path := filepath.Clean(s.cfg.LogPath)
	var offset int64
	attached := false

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		file, err := os.Open(path)
		if err != nil {
			time.Sleep(s.cfg.LogPollInterval)
			continue
		}

		stat, err := file.Stat()
		if err != nil {
			file.Close()
			time.Sleep(s.cfg.LogPollInterval)
			continue
		}

		if !attached {
			offset = stat.Size()
			attached = true
			s.logger.Info("anti-vpn log monitor attached", "path", path)
		}
		newOffset, action, skipped := nextLogOffset(offset, stat.Size(), logMonitorMaxBytesPerPoll)
		switch action {
		case logOffsetActionReattachAfterShrink:
			// File rotated, truncated, or replaced. Re-attach to the
			// CURRENT end-of-file instead of replaying the (potentially
			// large) post-rotation content from offset 0.
			//
			// The supervisor's stdout/stderr scanner is the primary
			// source for live engine events; it has already processed any
			// ClientConnect/Userinfo/Disconnect lines emitted in real
			// time. Replaying from 0 here would re-feed historical
			// events (with stale slot/IP pairings) into handleLogLine,
			// which in turn re-triggers cache-hit decisions, audit-log
			// writes, and `say` broadcasts at microsecond cadence,
			// flooding the engine command buffer and preventing new
			// players from completing their connection handshake.
			//
			// Skipping straight to end-of-file is safe: the worst case
			// is that the fallback tailer misses a handful of events
			// that were written between two polls, all of which were
			// already observed via stdout. If stdout capture is broken
			// in some deployment, a full reattach still beats a
			// thundering herd of stale replays.
			if s.logger != nil {
				s.logger.Warn(
					"anti-vpn log file shrank; re-attaching to current end-of-file without replay to avoid event storm",
					"path", path,
					"previous_offset", offset,
					"new_size", stat.Size(),
				)
			}
		case logOffsetActionSkipBacklog:
			if s.logger != nil {
				s.logger.Warn(
					"anti-vpn log poll backlog exceeds cap; skipping ahead to avoid replay storm",
					"path", path,
					"skipped_bytes", skipped,
					"cap_bytes", logMonitorMaxBytesPerPoll,
				)
			}
		}
		offset = newOffset

		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			file.Close()
			time.Sleep(s.cfg.LogPollInterval)
			continue
		}

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		readLine := false
		for scanner.Scan() {
			line := scanner.Text()
			offset += int64(len(scanner.Bytes()) + 1)
			readLine = true
			s.handleLogLine(ctx, stdin, line, filepath.Base(path))
		}
		file.Close()

		if !readLine {
			time.Sleep(s.cfg.LogPollInterval)
		}
	}
}

func (s *Supervisor) handleLogLine(ctx context.Context, stdin io.Writer, line string, source string) {
	line = normalizeLogLineForParsing(line)
	if line == "" {
		return
	}

	if initGameBurstPattern.MatchString(line) {
		s.markReinitBurst()
		// Continue processing the line in case it also matches one of the
		// client-event patterns below (defensive; current patterns are
		// disjoint).
	}

	if event, ok := parseBadRcon(line); ok {
		s.handleBadRcon(stdin, source, line, event)
		return
	}

	if slot, ok := parseClientDisconnect(line); ok {
		s.clearConnectionState(slot)
		return
	}

	slot, addr, playerName, ok := parseClientConnect(line)
	if ok {
		s.storeConnectionState(slot, addr, playerName)
		s.processConnectionEvent(ctx, stdin, source, slot, addr, playerName, "connect")
		return
	}

	slot, addr, playerName, hasAddr, ok := parseClientUserinfoChangedFields(line)
	if !ok {
		return
	}

	if !hasAddr {
		// Name/team-only userinfo change: update display name in tracked state but do
		// not trigger a new connection check. The player was already checked on
		// ClientConnect or the first ClientUserinfoChanged that carried an IP.
		if strings.TrimSpace(playerName) != "" {
			s.updateConnectionStateName(slot, playerName)
		}
		return
	}

	// IP is present: only trigger a check when the address is genuinely new for this
	// slot's active session. Subsequent ClientUserinfoChanged lines with the same IP
	// (common in JKA on team/model changes) would otherwise re-queue redundant checks.
	prevState, hasPrevState := s.lookupConnectionState(slot)
	s.storeConnectionState(slot, addr, playerName)
	if hasPrevState && prevState.Addr.IsValid() && prevState.Addr == addr {
		return
	}

	s.processConnectionEvent(ctx, stdin, source, slot, addr, playerName, "userinfo")
}

// markReinitBurst extends the suppression window during which broadcasts
// triggered by cached re-checks (i.e. ClientUserinfoChanged events that hit
// the cache) are suppressed. The engine emits ClientUserinfoChanged for
// every connected client immediately after `InitGame:` / `ShutdownGame` /
// `exec server.cfg`; without this guard we would re-broadcast a "VPN PASS"
// line for every existing player every time the map cycles, which floods the
// game console (observed in real game logs as 7+ broadcasts within a single
// second, several truncated mid-string by JKA's per-frame command buffer).
func (s *Supervisor) markReinitBurst() {
	window := s.cfg.BroadcastEmissionSpacing * 8
	if window < 5*time.Second {
		window = 5 * time.Second
	}
	if window > 30*time.Second {
		window = 30 * time.Second
	}
	deadline := time.Now().UTC().Add(window)

	s.reinitMu.Lock()
	if deadline.After(s.reinitBurstUntil) {
		s.reinitBurstUntil = deadline
	}
	s.reinitMu.Unlock()
}

// inReinitBurst reports whether the supervisor is currently inside a
// post-reinit burst suppression window.
func (s *Supervisor) inReinitBurst() bool {
	s.reinitMu.Lock()
	defer s.reinitMu.Unlock()
	return time.Now().UTC().Before(s.reinitBurstUntil)
}

func (s *Supervisor) processConnectionEvent(ctx context.Context, stdin io.Writer, source, slot string, addr netip.Addr, playerName, triggerKind string) {
	eventKey := slot + "|" + addr.String()
	if !s.markEvent(eventKey) {
		return
	}

	go func() {
		select {
		case s.checkSlots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-s.checkSlots }()

		decision, err := s.engine.CheckIP(ctx, addr)
		if err != nil {
			s.logger.Warn("anti-vpn IP check failed", "ip", addr.String(), "error", err)
			return
		}

		s.auditDecision(source, slot, addr, decision)

		if s.cfg.LogDecisions {
			level := slog.LevelInfo
			if decision.Blocked || decision.WouldBlock || decision.Degraded {
				level = slog.LevelWarn
			}
			fields := append([]any{"event_source", source, "slot", slot, "player", playerName, "trigger", triggerKind}, DecisionLogFields(decision)...)
			s.logger.Log(ctx, level, "anti-vpn decision", fields...)
		}

		s.broadcastDecision(stdin, slot, playerName, decision, triggerKind)

		if decision.Blocked {
			s.enforceDecision(stdin, slot, addr, decision)
		}
	}()
}

func (s *Supervisor) storeConnectionState(slot string, addr netip.Addr, playerName string) {
	if strings.TrimSpace(slot) == "" || !addr.IsValid() {
		return
	}

	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()

	state := s.connectionState[slot]
	state.Addr = addr
	if strings.TrimSpace(playerName) != "" {
		state.PlayerName = playerName
	}
	state.SeenAt = time.Now().UTC()
	s.connectionState[slot] = state
}

func (s *Supervisor) lookupConnectionState(slot string) (slotConnectionState, bool) {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()

	state, ok := s.connectionState[slot]
	return state, ok
}

// lookupSlotByIP returns the most recently observed connected slot whose
// tracked address matches the supplied IP, if any. Used by the built-in
// RCON guard module to map the source IP of a `Bad rcon` line to a
// currently-connected player slot without having to issue an RCON
// `status` query (which the legacy Python addon did, and which created a
// feedback loop because the supervisor's own RCON traffic produced more
// log lines for the addon to react to).
func (s *Supervisor) lookupSlotByIP(addr netip.Addr) (string, slotConnectionState, bool) {
	if !addr.IsValid() {
		return "", slotConnectionState{}, false
	}

	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()

	var (
		bestSlot  string
		bestState slotConnectionState
		found     bool
	)
	for slot, state := range s.connectionState {
		if !state.Addr.IsValid() || state.Addr != addr {
			continue
		}
		if !found || state.SeenAt.After(bestState.SeenAt) {
			bestSlot = slot
			bestState = state
			found = true
		}
	}
	return bestSlot, bestState, found
}

func (s *Supervisor) clearConnectionState(slot string) {
	if strings.TrimSpace(slot) == "" {
		return
	}

	s.connectionMu.Lock()
	state, hasState := s.connectionState[slot]
	delete(s.connectionState, slot)
	s.connectionMu.Unlock()

	// Also clear the dedupe entry for this slot so that a new player joining the
	// same slot after a disconnect gets a fresh check even within the dedupe window.
	// The broadcast cooldown is intentionally NOT cleared here: the cooldown is
	// keyed by IP (not slot), and persisting it across disconnects prevents a rapid
	// kick-and-reconnect cycle from mass-spamming the console with repeated VPN PASS
	// or VPN BLOCKED announcements. The natural cooldown window (BroadcastCooldown)
	// ensures that a legitimately reconnecting player still receives a fresh broadcast
	// once the window expires.
	if hasState && state.Addr.IsValid() {
		s.seenMu.Lock()
		delete(s.seenEvents, slot+"|"+state.Addr.String())
		s.seenMu.Unlock()
	}
}

func (s *Supervisor) updateConnectionStateName(slot, playerName string) {
	if strings.TrimSpace(slot) == "" || strings.TrimSpace(playerName) == "" {
		return
	}

	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()

	if state, ok := s.connectionState[slot]; ok {
		state.PlayerName = playerName
		s.connectionState[slot] = state
	}
}

func (s *Supervisor) enforceDecision(stdin io.Writer, slot string, addr netip.Addr, decision Decision) {
	ipText := addr.String()
	commands := make([]string, 0, 2)

	appendBan := func(command string) {
		if strings.TrimSpace(command) == "" {
			return
		}
		commands = append(commands, fillCommandTemplate(command, commandTemplateData{
			IP:   ipText,
			Slot: slot,
		}))
	}
	appendKick := func(command string) {
		if slot == "" || strings.TrimSpace(command) == "" {
			return
		}
		commands = append(commands, fillCommandTemplate(command, commandTemplateData{
			IP:   ipText,
			Slot: slot,
		}))
	}

	switch s.cfg.EnforcementMode {
	case EnforcementKickOnly:
		appendKick("clientkick %SLOT%")
	case EnforcementBanAndKick:
		appendBan("addip %IP%")
		appendKick("clientkick %SLOT%")
	case EnforcementBanOnly:
		appendBan("addip %IP%")
	case EnforcementCustom:
		appendBan(s.cfg.BanCommand)
		appendKick(s.cfg.KickCommand)
	default:
		appendKick("clientkick %SLOT%")
	}

	for _, command := range commands {
		if _, err := fmt.Fprintln(stdin, command); err != nil {
			s.logger.Warn("anti-vpn enforcement command failed", "ip", ipText, "slot", slot, "command", command, "error", err)
			s.auditEnforcement(ipText, slot, command, decision, err)
			return
		}
		s.logger.Warn("anti-vpn enforcement command sent", "ip", ipText, "slot", slot, "command", command, "summary", decision.Summary)
		s.auditEnforcement(ipText, slot, command, decision, nil)
	}
}

func (s *Supervisor) auditDecision(source, slot string, addr netip.Addr, decision Decision) {
	if s.auditLogger == nil {
		return
	}

	action := decisionAction(decision)
	// Suppress per-allow audit rows by default. A busy server can produce
	// thousands of allow decisions per map (cache hits, userinfo
	// re-checks, reconnects), and the historical audit log was the
	// observed source of console-flooding storms after a map restart
	// fed cached events back through the file tailer. Block /
	// would-block / degraded / provider errors are always audited.
	if action == "allow" && !decision.Degraded && !s.cfg.AuditAllow {
		return
	}

	fields := []any{
		"event", "decision",
		"event_source", source,
		"slot", slot,
		"ip", addr.String(),
		"action", action,
	}
	fields = append(fields, DecisionLogFields(decision)...)
	s.auditLogger.Info("anti-vpn audit", fields...)
}

func (s *Supervisor) broadcastDecision(stdin io.Writer, slot, playerName string, decision Decision, triggerKind string) {
	// Suppress redundant cached "VPN PASS" broadcasts triggered by the engine
	// re-emitting ClientUserinfoChanged for every existing client during a map
	// restart / cfg reload. Genuine ClientConnect events (triggerKind ==
	// "connect") and any blocked / would-block decision still go through.
	if triggerKind == "userinfo" && decision.FromCache && !decision.Blocked && !decision.WouldBlock && s.inReinitBurst() {
		return
	}

	if !s.shouldBroadcast(decision) {
		return
	}

	publicSummary := publicDecisionSummary(decision)
	template := s.cfg.BroadcastPassCommand
	if decision.Blocked {
		template = s.cfg.BroadcastBlockCommand
	}

	command := fillCommandTemplate(
		template,
		commandTemplateData{
			Player:    sanitizePlayerNameForConsoleCommand(playerName),
			Score:     decision.Score,
			Threshold: decision.Threshold,
			Summary:   publicSummary,
			IP:        decision.IP,
			Slot:      slot,
		},
	)
	if command == "" {
		return
	}

	s.enqueueBroadcast(broadcastJob{
		stdin:   stdin,
		command: command,
		slot:    slot,
		player:  playerName,
		summary: publicSummary,
	})
}

// enqueueBroadcast hands a broadcast command off to the serialised emission
// queue. If the queue is full (rare; would require >64 broadcasts pending at
// once) the broadcast is dropped with a warning rather than blocking the
// caller. If the queue is uninitialised (e.g. in unit tests that exercise
// broadcastDecision in isolation), the command is written inline.
func (s *Supervisor) enqueueBroadcast(job broadcastJob) {
	if s.broadcastQueue == nil {
		s.emitBroadcastJob(job)
		return
	}
	select {
	case s.broadcastQueue <- job:
	default:
		if s.logger != nil {
			s.logger.Warn("anti-vpn broadcast queue saturated; dropping broadcast", "slot", job.slot, "player", job.player, "summary", job.summary)
		}
	}
}

// emitBroadcastJob writes a single queued broadcast to the engine and records
// the audit trail. Used both by the worker (one job per spaced tick) and by
// the inline fallback when no worker is running.
func (s *Supervisor) emitBroadcastJob(job broadcastJob) {
	if _, err := fmt.Fprintln(job.stdin, job.command); err != nil {
		if s.logger != nil {
			s.logger.Warn("anti-vpn broadcast command failed", "slot", job.slot, "player", job.player, "command", job.command, "error", err)
		}
		if s.auditLogger != nil {
			s.auditLogger.Warn("anti-vpn audit", "event", "broadcast", "status", "failed", "slot", job.slot, "player", sanitizePlayerName(job.player), "command", job.command, "summary", job.summary, "error", err)
		}
		return
	}
	if s.logger != nil {
		s.logger.Info("anti-vpn broadcast command sent", "slot", job.slot, "player", job.player, "command", job.command, "summary", job.summary)
	}
	if s.auditLogger != nil {
		s.auditLogger.Info("anti-vpn audit", "event", "broadcast", "status", "sent", "slot", job.slot, "player", sanitizePlayerName(job.player), "command", job.command, "summary", job.summary)
	}
}

// runBroadcastWorker drains the broadcast queue, emitting one command at a
// time with cfg.BroadcastEmissionSpacing between successive sends. Spacing
// the writes guarantees the JKA engine processes one `say` per simulation
// frame, eliminating the per-frame command buffer truncation that previously
// produced cut-off broadcasts (e.g. "VPN PASS: ... cleared checks (10/").
func (s *Supervisor) runBroadcastWorker(ctx context.Context) {
	s.broadcastWorker.Do(func() {
		spacing := s.cfg.BroadcastEmissionSpacing
		var lastEmit time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-s.broadcastQueue:
				if spacing > 0 {
					if since := time.Since(lastEmit); since < spacing {
						select {
						case <-ctx.Done():
							return
						case <-time.After(spacing - since):
						}
					}
				}
				s.emitBroadcastJob(job)
				lastEmit = time.Now()
			}
		}
	})
}

func (s *Supervisor) auditEnforcement(ipText, slot, command string, decision Decision, commandErr error) {
	if s.auditLogger == nil {
		return
	}

	fields := []any{
		"event", "enforcement",
		"ip", ipText,
		"slot", slot,
		"command", command,
		"summary", decision.Summary,
	}
	if commandErr != nil {
		fields = append(fields, "status", "failed", "error", commandErr)
		s.auditLogger.Warn("anti-vpn audit", fields...)
		return
	}

	fields = append(fields, "status", "sent")
	s.auditLogger.Warn("anti-vpn audit", fields...)
}

func decisionAction(decision Decision) string {
	switch {
	case decision.Blocked:
		return "block"
	case decision.WouldBlock:
		return "would-block"
	default:
		return "allow"
	}
}

func (s *Supervisor) shouldBroadcast(decision Decision) bool {
	switch s.cfg.BroadcastMode {
	case BroadcastOff:
		return false
	case BroadcastBlockOnly:
		if !decision.Blocked {
			return false
		}
	case BroadcastPassAndBlock:
	default:
		return false
	}

	if s.cfg.BroadcastCooldown <= 0 {
		return true
	}

	now := time.Now().UTC()
	// Key by IP only (no slot) so the cooldown persists across disconnect/reconnect
	// cycles regardless of which slot the player lands on. This prevents a rapid
	// kick-and-reconnect loop (e.g., triggered by a long in-game chat message) from
	// mass-spamming the console with repeated VPN PASS/BLOCK announcements.
	key := decision.IP + "|" + decisionAction(decision)

	s.broadcastMu.Lock()
	defer s.broadcastMu.Unlock()

	for current, seenAt := range s.broadcastSeen {
		if now.Sub(seenAt) > 10*time.Minute {
			delete(s.broadcastSeen, current)
		}
	}

	if seenAt, exists := s.broadcastSeen[key]; exists && now.Sub(seenAt) < s.cfg.BroadcastCooldown {
		return false
	}

	s.broadcastSeen[key] = now
	return true
}

func (s *Supervisor) markEvent(key string) bool {
	now := time.Now().UTC()

	s.seenMu.Lock()
	defer s.seenMu.Unlock()

	for current, seenAt := range s.seenEvents {
		if now.Sub(seenAt) > 5*time.Minute {
			delete(s.seenEvents, current)
		}
	}

	if seenAt, exists := s.seenEvents[key]; exists && now.Sub(seenAt) < s.cfg.EventDedupeInterval {
		return false
	}

	s.seenEvents[key] = now
	return true
}

func parseClientConnect(line string) (string, netip.Addr, string, bool) {
	matches := clientConnectPattern.FindStringSubmatch(line)
	if len(matches) != 4 {
		return "", netip.Addr{}, "", false
	}

	addr, err := parseServerIPField(matches[2])
	if err != nil {
		return "", netip.Addr{}, "", false
	}

	return matches[1], addr, normalizeLoggedPlayerName(matches[3]), true
}

func normalizeLogLineForParsing(line string) string {
	line = ansiEscapePattern.ReplaceAllString(line, "")
	line = strings.TrimSpace(strings.TrimRight(line, "\r\n"))
	if line == "" {
		return ""
	}
	return line
}

func parseClientDisconnect(line string) (string, bool) {
	matches := clientDisconnectPattern.FindStringSubmatch(line)
	if len(matches) != 2 {
		return "", false
	}
	return matches[1], true
}

func parseClientUserinfoChanged(line string) (string, netip.Addr, string, bool) {
	slot, addr, playerName, hasAddr, ok := parseClientUserinfoChangedFields(line)
	if !ok || !hasAddr {
		return "", netip.Addr{}, "", false
	}
	return slot, addr, playerName, true
}

func parseClientUserinfoChangedFields(line string) (string, netip.Addr, string, bool, bool) {
	matches := clientUserinfoChangedPattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		return "", netip.Addr{}, "", false, false
	}

	slot := matches[1]
	userinfo := strings.TrimSpace(matches[2])
	if userinfo == "" || strings.EqualFold(userinfo, "<no change>") {
		return "", netip.Addr{}, "", false, false
	}

	playerName := extractUserinfoValue(userinfo, "n")
	rawIP := strings.TrimSpace(extractUserinfoValue(userinfo, "ip"))
	if rawIP == "" {
		return slot, netip.Addr{}, playerName, false, true
	}

	addr, err := parseServerIPField(rawIP)
	if err != nil {
		return "", netip.Addr{}, "", false, false
	}

	return slot, addr, playerName, true, true
}

func parseServerIPField(value string) (netip.Addr, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}, fmt.Errorf("empty IP field")
	}

	if addr, err := netip.ParseAddr(value); err == nil {
		return addr, nil
	}
	if addrPort, err := netip.ParseAddrPort(value); err == nil {
		return addrPort.Addr(), nil
	}

	lastColon := strings.LastIndex(value, ":")
	if lastColon > 0 && lastColon < len(value)-1 && isAllDigits(value[lastColon+1:]) {
		if addr, err := netip.ParseAddr(value[:lastColon]); err == nil {
			return addr, nil
		}
	}

	return netip.Addr{}, fmt.Errorf("unsupported IP field %q", value)
}

type commandTemplateData struct {
	Player    string
	Score     int
	Threshold int
	Summary   string
	IP        string
	Slot      string
}

func fillCommandTemplate(template string, data commandTemplateData) string {
	replacer := strings.NewReplacer(
		"%PLAYER%", data.Player,
		"%SCORE%", fmt.Sprintf("%d", data.Score),
		"%THRESHOLD%", fmt.Sprintf("%d", data.Threshold),
		"%SUMMARY%", data.Summary,
		"%IP%", data.IP,
		"%SLOT%", data.Slot,
	)
	return strings.TrimSpace(replacer.Replace(template))
}

func extractUserinfoValue(line, key string) string {
	line = strings.TrimSpace(line)
	if line == "" || key == "" {
		return ""
	}

	remaining := line
	if strings.HasPrefix(remaining, `\`) {
		remaining = remaining[1:]
	}

	for remaining != "" {
		keyEnd := strings.Index(remaining, `\`)
		if keyEnd == -1 {
			return ""
		}

		currentKey := remaining[:keyEnd]
		remaining = remaining[keyEnd+1:]

		valueEnd := strings.Index(remaining, `\`)
		if valueEnd == -1 {
			if currentKey == key {
				return remaining
			}
			return ""
		}

		currentValue := remaining[:valueEnd]
		if currentKey == key {
			return currentValue
		}

		remaining = remaining[valueEnd+1:]
	}

	return ""
}

func normalizeLoggedPlayerName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}

	for {
		trimmed, changed := trimTrailingColorCode(value)
		if !changed {
			return value
		}
		value = strings.TrimSpace(trimmed)
	}
}

func trimTrailingColorCode(value string) (string, bool) {
	if len(value) >= 8 && value[len(value)-8] == '^' && (value[len(value)-7] == 'x' || value[len(value)-7] == 'X') {
		hex := value[len(value)-6:]
		if isHexString(hex) {
			return value[:len(value)-8], true
		}
	}

	if len(value) >= 2 && value[len(value)-2] == '^' && isColorCodeSuffix(value[len(value)-1]) {
		return value[:len(value)-2], true
	}

	return value, false
}

func isHexString(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func isColorCodeSuffix(char byte) bool {
	return (char >= '0' && char <= '9') || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func sanitizePlayerName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown Player"
	}

	var builder strings.Builder
	builder.Grow(len(value))

	skipColorDigit := false
	for _, char := range value {
		if skipColorDigit {
			skipColorDigit = false
			if char >= '0' && char <= '9' {
				continue
			}
		}
		if char == '^' {
			skipColorDigit = true
			continue
		}
		if char < 32 || char == 127 {
			continue
		}
		builder.WriteRune(char)
	}

	sanitized := strings.TrimSpace(builder.String())
	if sanitized == "" {
		sanitized = "Unknown Player"
	}
	if len([]rune(sanitized)) > 32 {
		runes := []rune(sanitized)
		sanitized = string(runes[:32]) + "..."
	}
	return sanitized
}

func sanitizePlayerNameForConsoleCommand(value string) string {
	sanitized := sanitizePlayerName(value)

	var builder strings.Builder
	builder.Grow(len(sanitized))

	for _, char := range sanitized {
		switch char {
		case '"', '\'', ';', '\\', '`', '$':
			continue
		case '\r', '\n':
			continue
		}
		builder.WriteRune(char)
	}

	safe := strings.TrimSpace(builder.String())
	if safe == "" {
		return "Unknown Player"
	}
	return safe
}

func publicDecisionSummary(decision Decision) string {
	switch {
	case decision.Blocked && decision.StrongSignals > 0:
		return "High-confidence VPN or non-residential signal detected."
	case decision.Blocked && decision.DetectingProviders >= 2:
		return "Multiple providers reported VPN or hosting signals."
	case decision.Blocked:
		return "Configured VPN threshold was reached."
	case decision.Degraded && decision.ProviderSuccesses == 0:
		return "Allowed with limited provider coverage."
	case decision.Degraded:
		return "Allowed with partial provider coverage."
	case decision.DetectingProviders == 0:
		return "No provider reported a VPN or hosting signal."
	default:
		return "Score remained below the configured threshold."
	}
}

func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
