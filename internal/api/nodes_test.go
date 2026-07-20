package api

import (
	"net/http"
	"testing"
)

// TestNodeHostInfoSurfaced verifies an operator-set host_info is returned from
// the node detail API and from a subscription's connection info.
func TestNodeHostInfoSurfaced(t *testing.T) {
	const masterKey = "node-master"
	const hostInfo = "SNI: example.com; use reality"
	_, srv := newFakeNode(masterKey, 0)
	defer srv.Close()

	a := newTestAPI(t)
	h := a.Router()

	// Register a node carrying host_info.
	rr := call(t, h, http.MethodPost, "/api/v1/nodes", map[string]any{
		"name": "edge-1", "address": srv.URL, "master_key": masterKey, "host_info": hostInfo,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("register node: %d body=%s", rr.Code, rr.Body.String())
	}
	nodeID := decode[map[string]any](t, rr)["id"].(string)

	// Node detail must expose host_info.
	detail := decode[nodeDetailResponse](t, call(t, h, http.MethodGet, "/api/v1/nodes/"+nodeID, nil))
	if detail.HostInfo != hostInfo {
		t.Fatalf("node detail host_info = %q, want %q", detail.HostInfo, hostInfo)
	}

	// Create a customer, plan, and subscription on this node.
	customerID := decode[map[string]any](t, call(t, h, http.MethodPost, "/api/v1/customers", map[string]any{"name": "acme"}))["id"].(string)
	planID := decode[map[string]any](t, call(t, h, http.MethodPost, "/api/v1/plans", map[string]any{
		"name": "100MB", "quota_bytes": 104857600, "duration_days": 30, "max_users": 10,
	}))["id"].(string)
	rr = call(t, h, http.MethodPost, "/api/v1/customers/"+customerID+"/subscriptions", map[string]any{
		"plan_id": planID, "node_id": nodeID,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create subscription: %d body=%s", rr.Code, rr.Body.String())
	}
	subID := decode[subscriptionResponse](t, rr).Subscription.ID

	// Connection info must include the same host_info.
	conn := decode[connectionInfoResponse](t, call(t, h, http.MethodGet, "/api/v1/subscriptions/"+subID+"/connection", nil))
	if conn.HostInfo != hostInfo {
		t.Fatalf("connection host_info = %q, want %q", conn.HostInfo, hostInfo)
	}
}
