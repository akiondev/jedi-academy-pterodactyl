package antivpn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// AddonRunnerConfig is the operator-facing configuration for the
// supervisor's event-driven addon runner. Values are populated from
// environment variables in config.go.
type AddonRunnerConfig struct {
	// Enabled gates the entire event-driven addon subsystem. When
	// false the supervisor still parses and dispatches events to
	// built-in modules but never spawns external addon child
	// processes.
	Enabled bool
	// AddonsDir is the directory scanned for executable event-driven
	// addons. Each top-level `.sh` / `.py` file is launched once at
	// supervisor startup and receives parsed events as NDJSON on
	// stdin. The legacy run-once startup helpers in /home/container/
	// addons are NOT scanned here.
	AddonsDir string
	// BufferSize is the per-handler event-bus channel capacity. When
	// an addon stops reading its stdin, events queued for that addon
	// are bounded by this value.
	BufferSize int
	// DropPolicy selects what happens when an addon falls behind and
	// its queue fills up. See EventDispatchPolicy for the semantics.
	DropPolicy EventDispatchPolicy
}

// AddonRunner manages the lifecycle of supervisor-launched event-driven
// addon child processes. Each addon is registered as an EventHandler
// with the central dispatcher: parsed events are encoded as NDJSON and
// written to the addon's stdin. The addon's stdout/stderr are
// line-prefixed and forwarded to the runtime console so operators can
// see what each addon is doing without having to find a separate log.
//
// Crash isolation: a panicked / closed-stdin / killed addon is removed
// from the dispatcher; the supervisor and the other addons keep
// running.
type AddonRunner struct {
	cfg    AddonRunnerConfig
	logger *slog.Logger

	mu         sync.Mutex
	addons     []*runningAddon
	dispatcher *EventDispatcher
	startCtx   context.Context //nolint:containedctx // lifecycle context propagated from Run
}

type runningAddon struct {
	name        string
	path        string
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdinClosed bool
	stdinMu     sync.Mutex
	logger      *slog.Logger
	cancel      context.CancelFunc
	unsubscribe func()
	exited      chan struct{}
}

// NewAddonRunner constructs an addon runner. It does not yet spawn any
// child processes; call Start with a dispatcher to begin.
func NewAddonRunner(cfg AddonRunnerConfig, logger *slog.Logger) *AddonRunner {
	return &AddonRunner{
		cfg:    cfg,
		logger: logger,
	}
}

// Start scans the configured addons directory and launches every
// matching addon as a child process subscribed to the dispatcher. It
// returns nil if event-driven addons are disabled or the directory
// does not exist; this is intentional so the supervisor still starts
// cleanly on installs that do not use addons.
func (r *AddonRunner) Start(ctx context.Context, dispatcher *EventDispatcher) error {
	if !r.cfg.Enabled {
		if r.logger != nil {
			r.logger.Info("addon event bus disabled, skipping event addon runner")
		}
		return nil
	}
	if dispatcher == nil {
		return errors.New("addon runner requires a non-nil dispatcher")
	}
	if strings.TrimSpace(r.cfg.AddonsDir) == "" {
		return nil
	}

	entries, err := listAddonScripts(r.cfg.AddonsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if r.logger != nil {
				r.logger.Info("event addon directory not present; nothing to launch", "dir", r.cfg.AddonsDir)
			}
			return nil
		}
		return fmt.Errorf("scan event addon directory %q: %w", r.cfg.AddonsDir, err)
	}

	r.mu.Lock()
	r.dispatcher = dispatcher
	r.startCtx = ctx
	r.mu.Unlock()

	if len(entries) == 0 {
		if r.logger != nil {
			r.logger.Info("no event addons present", "dir", r.cfg.AddonsDir)
		}
		return nil
	}

	for _, path := range entries {
		if err := r.launch(ctx, path); err != nil {
			if r.logger != nil {
				r.logger.Warn("event addon failed to launch", "path", path, "error", err)
			}
		}
	}
	return nil
}

// listAddonScripts returns the absolute paths of every executable
// addon script in dir, sorted lexicographically. The set of supported
// suffixes is intentionally narrow (`.sh`, `.py`) and matches the
// existing run-once addon loader convention so operators only have to
// learn one rule.
func listAddonScripts(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".sh", ".py":
		default:
			continue
		}
		if strings.HasSuffix(name, ".disable") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	sort.Strings(out)
	return out, nil
}

func (r *AddonRunner) launch(ctx context.Context, path string) error {
	addonCtx, cancel := context.WithCancel(ctx)

	args := addonInvocation(path)
	cmd := exec.CommandContext(addonCtx, args[0], args[1:]...)
	cmd.Dir = "/home/container"
	cmd.Env = append(os.Environ(), "JKA_ADDON_EVENT_BUS=stdin-ndjson")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("addon stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("addon stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("addon stderr pipe: %w", err)
	}

	// Place the child in its own process group so we can deliver a
	// graceful signal without affecting the parent or sibling addons.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start addon: %w", err)
	}

	name := filepath.Base(path)
	addon := &runningAddon{
		name:   name,
		path:   path,
		cmd:    cmd,
		stdin:  stdin,
		logger: r.logger,
		cancel: cancel,
		exited: make(chan struct{}),
	}

	go addon.forwardOutput(stdout, "stdout")
	go addon.forwardOutput(stderr, "stderr")
	go addon.waitForExit(r)

	r.mu.Lock()
	r.addons = append(r.addons, addon)
	dispatcher := r.dispatcher
	r.mu.Unlock()

	if dispatcher != nil {
		addon.unsubscribe = dispatcher.Subscribe(ctx, addon)
	}

	if r.logger != nil {
		r.logger.Info("event addon started", "name", name, "pid", cmd.Process.Pid)
	}
	return nil
}

// addonInvocation chooses how to execute the addon based on file
// extension. The current rules mirror the legacy run-once helper
// loader so operators only need to memorise one convention.
func addonInvocation(path string) []string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py":
		return []string{"python3", path}
	default:
		return []string{"bash", path}
	}
}

// Name implements EventHandler.
func (a *runningAddon) Name() string { return a.name }

// HandleEvent implements EventHandler. It encodes the event as NDJSON
// and writes it to the addon's stdin. A failed write closes stdin and
// causes the addon to be removed on the next exit.
func (a *runningAddon) HandleEvent(_ context.Context, ev Event) {
	encoded, err := ev.MarshalNDJSON()
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("addon event encode failed", "addon", a.name, "type", ev.Type, "error", err)
		}
		return
	}

	a.stdinMu.Lock()
	defer a.stdinMu.Unlock()
	if a.stdinClosed {
		return
	}
	if _, err := a.stdin.Write(encoded); err != nil {
		// Closed stdin / dead child: stop trying so we do not log on
		// every event. The addon will be cleaned up by the wait
		// goroutine.
		a.stdinClosed = true
		_ = a.stdin.Close()
		if a.logger != nil {
			a.logger.Warn("addon stdin write failed; closing addon feed", "addon", a.name, "error", err)
		}
	}
}

func (a *runningAddon) forwardOutput(stream io.ReadCloser, label string) {
	defer stream.Close()
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	prefix := fmt.Sprintf("[addon:%s:%s] ", a.name, label)

	dest := os.Stdout
	if label == "stderr" {
		dest = os.Stderr
	}
	for scanner.Scan() {
		fmt.Fprintln(dest, prefix+scanner.Text())
	}
}

func (a *runningAddon) waitForExit(r *AddonRunner) {
	err := a.cmd.Wait()
	a.stdinMu.Lock()
	a.stdinClosed = true
	a.stdinMu.Unlock()

	if a.unsubscribe != nil {
		a.unsubscribe()
	}
	a.cancel()
	close(a.exited)

	if r != nil && r.logger != nil {
		if err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Warn("event addon exited", "addon", a.name, "error", err)
		} else {
			r.logger.Info("event addon exited", "addon", a.name)
		}
	}

	if r != nil {
		r.mu.Lock()
		for i, candidate := range r.addons {
			if candidate == a {
				r.addons = append(r.addons[:i], r.addons[i+1:]...)
				break
			}
		}
		r.mu.Unlock()
	}
}

// Stop signals every running addon to terminate gracefully and waits
// (briefly) for them to exit. Stop is safe to call from a deferred
// shutdown path; addons that do not exit on their own are killed when
// the parent context is cancelled.
func (r *AddonRunner) Stop(timeout time.Duration) {
	r.mu.Lock()
	addons := make([]*runningAddon, len(r.addons))
	copy(addons, r.addons)
	r.mu.Unlock()

	if len(addons) == 0 {
		return
	}

	for _, addon := range addons {
		addon.stdinMu.Lock()
		if !addon.stdinClosed {
			addon.stdinClosed = true
			_ = addon.stdin.Close()
		}
		addon.stdinMu.Unlock()
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for _, addon := range addons {
		select {
		case <-addon.exited:
		case <-deadline.C:
			// Time is up: cancel the addon's context which kills the
			// process via exec.CommandContext.
			addon.cancel()
		}
	}
}
