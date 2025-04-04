package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port         int
	Host         string
	ServerPrefix string

	RedisURL    string
	RegistryKey string

	WSPort int

	HeartbeatInterval int
}

func NewConfig() (*Config, error) {
	godotenv.Load()
	port, err := strconv.Atoi(getEnvWithDefault("PORT", "3000"))
	if err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	wsPort, err := strconv.Atoi(getEnvWithDefault("WS_PORT", "3001"))
	if err != nil {
		return nil, fmt.Errorf("invalid websocket port: %w", err)
	}

	return &Config{
		Port:              port,
		Host:              getEnvWithDefault("HOST", "0.0.0.0"),
		ServerPrefix:      getEnvWithDefault("SERVER_PREFIX", "relay"),
		RedisURL:          requireEnv("REDIS_URL"),
		RegistryKey:       getEnvWithDefault("REGISTRY_KEY", "relay_servers"),
		WSPort:            wsPort,
		HeartbeatInterval: 15,
	}, nil
}

func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		panic(fmt.Sprintf("required environment variable %s is not set", key))
	}

	return val
}

func getEnvWithDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	return defaultValue
}
