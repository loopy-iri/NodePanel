package config

import (
	"os"
	"time"
)

// Config holds the panel runtime configuration, loaded from environment.
// The panel has no money/wallet logic; billing lives in the external sales bot.
type Config struct {
	HTTPAddr        string        // address the public/admin API listens on
	DBPath          string        // path to the SQLite database file
	APIToken        string        // bearer token required for /api/* routes
	CollectInterval time.Duration // usage collector polling interval
}

func Load() *Config {
	return &Config{
		HTTPAddr:        env("PANEL_HTTP_ADDR", ":8080"),
		DBPath:          env("PANEL_DB_PATH", "panel.db"),
		APIToken:        env("PANEL_API_TOKEN", "dev-token-change-me"),
		CollectInterval: envDuration("PANEL_COLLECT_INTERVAL", 30*time.Second),
	}
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
