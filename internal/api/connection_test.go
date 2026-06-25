package api

import (
	"strings"
	"testing"
)

func TestInboundsFromConfigStripsRealityPrivateKey(t *testing.T) {
	cfg := `{"inbounds":[{"tag":"vless-in","port":443,"protocol":"vless",
		"streamSettings":{"security":"reality","realitySettings":{"privateKey":"SECRET","serverNames":["x.com"]}}}],
		"outbounds":[{"protocol":"freedom"}]}`
	out := inboundsFromConfig(cfg)
	if out == nil {
		t.Fatal("expected inbounds, got nil")
	}
	s := string(out)
	if strings.Contains(s, "SECRET") {
		t.Fatalf("private key leaked: %s", s)
	}
	if !strings.Contains(s, "vless-in") || strings.Contains(s, "freedom") {
		t.Fatalf("unexpected content (should keep inbounds, drop outbounds): %s", s)
	}
	if inboundsFromConfig("") != nil || inboundsFromConfig(`{"inbounds":[]}`) != nil {
		t.Fatal("empty config should yield nil (no fallback)")
	}
}

func TestGRPCAddressFor(t *testing.T) {
	cases := []struct {
		addr string
		port int
		want string
	}{
		{"https://1.2.3.4:8090", 62050, "1.2.3.4:62050"},
		{"http://node.example", 62051, "node.example:62051"},
		{"1.2.3.4:8090", 0, "1.2.3.4:62050"},
		{"1.2.3.4", 62050, "1.2.3.4:62050"},
	}
	for _, c := range cases {
		if got := grpcAddressFor(c.addr, c.port); got != c.want {
			t.Errorf("grpcAddressFor(%q,%d) = %q, want %q", c.addr, c.port, got, c.want)
		}
	}
}
