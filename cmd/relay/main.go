package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/whookdev/relay/internal/config"
	"github.com/whookdev/relay/internal/lifecycle"
	"github.com/whookdev/relay/internal/redis"
	"github.com/whookdev/relay/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.NewConfig()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger = logger.With("server_id", cfg.ServerID)

	rdb, err := redis.New(cfg, logger)
	if err != nil {
		logger.Error("failed to create redis client", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigChan
		logger.Info("received shutdown signal", "signal", sig)
		cancel()
		time.Sleep(100 * time.Millisecond) // Give time for cleanup logs to flush
	}()

	if err := rdb.Start(ctx); err != nil {
		logger.Error("unable to connect to redis server", "error", err)
		os.Exit(1)
	}

	lc, err := lifecycle.New(cfg, rdb.Client, logger)
	if err != nil {
		logger.Error("failed to create tunnel coordinator", "error", err)
		os.Exit(1)
	}

	if err = lc.RegisterWithConductor(); err != nil {
		logger.Error("failed to register with conductor", "error", err)
		os.Exit(1)
	}

	go lc.MaintainRegistration(ctx)

	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	if err := srv.Start(ctx); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()
	logger.Info("starting graceful shutdown")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	tunnelCleanup := make(chan struct{})
	go func() {
		defer close(tunnelCleanup)
		time.Sleep(100 * time.Millisecond)
	}()

	select {
	case <-tunnelCleanup:
		logger.Info("tunnel cleanup complete")
	case <-shutdownCtx.Done():
		logger.Error("tunnel cleanup timed out")
	}

	if err := rdb.Stop(); err != nil {
		logger.Error("error stopping redis", "error", err)
	}

	logger.Info("shutdown complete")
}
