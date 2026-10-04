// Package config reads env once at boot and fails fast on bad values.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port            string
	Env             string // Railway environment name, "development" locally
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Port: getenv("PORT", "8080"),
		Env:  getenv("RAILWAY_ENVIRONMENT_NAME", "development"),
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("LOG_LEVEL", "info"))); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	secs, err := strconv.Atoi(getenv("SHUTDOWN_TIMEOUT_SECONDS", "20"))
	if err != nil || secs <= 0 {
		return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT_SECONDS: want a positive integer, got %q", os.Getenv("SHUTDOWN_TIMEOUT_SECONDS"))
	}
	cfg.ShutdownTimeout = time.Duration(secs) * time.Second

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
