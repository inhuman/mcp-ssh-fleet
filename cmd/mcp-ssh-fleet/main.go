package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/inhuman/mcp-ssh-fleet/internal/config"
	"github.com/inhuman/mcp-ssh-fleet/internal/inventory"
	"github.com/inhuman/mcp-ssh-fleet/internal/sshx"
	"github.com/inhuman/mcp-ssh-fleet/internal/tools"
	"github.com/inhuman/mcp-ssh-fleet/internal/transport"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

const serverName = "mcp-ssh-fleet"

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer func() { _ = log.Sync() }()

	if err := run(log); err != nil {
		log.Fatal("fatal", zap.Error(err))
	}
}

func run(log *zap.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	inv, err := inventory.Load(cfg.InventoryPath)
	if err != nil {
		return err
	}
	log.Info("inventory loaded", zap.Int("hosts", inv.Len()))

	client, err := sshx.New(cfg.KeyPath, cfg.OutputCapBytes, time.Duration(cfg.CmdTimeoutS)*time.Second)
	if err != nil {
		return err
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: version}, nil)
	tools.NewProbe(inv, client, cfg.ProbeConcurrency, cfg.ProbeMaxHosts, log).Register(srv)
	tools.NewExec(inv, client, log).Register(srv)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return transport.Serve(ctx, cfg, srv, log)
}
