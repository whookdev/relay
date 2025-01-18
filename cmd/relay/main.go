package main

import (
	"context"
	"fmt"
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

	if err := initiateApp(logger); err != nil {
		logger.Error("error in app lifecycle", "error", err)
		os.Exit(1)
	}
}

func initiateApp(logger *slog.Logger) error {
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	rdb, err := redis.New(cfg, logger)
	if err != nil {
		return fmt.Errorf("creating redis client: %w", err)
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
		return fmt.Errorf("connecting to redis server: %w", err)
	}

	lc, err := lifecycle.New(cfg, rdb.Client, logger)
	if err != nil {
		return fmt.Errorf("creating lifecycle: %w", err)
	}

	if err = lc.RegisterWithConductor(); err != nil {
		return fmt.Errorf("registering with conductor: %w", err)
	}

	go lc.MaintainRegistration(ctx)

	srv, err := server.New(cfg, logger)
	if err != nil {
		return fmt.Errorf("creating server: %w", err)
	}

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("starting server: %w", err)
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

	return nil
}
