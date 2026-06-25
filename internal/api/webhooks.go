package api

import (
	"net/http"
	"strings"
)

type createWebhookRequest struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
	Events string `json:"events"` // comma-separated, or "*" (default)
}

func (a *API) createWebhook(w http.ResponseWriter, r *http.Request) {
	var req createWebhookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if strings.TrimSpace(req.URL) == "" || req.Secret == "" {
		writeError(w, http.StatusBadRequest, "url and secret are required")
		return
	}
	wh, err := a.store.CreateWebhook(req.URL, req.Secret, req.Events)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create webhook")
		return
	}
	writeJSON(w, http.StatusCreated, wh)
}

func (a *API) listWebhooks(w http.ResponseWriter, _ *http.Request) {
	hooks, err := a.store.ListWebhooks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list webhooks")
		return
	}
	writeJSON(w, http.StatusOK, hooks)
}
