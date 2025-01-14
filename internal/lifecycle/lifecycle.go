package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/whookdev/relay/internal/config"
)

type Lifecycle struct {
	cfg    *config.Config
	logger *slog.Logger
	rdb    *redis.Client
}

type ServerInfo struct {
	Load          int       `json:"load"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

func New(cfg *config.Config, redis *redis.Client, logger *slog.Logger) (*Lifecycle, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if redis == nil {
		return nil, fmt.Errorf("redis client cannot be nil")
	}

	logger = logger.With("component", "lifecycle")

	lc := &Lifecycle{
		cfg:    cfg,
		rdb:    redis,
		logger: logger,
	}

	return lc, nil
}

func (lc *Lifecycle) RegisterWithConductor() error {
	info := &ServerInfo{
		Load:          0,
		LastHeartbeat: time.Now(),
	}

	val, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal server info: %w", err)
	}

	result := lc.rdb.HSet(context.Background(),
		"tunnel_servers",
		lc.cfg.ServerID,
		string(val),
	)
	if err := result.Err(); err != nil {
		return fmt.Errorf("failed to register with conductor: %w", err)
	}

	lc.logger.Info("registered with conductor", "info", info)
	return nil
}

func (lc *Lifecycle) MaintainRegistration(ctx context.Context) chan struct{} {
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		if err := lc.updateHeartbeat(); err != nil {
			lc.logger.Error("failed initial heartbeat", "error", err)
		}
		lc.logger.Info("heartbeat routine started")

		for {
			select {
			case <-ticker.C:
				if err := lc.updateHeartbeat(); err != nil {
					lc.logger.Error("failed heartbeat", "error", err)
				}
			case <-ctx.Done():
				lc.logger.Info("context cancelled, cleaning up tunnel registration")
				if err := lc.deregisterFromConductor(); err != nil {
					lc.logger.Error("failed to de-register from conductor", "error", err)
				} else {
					lc.logger.Info("de-registered from conductor")
				}
				lc.logger.Info("heartbeat routine stopped")
				return
			}
		}
	}()

	return done
}

func (lc *Lifecycle) deregisterFromConductor() error {
	result := lc.rdb.HDel(context.Background(),
		"tunnel_servers",
		lc.cfg.ServerID)
	if err := result.Err(); err != nil {
		return fmt.Errorf("failed to de-register from conductor: %w", err)
	}
	return nil
}

func (lc *Lifecycle) updateHeartbeat() error {
	info := &ServerInfo{
		Load:          0,
		LastHeartbeat: time.Now(),
	}

	val, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat info: %w", err)
	}

	result := lc.rdb.HSet(context.Background(),
		"tunnel_servers",
		lc.cfg.ServerID,
		string(val),
	)
	if err := result.Err(); err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	lc.logger.Debug("heartbeat update", "server_id", lc.cfg.ServerID, "info", info)
	return nil
}
