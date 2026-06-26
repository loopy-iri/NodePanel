package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/pasarguard/panel/internal/config"
	"github.com/pasarguard/panel/internal/store"
	"github.com/pasarguard/panel/internal/web"
	"github.com/pasarguard/panel/internal/webhook"
)

// API holds dependencies shared by the HTTP handlers.
type API struct {
	store *store.Store
	cfg   *config.Config
	wh    *webhook.Dispatcher
}

// New builds the API with its dependencies.
func New(st *store.Store, cfg *config.Config) *API {
	return &API{store: st, cfg: cfg, wh: webhook.NewDispatcher(st)}
}

// Router builds the panel HTTP router.
func (a *API) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Health is unauthenticated.
	r.Get("/health", a.handleHealth)

	// Public/admin API requires a bearer token (the sales bot uses this).
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(a.bearerAuth)

		r.Get("/customers", a.listCustomers)
		r.Post("/customers", a.createCustomer)
		r.Delete("/customers/{id}", a.deleteCustomer)
		r.Post("/customers/{id}/enable", a.enableCustomer)
		r.Post("/customers/{id}/disable", a.disableCustomer)
		r.Get("/customers/{id}/usage", a.customerUsage)
		r.Get("/customers/{id}/subscriptions", a.listSubscriptions)
		r.Post("/customers/{id}/subscriptions", a.createSubscription)

		r.Get("/plans", a.listPlans)
		r.Post("/plans", a.createPlan)
		r.Delete("/plans/{id}", a.deletePlan)

		r.Get("/nodes", a.listNodes)
		r.Post("/nodes", a.registerNode)
		r.Delete("/nodes/{id}", a.deleteNode)
		r.Get("/nodes/{id}", a.getNodeDetail)
		r.Patch("/nodes/{id}", a.updateNode)
		r.Get("/nodes/{id}/health", a.nodeHealth)
		r.Get("/nodes/{id}/config", a.getNodeConfig)
		r.Put("/nodes/{id}/config", a.updateNodeConfig)
		r.Get("/nodes/{id}/inbounds", a.getNodeInbounds)

		r.Post("/subscriptions/{id}/suspend", a.suspendSubscription)
		r.Post("/subscriptions/{id}/resume", a.resumeSubscription)
		r.Post("/subscriptions/{id}/topup-quota", a.topupQuota)
		r.Post("/subscriptions/{id}/renew", a.renewSubscription)
		r.Get("/subscriptions/{id}/connection", a.subscriptionConnection)
		r.Delete("/subscriptions/{id}", a.deleteSubscription)

		r.Get("/webhooks", a.listWebhooks)
		r.Post("/webhooks", a.createWebhook)
	})

	// Public subscription page (token-gated, no bearer auth): the customer opens
	// this to get the node config to paste and to see quota/expiry.
	r.Get("/sub/{token}", a.subPage)
	r.Get("/sub/{token}/info", a.subInfo)

	// API docs (Swagger UI) and OpenAPI spec are public.
	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs.html", http.StatusFound)
	})

	// Static admin SPA + assets (served last as the catch-all).
	r.Handle("/*", web.Handler())

	return r
}

func (a *API) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// bearerAuth enforces a static bearer token for the public API.
func (a *API) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if token == "" || token != a.cfg.APIToken {
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeNotFoundOr500(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func decodeJSON(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	// Tolerate a leading UTF-8 BOM (some clients/editors prepend one).
	body = bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
