package redis

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
	redisi "github.com/redis/go-redis/v9"
	"github.com/whookdev/relay/internal/config"
)

type RedisServer struct {
	cfg    *config.Config
	Client *redisi.Client
	logger *slog.Logger
}

func New(cfg *config.Config, logger *slog.Logger) (*RedisServer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	logger = logger.With("component", "redis")

	rs := &RedisServer{
		cfg:    cfg,
		logger: logger,
	}

	return rs, nil
}

func (rs *RedisServer) Start(ctx context.Context) error {
	rs.Client = redis.NewClient(&redis.Options{
		Addr:     rs.cfg.RedisURL,
		Password: "",
		DB:       0,
	})

	if err := rs.Client.Ping(ctx).Err(); err != nil {
		rs.logger.Error("failed to connect to redis", "error", err)
		return err
	}

	rs.logger.Info("redis connection established successfully", "addr", rs.cfg.RedisURL)
	return nil
}

func (rs *RedisServer) Stop() error {
	if rs.Client != nil {
		if err := rs.Client.Close(); err != nil {
			rs.logger.Error("failed to close redis connection", "error", err)
			return fmt.Errorf("failed to close redis connection: %w", err)
		}
		rs.logger.Info("redis connection closed successfully")
	}
	return nil
}
