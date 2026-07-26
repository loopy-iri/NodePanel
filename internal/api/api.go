package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
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

const (
	// defaultRequestTimeout bounds ordinary API requests.
	defaultRequestTimeout = 30 * time.Second
	// longNodeOpTimeout covers operations that drive a node through a download
	// and restart (Xray version switch, node self-update). These legitimately
	// take minutes, so they get their own budget instead of being cut off by
	// the default and retried by an operator onto a node already mid-swap.
	longNodeOpTimeout = 200 * time.Second
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
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)

	// Health is unauthenticated.
	r.Get("/health", a.handleHealth)

	// Public/admin API requires a bearer token (the sales bot uses this).
	//
	// Timeouts are applied per group, never globally: chi's Timeout middleware
	// replaces the request context and Go takes the EARLIER of two deadlines,
	// so a blanket 30s silently capped the deliberately long node operations
	// (Xray version switch, self-update) and made them fail every time. For the
	// same reason the long group must not sit inside the default one.
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(a.bearerAuth)

		// Long node operations: a download plus a core restart on the node.
		// A sibling group, never nested inside the default-timeout one.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(longNodeOpTimeout))
			r.Post("/nodes/{id}/xray-version", a.setXrayVersion)
			r.Post("/nodes/{id}/update", a.updateNodeBinary)
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(defaultRequestTimeout))

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
			r.Post("/nodes/{id}/core/{action}", a.coreLifecycle)

			r.Post("/subscriptions/{id}/suspend", a.suspendSubscription)
			r.Post("/subscriptions/{id}/resume", a.resumeSubscription)
			r.Post("/subscriptions/{id}/topup-quota", a.topupQuota)
			r.Post("/subscriptions/{id}/renew", a.renewSubscription)
			r.Get("/subscriptions/{id}/connection", a.subscriptionConnection)
			r.Delete("/subscriptions/{id}", a.deleteSubscription)

			r.Get("/webhooks", a.listWebhooks)
			r.Post("/webhooks", a.createWebhook)
		})
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

// requestLogger logs requests with subscription tokens redacted. A /sub/<token>
// URL is itself a live credential — it hands out the node API key — so writing
// it verbatim into stdout, journald, and every log shipper downstream would
// leak access to anyone who can read logs.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, redactPath(r.URL.Path), rec.status, time.Since(start).Round(time.Millisecond))
	})
}

// redactPath replaces a subscription token with a placeholder.
func redactPath(path string) string {
	const prefix = "/sub/"
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	rest := path[len(prefix):]
	suffix := ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		suffix = rest[i:]
	}
	return prefix + "<redacted>" + suffix
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// bearerAuth enforces the static bearer token guarding the admin API. It
// requires the exact "Bearer <token>" form and compares in constant time: this
// single token grants everything, including reading node master keys, so a
// timing side channel on it would be a full compromise.
func (a *API) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, prefix) {
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		token := strings.TrimPrefix(auth, prefix)
		if token == "" || a.cfg.APIToken == "" ||
			subtle.ConstantTimeCompare([]byte(token), []byte(a.cfg.APIToken)) != 1 {
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
