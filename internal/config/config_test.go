package config

import (
	"strings"
	"testing"
)

const goodToken = "a-sufficiently-long-panel-token-value"

// The API token is the only authentication in front of an API that returns node
// master keys, so the panel must refuse to start without a real one. The
// placeholder in particular is published in this repository: a panel serving it
// hands every node's master key to anyone who read the README.
func TestLoadRejectsMissingWeakOrPlaceholderToken(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"unset", "", "required"},
		{"placeholder", DefaultAPIToken, "placeholder"},
		{"too short", "short-token", "at least"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PANEL_API_TOKEN", tc.token)
			cfg, err := Load()
			if err == nil {
				t.Fatalf("Load accepted the %s token and returned %+v", tc.name, cfg)
			}
			if !strings.Contains(err.Error(), "PANEL_API_TOKEN") {
				t.Fatalf("error = %v, want it to name PANEL_API_TOKEN", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PANEL_API_TOKEN", goodToken)
	t.Setenv("PANEL_HTTP_ADDR", "")
	t.Setenv("PANEL_DB_PATH", "")
	t.Setenv("PANEL_COLLECT_INTERVAL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIToken != goodToken {
		t.Fatalf("APIToken = %q", cfg.APIToken)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DBPath != "panel.db" {
		t.Fatalf("DBPath = %q, want panel.db", cfg.DBPath)
	}
	if cfg.CollectInterval.String() != "30s" {
		t.Fatalf("CollectInterval = %s, want 30s", cfg.CollectInterval)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("PANEL_API_TOKEN", goodToken)
	t.Setenv("PANEL_HTTP_ADDR", ":9999")
	t.Setenv("PANEL_DB_PATH", "/tmp/custom.db")
	t.Setenv("PANEL_COLLECT_INTERVAL", "5m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":9999" || cfg.DBPath != "/tmp/custom.db" || cfg.CollectInterval.String() != "5m0s" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}

	// An unparseable duration falls back to the default rather than failing.
	t.Setenv("PANEL_COLLECT_INTERVAL", "not-a-duration")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CollectInterval.String() != "30s" {
		t.Fatalf("CollectInterval = %s, want the 30s fallback", cfg.CollectInterval)
	}
}
