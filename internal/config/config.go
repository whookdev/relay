package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port     int
	Host     string
	ServerID string

	RedisURL string

	WSPort int

	HealthCheckInterval int
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
		Port:                port,
		Host:                getEnvWithDefault("HOST", "0.0.0.0"),
		ServerID:            requireEnv("SERVER_ID"),
		RedisURL:            requireEnv("REDIS_URL"),
		WSPort:              wsPort,
		HealthCheckInterval: 30,
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
