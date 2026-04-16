package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/akiondev/jedi-academy-pterodactyl/internal/antivpn"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := run(ctx, logger, os.Args[1:]); err != nil {
		logger.Error("anti-vpn command failed", "error", err)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, args []string) error {
	if len(args) == 0 {
		return usageError("missing subcommand")
	}

	switch args[0] {
	case "check":
		return runCheck(ctx, logger, args[1:])
	case "supervise":
		return runSupervise(ctx, logger, args[1:])
	default:
		return usageError(fmt.Sprintf("unknown subcommand %q", args[0]))
	}
}

func runCheck(ctx context.Context, logger *slog.Logger, args []string) error {
	if len(args) != 1 {
		return usageError("check requires exactly one IP address")
	}

	ip, err := netip.ParseAddr(args[0])
	if err != nil {
		return fmt.Errorf("parse IP %q: %w", args[0], err)
	}

	cfg, err := antivpn.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	engine, err := antivpn.NewEngine(cfg, logger)
	if err != nil {
		return err
	}

	decision, err := engine.CheckIP(ctx, ip)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(decision); err != nil {
		return fmt.Errorf("encode decision: %w", err)
	}

	if decision.Blocked {
		return fmt.Errorf("IP %s would be blocked in mode %s", decision.IP, decision.Mode)
	}
	return nil
}

func runSupervise(ctx context.Context, logger *slog.Logger, args []string) error {
	if len(args) == 0 {
		return usageError("supervise requires a server command after --")
	}
	if args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return usageError("supervise requires a server command after --")
	}

	cfg, err := antivpn.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	supervisor, err := antivpn.NewSupervisor(cfg, logger)
	if err != nil {
		return err
	}

	return supervisor.Run(ctx, args)
}

func usageError(message string) error {
	return fmt.Errorf("%s\nusage:\n  taystjk-antivpn check <ip>\n  taystjk-antivpn supervise -- <server command...>", message)
}
