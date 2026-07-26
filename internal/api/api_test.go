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
	// failDelete makes tenant deletion fail, so tests can prove the panel keeps
	// local state rather than orphaning a live tenant.
	failDelete bool
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
		if r.Method == http.MethodDelete {
			f.mu.Lock()
			fail := f.failDelete
			f.mu.Unlock()
			if fail {
				http.Error(w, "tenant busy", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
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

	// Usage history must actually be written. It used to reference a table
	// nothing ever populates, so every insert failed the FK check and the audit
	// trail was silently empty for the life of the database.
	if n, err := a.store.CountUsageRecords(); err != nil || n == 0 {
		t.Fatalf("usage records = %d err=%v, want at least one sample persisted", n, err)
	}
}

// registerTestNode registers a node against a fake agent and returns its id.
func registerTestNode(t *testing.T, h http.Handler, addr, masterKey string) string {
	t.Helper()
	rr := call(t, h, http.MethodPost, "/api/v1/nodes", map[string]any{
		"name": "edge-1", "address": addr, "master_key": masterKey,
		"core_key": "core-secret", "host_info": "SNI: cdn.example.com",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("register node: %d body=%s", rr.Code, rr.Body.String())
	}
	return decode[map[string]any](t, rr)["id"].(string)
}

// PATCH is a partial update: a client that sends only the field it means to
// change must not silently erase the others. core_key and host_info used to be
// wiped by any such request, taking the buyer-facing host note with them.
func TestPatchNodePreservesOmittedFields(t *testing.T) {
	const masterKey = "node-master"
	_, srv := newFakeNode(masterKey, 0)
	defer srv.Close()

	a := newTestAPI(t)
	h := a.Router()
	nodeID := registerTestNode(t, h, srv.URL, masterKey)

	// Rename only. Everything else must survive.
	rr := call(t, h, http.MethodPatch, "/api/v1/nodes/"+nodeID, map[string]any{"name": "edge-renamed"})
	if rr.Code != http.StatusOK {
		t.Fatalf("patch: %d body=%s", rr.Code, rr.Body.String())
	}

	detail := decode[map[string]any](t, call(t, h, http.MethodGet, "/api/v1/nodes/"+nodeID, nil))
	if detail["name"] != "edge-renamed" {
		t.Fatalf("name = %v, want edge-renamed", detail["name"])
	}
	if detail["core_key"] != "core-secret" {
		t.Fatalf("core_key = %v, want it preserved", detail["core_key"])
	}
	if detail["host_info"] != "SNI: cdn.example.com" {
		t.Fatalf("host_info = %v, want it preserved", detail["host_info"])
	}
	if detail["master_key"] != masterKey {
		t.Fatalf("master_key = %v, want it preserved", detail["master_key"])
	}

	// An explicitly empty string still clears a field.
	empty := ""
	if rr := call(t, h, http.MethodPatch, "/api/v1/nodes/"+nodeID, map[string]any{"host_info": empty}); rr.Code != http.StatusOK {
		t.Fatalf("patch clear: %d body=%s", rr.Code, rr.Body.String())
	}
	detail = decode[map[string]any](t, call(t, h, http.MethodGet, "/api/v1/nodes/"+nodeID, nil))
	if detail["host_info"] != "" {
		t.Fatalf("host_info = %v, want it cleared", detail["host_info"])
	}

	// Blanking a required field is refused rather than applied.
	if rr := call(t, h, http.MethodPatch, "/api/v1/nodes/"+nodeID, map[string]any{"name": ""}); rr.Code != http.StatusBadRequest {
		t.Fatalf("blank name: %d, want 400", rr.Code)
	}
}

// Deleting a subscription discards the only record of node_tenant_id, so it
// must not proceed when the node refuses to remove the tenant — that would
// leave a live tenant serving traffic with nothing pointing at it.
func TestDeleteSubscriptionKeptWhenNodeFails(t *testing.T) {
	const masterKey = "node-master"
	fake, srv := newFakeNode(masterKey, 0)
	defer srv.Close()

	a := newTestAPI(t)
	h := a.Router()
	nodeID := registerTestNode(t, h, srv.URL, masterKey)

	cust := decode[map[string]any](t, call(t, h, http.MethodPost, "/api/v1/customers", map[string]any{"name": "acme"}))
	plan := decode[map[string]any](t, call(t, h, http.MethodPost, "/api/v1/plans", map[string]any{
		"name": "100MB", "quota_bytes": 104857600, "duration_days": 30, "max_users": 10,
	}))
	rr := call(t, h, http.MethodPost, "/api/v1/customers/"+cust["id"].(string)+"/subscriptions", map[string]any{
		"plan_id": plan["id"].(string), "node_id": nodeID,
	})
	var subResp subscriptionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &subResp); err != nil {
		t.Fatalf("decode sub: %v", err)
	}
	subID := subResp.Subscription.ID

	// The node now rejects tenant deletion.
	fake.mu.Lock()
	fake.failDelete = true
	fake.mu.Unlock()

	if rr := call(t, h, http.MethodDelete, "/api/v1/subscriptions/"+subID, nil); rr.Code != http.StatusBadGateway {
		t.Fatalf("delete with failing node: %d, want 502 (subscription kept)", rr.Code)
	}
	if _, err := a.store.GetSubscription(subID); err != nil {
		t.Fatalf("subscription was removed despite the node failure: %v", err)
	}

	// Once the node accepts it, the delete goes through.
	fake.mu.Lock()
	fake.failDelete = false
	fake.mu.Unlock()
	if rr := call(t, h, http.MethodDelete, "/api/v1/subscriptions/"+subID, nil); rr.Code != http.StatusNoContent {
		t.Fatalf("delete with healthy node: %d, want 204", rr.Code)
	}
	if _, err := a.store.GetSubscription(subID); err == nil {
		t.Fatal("subscription should be gone after a successful deprovision")
	}
}

// The /sub/{token} responses carry a live node credential, so they must never
// be cacheable by an intermediary.
func TestSubResponsesAreNotCacheable(t *testing.T) {
	const masterKey = "node-master"
	_, srv := newFakeNode(masterKey, 0)
	defer srv.Close()

	a := newTestAPI(t)
	h := a.Router()
	nodeID := registerTestNode(t, h, srv.URL, masterKey)

	cust := decode[map[string]any](t, call(t, h, http.MethodPost, "/api/v1/customers", map[string]any{"name": "acme"}))
	plan := decode[map[string]any](t, call(t, h, http.MethodPost, "/api/v1/plans", map[string]any{
		"name": "100MB", "quota_bytes": 104857600, "duration_days": 30, "max_users": 10,
	}))
	rr := call(t, h, http.MethodPost, "/api/v1/customers/"+cust["id"].(string)+"/subscriptions", map[string]any{
		"plan_id": plan["id"].(string), "node_id": nodeID,
	})
	var subResp subscriptionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &subResp); err != nil {
		t.Fatalf("decode sub: %v", err)
	}

	sub, err := a.store.GetSubscription(subResp.Subscription.ID)
	if err != nil {
		t.Fatalf("load subscription: %v", err)
	}
	for _, path := range []string{"/sub/" + sub.SubToken, "/sub/" + sub.SubToken + "/info"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("GET %s Cache-Control = %q, want no-store", path, cc)
		}
	}
}

// A subscription token in a URL is itself a credential; the access log must not
// record it verbatim.
func TestRequestLogRedactsSubToken(t *testing.T) {
	cases := map[string]string{
		"/sub/abc123":           "/sub/<redacted>",
		"/sub/abc123/info":      "/sub/<redacted>/info",
		"/api/v1/nodes":         "/api/v1/nodes",
		"/sub/":                 "/sub/<redacted>",
		"/health":               "/health",
		"/api/v1/subscriptions": "/api/v1/subscriptions",
	}
	for in, want := range cases {
		if got := redactPath(in); got != want {
			t.Errorf("redactPath(%q) = %q, want %q", in, got, want)
		}
	}
}
