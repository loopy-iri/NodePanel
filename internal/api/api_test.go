package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/pasarguard/panel/internal/config"
	"github.com/pasarguard/panel/internal/nodeclient"
	"github.com/pasarguard/panel/internal/store"
)

const testToken = "test-token"

// fakeNode emulates a node agent's master admin API for panel tests.
type fakeNode struct {
	mu        sync.Mutex
	masterKey string
	created   map[string]nodeclient.CreateTenantRequest
	usedBytes int64
}

func newFakeNode(masterKey string, usedBytes int64) (*fakeNode, *httptest.Server) {
	f := &fakeNode{masterKey: masterKey, created: map[string]nodeclient.CreateTenantRequest{}, usedBytes: usedBytes}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(nodeclient.Health{Status: "ok", CoreStarted: true, CoreVersion: "26.3.27"})
	})
	mux.HandleFunc("/admin/config", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/admin/tenants", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != f.masterKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req nodeclient.CreateTenantRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.created[req.ID] = req
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(nodeclient.TenantView{ID: req.ID, Status: "active", QuotaBytes: req.QuotaBytes, ExpireAt: req.ExpireAt})
	})
	mux.HandleFunc("/admin/tenants/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != f.masterKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/usage"):
			_ = json.NewEncoder(w).Encode(nodeclient.UsageView{ID: "t", Status: "active", UsedBytes: f.usedBytes, QuotaBytes: 1000})
		default:
			_ = json.NewEncoder(w).Encode(nodeclient.TenantView{ID: "t", Status: "active"})
		}
	})
	return f, httptest.NewServer(mux)
}

func newTestAPI(t *testing.T) *API {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st, &config.Config{APIToken: testToken})
}

func call(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func decode[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %T: %v (body=%s)", v, err, rr.Body.String())
	}
	return v
}

func TestProvisioningFlow(t *testing.T) {
	const masterKey = "node-master"
	fake, srv := newFakeNode(masterKey, 5000)
	defer srv.Close()

	a := newTestAPI(t)
	h := a.Router()

	// Register the node (panel probes health).
	rr := call(t, h, http.MethodPost, "/api/v1/nodes", map[string]any{
		"name": "edge-1", "address": srv.URL, "master_key": masterKey,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("register node: %d body=%s", rr.Code, rr.Body.String())
	}
	node := decode[map[string]any](t, rr)
	if node["status"] != "online" {
		t.Fatalf("node should be online, got %v", node["status"])
	}
	if _, leaked := node["master_key"]; leaked {
		t.Fatal("master_key must not be exposed in API response")
	}
	nodeID := node["id"].(string)

	// Create a customer and a plan.
	cust := decode[map[string]any](t, call(t, h, http.MethodPost, "/api/v1/customers", map[string]any{"name": "acme"}))
	customerID := cust["id"].(string)
	plan := decode[map[string]any](t, call(t, h, http.MethodPost, "/api/v1/plans", map[string]any{
		"name": "100MB", "quota_bytes": 104857600, "duration_days": 30, "max_users": 10,
	}))
	planID := plan["id"].(string)

	// Create a subscription -> provisions a tenant on the node, returns key once.
	rr = call(t, h, http.MethodPost, "/api/v1/customers/"+customerID+"/subscriptions", map[string]any{
		"plan_id": planID, "node_id": nodeID,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create subscription: %d body=%s", rr.Code, rr.Body.String())
	}
	var subResp subscriptionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &subResp); err != nil {
		t.Fatalf("decode sub: %v", err)
	}
	if subResp.APIKey == "" {
		t.Fatal("api_key should be returned once")
	}
	tenantID := subResp.Subscription.NodeTenantID
	if tenantID == "" {
		t.Fatal("subscription should reference a node tenant id")
	}

	// The fake node must have received the provisioning request with our values.
	fake.mu.Lock()
	created, ok := fake.created[tenantID]
	fake.mu.Unlock()
	if !ok {
		t.Fatal("node did not receive CreateTenant")
	}
	if created.QuotaBytes != 104857600 || created.APIKey != subResp.APIKey {
		t.Fatalf("provisioning mismatch: %+v", created)
	}

	// Run the usage collector once; the customer's usage should reflect the node.
	a.collectUsageOnce(context.Background())
	usage := decode[customerUsageResponse](t, call(t, h, http.MethodGet, "/api/v1/customers/"+customerID+"/usage", nil))
	if usage.UsedBytes != 5000 {
		t.Fatalf("expected aggregated usage 5000, got %d", usage.UsedBytes)
	}

	// Suspend the subscription via the panel (forwarded to the node).
	subID := subResp.Subscription.ID
	if rr := call(t, h, http.MethodPost, "/api/v1/subscriptions/"+subID+"/suspend", nil); rr.Code != http.StatusOK {
		t.Fatalf("suspend: %d body=%s", rr.Code, rr.Body.String())
	}
	subs := decode[[]map[string]any](t, call(t, h, http.MethodGet, "/api/v1/customers/"+customerID+"/subscriptions", nil))
	if len(subs) != 1 || subs[0]["status"] != "suspended" {
		t.Fatalf("subscription should be suspended, got %+v", subs)
	}
}
