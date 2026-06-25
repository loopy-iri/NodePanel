package nodeclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeNode is a minimal stand-in for a node agent admin API.
func fakeNode(t *testing.T, masterKey string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Health{Status: "ok", CoreStarted: true, CoreVersion: "26.3.27"})
	})

	mux.HandleFunc("/admin/tenants", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != masterKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req CreateTenantRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(TenantView{
			ID: req.ID, Status: "active", QuotaBytes: req.QuotaBytes, ExpireAt: req.ExpireAt,
		})
	})

	mux.HandleFunc("/admin/tenants/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != masterKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/usage"):
			_ = json.NewEncoder(w).Encode(UsageView{ID: "t1", Status: "active", UsedBytes: 4242, QuotaBytes: 1000})
		case strings.HasSuffix(r.URL.Path, "/suspend"), strings.HasSuffix(r.URL.Path, "/resume"):
			_ = json.NewEncoder(w).Encode(TenantView{ID: "t1", Status: "active"})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})

	return httptest.NewServer(mux)
}

func TestClientCreateTenantAndUsage(t *testing.T) {
	const key = "master-key"
	srv := fakeNode(t, key)
	defer srv.Close()

	c := New(srv.URL, key, "")
	ctx := context.Background()

	h, err := c.Health(ctx)
	if err != nil || !h.CoreStarted {
		t.Fatalf("health: %+v err=%v", h, err)
	}

	tv, err := c.CreateTenant(ctx, CreateTenantRequest{ID: "t1", APIKey: "ck", QuotaBytes: 1000, ExpireAt: 999})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if tv.ID != "t1" || tv.QuotaBytes != 1000 || tv.ExpireAt != 999 {
		t.Fatalf("unexpected tenant view: %+v", tv)
	}

	uv, err := c.TenantUsage(ctx, "t1")
	if err != nil || uv.UsedBytes != 4242 {
		t.Fatalf("usage: %+v err=%v", uv, err)
	}

	if err := c.Suspend(ctx, "t1"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if err := c.Delete(ctx, "t1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestClientRejectsBadKey(t *testing.T) {
	srv := fakeNode(t, "right-key")
	defer srv.Close()

	c := New(srv.URL, "wrong-key", "")
	if _, err := c.CreateTenant(context.Background(), CreateTenantRequest{ID: "t1", APIKey: "ck"}); err == nil {
		t.Fatal("expected error for wrong master key")
	}
}
