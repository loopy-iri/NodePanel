package config

import (
	"fmt"
	"os"
	"time"
)

// DefaultAPIToken is the placeholder value shipped in the example config and
// the README. It is a well-known string in a public repository, so it is
// treated as "unset" and refused at startup — a running panel serving it would
// hand every node's master key to anyone who read the repo.
const DefaultAPIToken = "dev-token-change-me"

// MinAPITokenLength is the shortest accepted token. The token is the ONLY
// authentication in front of an API that returns node master keys, core keys,
// and certificates, so it must not be guessable.
const MinAPITokenLength = 24

// Config holds the panel runtime configuration, loaded from environment.
// The panel has no money/wallet logic; billing lives in the external sales bot.
type Config struct {
	HTTPAddr        string        // address the public/admin API listens on
	DBPath          string        // path to the SQLite database file
	APIToken        string        // bearer token required for /api/* routes
	CollectInterval time.Duration // usage collector polling interval
}

// Load reads the configuration and validates it. It returns an error rather
// than a half-usable Config: starting with no real credential is not a
// degraded mode, it is an open door.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:        env("PANEL_HTTP_ADDR", ":8080"),
		DBPath:          env("PANEL_DB_PATH", "panel.db"),
		APIToken:        os.Getenv("PANEL_API_TOKEN"),
		CollectInterval: envDuration("PANEL_COLLECT_INTERVAL", 30*time.Second),
	}

	switch {
	case cfg.APIToken == "":
		return nil, fmt.Errorf("PANEL_API_TOKEN is required; generate one with: openssl rand -hex 32")
	case cfg.APIToken == DefaultAPIToken:
		return nil, fmt.Errorf("PANEL_API_TOKEN is still the placeholder %q, which is public; generate one with: openssl rand -hex 32", DefaultAPIToken)
	case len(cfg.APIToken) < MinAPITokenLength:
		return nil, fmt.Errorf("PANEL_API_TOKEN must be at least %d characters; generate one with: openssl rand -hex 32", MinAPITokenLength)
	}

	return cfg, nil
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
