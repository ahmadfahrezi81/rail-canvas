// Package config reads env once at boot and fails fast on bad values.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type Config struct {
	Port            string
	Env             string // Railway environment name, "development" locally
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	DatabaseURL     string    // connects as the app role
	DevSpaceID      uuid.UUID // stand-in tenant until login (Step 5); uuid.Nil when unset
}

func Load() (Config, error) {
	cfg := Config{
		Port:        getenv("PORT", "8080"),
		Env:         getenv("RAILWAY_ENVIRONMENT_NAME", "development"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("LOG_LEVEL", "info"))); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	secs, err := strconv.Atoi(getenv("SHUTDOWN_TIMEOUT_SECONDS", "20"))
	if err != nil || secs <= 0 {
		return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT_SECONDS: want a positive integer, got %q", os.Getenv("SHUTDOWN_TIMEOUT_SECONDS"))
	}
	cfg.ShutdownTimeout = time.Duration(secs) * time.Second

	if v := os.Getenv("DEV_SPACE_ID"); v != "" {
		if cfg.DevSpaceID, err = uuid.Parse(v); err != nil {
			return Config{}, fmt.Errorf("DEV_SPACE_ID: %w", err)
		}
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
