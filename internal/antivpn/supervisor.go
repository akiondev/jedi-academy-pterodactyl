package antivpn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Supervisor struct {
	cfg           Config
	logger        *slog.Logger
	auditLogger   *slog.Logger
	auditCloser   io.Closer
	engine        *Engine
	commandMu     sync.Mutex
	seenMu        sync.Mutex
	seenEvents    map[string]time.Time
	checkSlots    chan struct{}
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
		cfg:         cfg,
		logger:      logger,
		auditLogger: auditLogger,
		auditCloser: auditCloser,
		engine:      engine,
		seenEvents:  make(map[string]time.Time),
		checkSlots:  make(chan struct{}, 8),
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
		"capture_mode", "stdout-first with server.log fallback",
		"log_path", s.cfg.LogPath,
		"audit_log_path", s.cfg.AuditLogPath,
		"score_threshold", s.cfg.ScoreThreshold,
		"cache_ttl", s.cfg.CacheTTL.String(),
		"cache_flush_interval", s.cfg.CacheFlushInterval.String(),
	)

	go s.scanOutput(runCtx, stdout, os.Stdout, "stdout", true, serverInput)
	go s.scanOutput(runCtx, stderr, os.Stderr, "stderr", false, serverInput)
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

		if _, err := fmt.Fprintln(stdin, scanner.Text()); err != nil {
			s.logger.Warn("anti-vpn stdin forwarding failed", "error", err)
			return
		}
	}

	if err := scanner.Err(); err != nil {
		s.logger.Warn("anti-vpn stdin scanner failed", "error", err)
	}
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
			s.handleLogLine(ctx, stdin, line, "server.log")
		}
		file.Close()

		if !readLine {
			time.Sleep(s.cfg.LogPollInterval)
		}
	}
}

func (s *Supervisor) handleLogLine(ctx context.Context, stdin io.Writer, line string, source string) {
	slot, addr, ok := parseClientUserinfoChanged(line)
	if !ok {
		return
	}

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
			fields := append([]any{"event_source", source, "slot", slot}, DecisionLogFields(decision)...)
			s.logger.Log(ctx, level, "anti-vpn decision", fields...)
		}

		if decision.Blocked {
			s.enforceDecision(stdin, slot, addr, decision)
		}
	}()
}

func (s *Supervisor) enforceDecision(stdin io.Writer, slot string, addr netip.Addr, decision Decision) {
	ipText := addr.String()
	commands := make([]string, 0, 2)

	if strings.TrimSpace(s.cfg.BanCommand) != "" {
		commands = append(commands, fillCommandTemplate(s.cfg.BanCommand, ipText, slot))
	}
	if slot != "" && strings.TrimSpace(s.cfg.KickCommand) != "" {
		commands = append(commands, fillCommandTemplate(s.cfg.KickCommand, ipText, slot))
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

func (s *Supervisor) markEvent(key string) bool {
	now := time.Now().UTC()

	s.seenMu.Lock()
	defer s.seenMu.Unlock()

	for current, seenAt := range s.seenEvents {
		if now.Sub(seenAt) > 5*time.Minute {
			delete(s.seenEvents, current)
		}
	}

	if seenAt, exists := s.seenEvents[key]; exists && now.Sub(seenAt) < 90*time.Second {
		return false
	}

	s.seenEvents[key] = now
	return true
}

func parseClientUserinfoChanged(line string) (string, netip.Addr, bool) {
	const prefix = "ClientUserinfoChanged:"

	index := strings.Index(line, prefix)
	if index == -1 {
		return "", netip.Addr{}, false
	}

	rest := strings.TrimSpace(line[index+len(prefix):])
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", netip.Addr{}, false
	}
	slot := fields[0]

	ipIndex := strings.Index(line, `\ip\`)
	if ipIndex == -1 {
		return "", netip.Addr{}, false
	}

	raw := line[ipIndex+len(`\ip\`):]
	end := strings.Index(raw, `\`)
	if end == -1 {
		return "", netip.Addr{}, false
	}

	addr, err := parseServerIPField(raw[:end])
	if err != nil {
		return "", netip.Addr{}, false
	}

	return slot, addr, true
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

func fillCommandTemplate(template, ip, slot string) string {
	command := strings.ReplaceAll(template, "%IP%", ip)
	command = strings.ReplaceAll(command, "%SLOT%", slot)
	return strings.TrimSpace(command)
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
