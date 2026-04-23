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
	clientConnectPattern = regexp.MustCompile(
		`^(?:\s*` + logTimestampPrefixPattern + `\s+)?ClientConnect:\s+(\d+)\s+\[([^\]]+)\](?:\s+\([^)]+\))?(?:\s+"([^"]*)")?\s*$`,
	)
	clientDisconnectPattern = regexp.MustCompile(
		`^(?:\s*` + logTimestampPrefixPattern + `\s+)?ClientDisconnect:\s+(\d+)(?:\s+\[[^\]]+\])?(?:\s+\([^)]+\))?(?:\s+"[^"]*")?\s*$`,
	)
	clientUserinfoChangedPattern = regexp.MustCompile(
		`^(?:\s*` + logTimestampPrefixPattern + `\s+)?ClientUserinfoChanged:\s+(\d+)\s+(.*)$`,
	)
)

type slotConnectionState struct {
	Addr       netip.Addr
	PlayerName string
	SeenAt     time.Time
}

type Supervisor struct {
	cfg             Config
	logger          *slog.Logger
	auditLogger     *slog.Logger
	auditCloser     io.Closer
	engine          *Engine
	commandMu       sync.Mutex
	seenMu          sync.Mutex
	seenEvents      map[string]time.Time
	broadcastMu     sync.Mutex
	broadcastSeen   map[string]time.Time
	connectionMu    sync.Mutex
	connectionState map[string]slotConnectionState
	checkSlots      chan struct{}
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

	auditLogger, auditCloser, auditErr := openAuditLogger(cfg)
	if auditErr != nil && logger != nil {
		logger.Warn("anti-vpn audit log unavailable, continuing without file audit output", "path", cfg.AuditLogPath, "error", auditErr)
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
	}, nil
}

func (s *Supervisor) Close() error {
	errs := make([]error, 0, 2)

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
		"capture_mode", "stdout-first with active log fallback",
		"log_path", s.cfg.LogPath,
		"audit_log_path", s.cfg.AuditLogPath,
		"score_threshold", s.cfg.ScoreThreshold,
		"cache_ttl", s.cfg.CacheTTL.String(),
		"cache_flush_interval", s.cfg.CacheFlushInterval.String(),
	)

	go s.scanOutput(runCtx, stdout, os.Stdout, "stdout", true, serverInput)
	go s.scanOutput(runCtx, stderr, os.Stderr, "stderr", true, serverInput)
	go s.forwardConsoleInput(runCtx, serverInput)
	go s.monitorLogFile(runCtx, serverInput)

	err = command.Wait()
	cancel()
	return err
}

func openAuditLogger(cfg Config) (*slog.Logger, io.Closer, error) {
	path := strings.TrimSpace(cfg.AuditLogPath)
	if path == "" {
		return nil, nil, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}

	logger := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	return logger, file, nil
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
		if stat.Size() < offset {
			offset = 0
		}

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
	if slot, ok := parseClientDisconnect(line); ok {
		s.clearConnectionState(slot)
		return
	}

	slot, addr, playerName, ok := parseClientConnect(line)
	if ok {
		s.storeConnectionState(slot, addr, playerName)
		s.processConnectionEvent(ctx, stdin, source, slot, addr, playerName)
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

	s.processConnectionEvent(ctx, stdin, source, slot, addr, playerName)
}

func (s *Supervisor) processConnectionEvent(ctx context.Context, stdin io.Writer, source, slot string, addr netip.Addr, playerName string) {
	eventKey := slot + "|" + addr.String()
	if !s.markEvent(eventKey) {
		return
	}

	select {
	case s.checkSlots <- struct{}{}:
	case <-ctx.Done():
		return
	}

	go func() {
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
			fields := append([]any{"event_source", source, "slot", slot, "player", playerName}, DecisionLogFields(decision)...)
			s.logger.Log(ctx, level, "anti-vpn decision", fields...)
		}

		s.broadcastDecision(stdin, slot, playerName, decision)

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

	fields := []any{
		"event", "decision",
		"event_source", source,
		"slot", slot,
		"ip", addr.String(),
		"action", decisionAction(decision),
	}
	fields = append(fields, DecisionLogFields(decision)...)
	s.auditLogger.Info("anti-vpn audit", fields...)
}

func (s *Supervisor) broadcastDecision(stdin io.Writer, slot, playerName string, decision Decision) {
	if !s.shouldBroadcast(slot, decision) {
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

	if _, err := fmt.Fprintln(stdin, command); err != nil {
		s.logger.Warn("anti-vpn broadcast command failed", "slot", slot, "player", playerName, "command", command, "error", err)
		if s.auditLogger != nil {
			s.auditLogger.Warn("anti-vpn audit", "event", "broadcast", "status", "failed", "slot", slot, "player", sanitizePlayerName(playerName), "command", command, "summary", publicSummary, "error", err)
		}
		return
	}

	s.logger.Info("anti-vpn broadcast command sent", "slot", slot, "player", playerName, "command", command, "summary", publicSummary)
	if s.auditLogger != nil {
		s.auditLogger.Info("anti-vpn audit", "event", "broadcast", "status", "sent", "slot", slot, "player", sanitizePlayerName(playerName), "command", command, "summary", publicSummary)
	}
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

func (s *Supervisor) shouldBroadcast(slot string, decision Decision) bool {
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
	key := slot + "|" + decision.IP + "|" + decisionAction(decision)

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
