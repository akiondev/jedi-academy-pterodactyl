package antivpn

import (
	"bufio"
	"context"
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

	return &Supervisor{
		cfg:        cfg,
		logger:     logger,
		engine:     engine,
		seenEvents: make(map[string]time.Time),
		checkSlots: make(chan struct{}, 8),
	}, nil
}

func (s *Supervisor) Run(ctx context.Context, serverCommand []string) error {
	if len(serverCommand) == 0 {
		return fmt.Errorf("no server command provided to supervisor")
	}

	command := exec.CommandContext(ctx, serverCommand[0], serverCommand[1:]...)
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
		"log_path", s.cfg.LogPath,
		"score_threshold", s.cfg.ScoreThreshold,
		"cache_ttl", s.cfg.CacheTTL.String(),
	)

	go func() {
		_, _ = io.Copy(os.Stdout, stdout)
	}()
	go func() {
		_, _ = io.Copy(os.Stderr, stderr)
	}()
	go s.forwardConsoleInput(ctx, serverInput)
	go s.monitorLogFile(ctx, serverInput)

	return command.Wait()
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
			s.handleLogLine(ctx, stdin, line)
		}
		file.Close()

		if !readLine {
			time.Sleep(s.cfg.LogPollInterval)
		}
	}
}

func (s *Supervisor) handleLogLine(ctx context.Context, stdin io.Writer, line string) {
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

		if s.cfg.LogDecisions {
			level := slog.LevelInfo
			if decision.Blocked || decision.WouldBlock {
				level = slog.LevelWarn
			}
			s.logger.Log(ctx, level, "anti-vpn decision", DecisionLogFields(decision)...)
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
			return
		}
		s.logger.Warn("anti-vpn enforcement command sent", "ip", ipText, "slot", slot, "command", command, "summary", decision.Summary)
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
