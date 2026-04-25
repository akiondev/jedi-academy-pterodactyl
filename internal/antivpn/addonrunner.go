package antivpn

import (
	"bufio"
	"context"
	"encoding/json"
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
	// DefaultsDir is the directory containing bundled default addon
	// scripts (typically /home/container/addons/defaults). The
	// supervisor never scans this directory blindly; it iterates the
	// "addons" map in ConfigPath and resolves each enabled addon's
	// "script" field relative to this directory.
	DefaultsDir string
	// ConfigPath is the absolute path to /home/container/config/
	// jka-addons.json. The runner reads the file at Start() time;
	// missing or invalid files result in zero addons being launched
	// (and a warning being logged) rather than a fatal error so the
	// supervisor still starts.
	ConfigPath string
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

// addonSpec is a single entry resolved from jka-addons.json. Only
// enabled addons whose "type" is "event" are launched by the
// supervisor; scheduled-type addons are owned by the shell layer.
type addonSpec struct {
	Name       string
	Path       string
	ConfigJSON []byte
}

// NewAddonRunner constructs an addon runner. It does not yet spawn any
// child processes; call Start with a dispatcher to begin.
func NewAddonRunner(cfg AddonRunnerConfig, logger *slog.Logger) *AddonRunner {
	return &AddonRunner{
		cfg:    cfg,
		logger: logger,
	}
}

// Start reads the jka-addons.json file at ConfigPath, picks every
// enabled addon whose "type" is "event", and launches each one as a
// child process subscribed to the dispatcher. It returns nil if event-
// driven addons are disabled or no enabled event addons are
// configured; this is intentional so the supervisor still starts
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

	specs, err := loadEnabledEventAddons(r.cfg.ConfigPath, r.cfg.DefaultsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if r.logger != nil {
				r.logger.Info("jka-addons.json not present; nothing to launch", "path", r.cfg.ConfigPath)
			}
			return nil
		}
		if r.logger != nil {
			r.logger.Warn("failed to load jka-addons.json; skipping event addons", "path", r.cfg.ConfigPath, "error", err)
		}
		return nil
	}

	r.mu.Lock()
	r.dispatcher = dispatcher
	r.mu.Unlock()

	if len(specs) == 0 {
		if r.logger != nil {
			r.logger.Info("no enabled event addons in config", "path", r.cfg.ConfigPath)
		}
		return nil
	}

	for _, spec := range specs {
		if err := r.launch(ctx, spec); err != nil {
			if r.logger != nil {
				r.logger.Warn("event addon failed to launch", "name", spec.Name, "path", spec.Path, "error", err)
			}
		}
	}
	return nil
}

// loadEnabledEventAddons parses the central jka-addons.json file and
// returns one addonSpec per enabled "event"-type addon. The script
// field of each entry is resolved relative to defaultsDir. A safety
// check rejects scripts that escape the defaults directory.
func loadEnabledEventAddons(configPath, defaultsDir string) ([]addonSpec, error) {
	if strings.TrimSpace(configPath) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Addons map[string]json.RawMessage `json:"addons"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configPath, err)
	}

	type addonOrder struct {
		name  string
		order int
	}
	ordering := make([]addonOrder, 0, len(doc.Addons))
	specs := make(map[string]addonSpec, len(doc.Addons))

	defaultsAbs, err := filepath.Abs(strings.TrimSpace(defaultsDir))
	if err != nil {
		return nil, fmt.Errorf("resolve defaults dir: %w", err)
	}

	for name, rawSection := range doc.Addons {
		var section struct {
			Enabled *bool  `json:"enabled"`
			Type    string `json:"type"`
			Script  string `json:"script"`
			Order   *int   `json:"order"`
		}
		if err := json.Unmarshal(rawSection, &section); err != nil {
			return nil, fmt.Errorf("parse addon %q: %w", name, err)
		}
		if section.Enabled == nil || !*section.Enabled {
			continue
		}
		if strings.ToLower(strings.TrimSpace(section.Type)) != "event" {
			continue
		}
		script := strings.TrimSpace(section.Script)
		if script == "" {
			return nil, fmt.Errorf("addon %q is enabled but has no script", name)
		}
		// Reject any traversal: scripts must be a simple relative
		// name inside DefaultsDir.
		if filepath.IsAbs(script) || strings.Contains(script, "..") {
			return nil, fmt.Errorf("addon %q script %q must be a relative name inside the defaults directory", name, script)
		}
		full := filepath.Join(defaultsAbs, script)
		// Ensure the resolved path is still inside DefaultsDir.
		if rel, err := filepath.Rel(defaultsAbs, full); err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("addon %q script %q escapes defaults dir", name, script)
		}
		ord := 0
		if section.Order != nil {
			ord = *section.Order
		}
		ordering = append(ordering, addonOrder{name: name, order: ord})
		specs[name] = addonSpec{
			Name:       name,
			Path:       full,
			ConfigJSON: append([]byte(nil), rawSection...),
		}
	}

	sort.SliceStable(ordering, func(i, j int) bool {
		if ordering[i].order != ordering[j].order {
			return ordering[i].order < ordering[j].order
		}
		return ordering[i].name < ordering[j].name
	})

	out := make([]addonSpec, 0, len(ordering))
	for _, e := range ordering {
		out = append(out, specs[e.name])
	}
	return out, nil
}

func (r *AddonRunner) launch(ctx context.Context, spec addonSpec) error {
	if _, err := os.Stat(spec.Path); err != nil {
		return fmt.Errorf("addon script not found: %w", err)
	}
	addonCtx, cancel := context.WithCancel(ctx)

	args := addonInvocation(spec.Path)
	cmd := exec.CommandContext(addonCtx, args[0], args[1:]...)
	cmd.Dir = "/home/container"
	cmd.Env = append(os.Environ(),
		"JKA_ADDON_EVENT_BUS=stdin-ndjson",
		"JKA_ADDON_NAME="+spec.Name,
		"JKA_ADDON_CONFIG_JSON="+string(spec.ConfigJSON),
	)
	if strings.TrimSpace(r.cfg.ConfigPath) != "" {
		cmd.Env = append(cmd.Env, "JKA_ADDONS_CONFIG_PATH="+r.cfg.ConfigPath)
	}

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

	addon := &runningAddon{
		name:   spec.Name,
		path:   spec.Path,
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
		r.logger.Info("event addon started", "name", spec.Name, "path", spec.Path, "pid", cmd.Process.Pid)
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
