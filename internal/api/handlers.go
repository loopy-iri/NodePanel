package api

import (
	"net/http"
	"strings"
)

// --- Customers ---

type createCustomerRequest struct {
	Name        string `json:"name"`
	ExternalRef string `json:"external_ref"`
}

func (a *API) createCustomer(w http.ResponseWriter, r *http.Request) {
	var req createCustomerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	c, err := a.store.CreateCustomer(req.Name, req.ExternalRef)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create customer")
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) listCustomers(w http.ResponseWriter, _ *http.Request) {
	customers, err := a.store.ListCustomers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list customers")
		return
	}
	writeJSON(w, http.StatusOK, customers)
}

// --- Plans ---

type createPlanRequest struct {
	Name         string `json:"name"`
	QuotaBytes   int64  `json:"quota_bytes"`
	DurationDays int    `json:"duration_days"`
	MaxUsers     int    `json:"max_users"`
}

func (a *API) createPlan(w http.ResponseWriter, r *http.Request) {
	var req createPlanRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.QuotaBytes <= 0 || req.DurationDays <= 0 {
		writeError(w, http.StatusBadRequest, "quota_bytes and duration_days must be positive")
		return
	}

	p, err := a.store.CreatePlan(req.Name, req.QuotaBytes, req.DurationDays, req.MaxUsers)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create plan")
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (a *API) listPlans(w http.ResponseWriter, _ *http.Request) {
	plans, err := a.store.ListPlans()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list plans")
		return
	}
	writeJSON(w, http.StatusOK, plans)
}
