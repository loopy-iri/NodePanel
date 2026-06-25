package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/pasarguard/panel/internal/webhook"
)

// webhookSink captures delivered events and validates their HMAC signatures.
type webhookSink struct {
	mu     sync.Mutex
	secret string
	events []webhook.Envelope
	badSig int
}

func newWebhookSink(secret string) (*webhookSink, *httptest.Server) {
	s := &webhookSink{secret: secret}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte(s.secret))
		mac.Write(body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))

		s.mu.Lock()
		defer s.mu.Unlock()
		if r.Header.Get("X-PG-Signature") != want {
			s.badSig++
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var env webhook.Envelope
		_ = json.Unmarshal(body, &env)
		s.events = append(s.events, env)
		w.WriteHeader(http.StatusOK)
	}))
	return s, srv
}

func (s *webhookSink) types() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.events))
	for i, e := range s.events {
		out[i] = e.Type
	}
	return out
}

func TestWebhookUsageAndOverQuotaEvents(t *testing.T) {
	const masterKey = "node-master"

	// usage 120 against a quota of 100 -> over quota.
	_, nodeSrv := newFakeNode(masterKey, 120)
	defer nodeSrv.Close()

	sink, sinkSrv := newWebhookSink("wh-secret")
	defer sinkSrv.Close()

	a := newTestAPI(t)
	h := a.Router()

	// Register webhook destination.
	if rr := call(t, h, http.MethodPost, "/api/v1/webhooks", map[string]any{
		"url": sinkSrv.URL, "secret": "wh-secret", "events": "*",
	}); rr.Code != http.StatusCreated {
		t.Fatalf("create webhook: %d body=%s", rr.Code, rr.Body.String())
	}

	// Register node, customer, a tiny 100-byte plan, and subscribe.
	node := decode[map[string]any](t, call(t, h, http.MethodPost, "/api/v1/nodes", map[string]any{
		"name": "n1", "address": nodeSrv.URL, "master_key": masterKey,
	}))
	cust := decode[map[string]any](t, call(t, h, http.MethodPost, "/api/v1/customers", map[string]any{"name": "acme"}))
	plan := decode[map[string]any](t, call(t, h, http.MethodPost, "/api/v1/plans", map[string]any{
		"name": "tiny", "quota_bytes": 100, "duration_days": 30,
	}))
	if rr := call(t, h, http.MethodPost, "/api/v1/customers/"+cust["id"].(string)+"/subscriptions", map[string]any{
		"plan_id": plan["id"].(string), "node_id": node["id"].(string),
	}); rr.Code != http.StatusCreated {
		t.Fatalf("create subscription: %d body=%s", rr.Code, rr.Body.String())
	}

	// Collect: usage 120/100 -> over_quota event fires once.
	a.collectUsageOnce(context.Background())
	a.collectUsageOnce(context.Background()) // second pass must not re-fire

	types := sink.types()
	overCount := 0
	for _, ty := range types {
		if ty == webhook.EventUsageOverQuota {
			overCount++
		}
	}
	if overCount != 1 {
		t.Fatalf("expected exactly one over_quota event, got %d (all: %v)", overCount, types)
	}
	if sink.badSig != 0 {
		t.Fatalf("got %d deliveries with bad signature", sink.badSig)
	}

	// Verify the over_quota payload carries overage = used - quota = 20.
	sink.mu.Lock()
	defer sink.mu.Unlock()
	var found bool
	for _, e := range sink.events {
		if e.Type != webhook.EventUsageOverQuota {
			continue
		}
		data, _ := e.Data.(map[string]any)
		if int64(data["overage_bytes"].(float64)) != 20 {
			t.Fatalf("expected overage 20, got %v", data["overage_bytes"])
		}
		found = true
	}
	if !found {
		t.Fatal("over_quota event payload not found")
	}
}
