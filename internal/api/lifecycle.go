package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// deletePlan removes a plan (existing subscriptions keep their copied values).
func (a *API) deletePlan(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeletePlan(chi.URLParam(r, "id")); err != nil {
		writeNotFoundOr500(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteCustomer deprovisions all of the customer's tenants from their nodes,
// then removes the customer (subscriptions and api keys cascade).
func (a *API) deleteCustomer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	subs, _ := a.store.ListSubscriptionsByCustomer(id)
	for _, s := range subs {
		if s.NodeTenantID == "" {
			continue
		}
		if node, err := a.store.GetNode(s.NodeID); err == nil {
			_ = a.clientForNode(node).Delete(ctx, s.NodeTenantID)
		}
	}
	if err := a.store.DeleteCustomer(id); err != nil {
		writeNotFoundOr500(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) enableCustomer(w http.ResponseWriter, r *http.Request)  { a.toggleCustomer(w, r, true) }
func (a *API) disableCustomer(w http.ResponseWriter, r *http.Request) { a.toggleCustomer(w, r, false) }

// toggleCustomer enables/disables a customer and propagates the change to every
// subscription's tenant on its node (resume / suspend).
func (a *API) toggleCustomer(w http.ResponseWriter, r *http.Request, enable bool) {
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	subs, _ := a.store.ListSubscriptionsByCustomer(id)
	for _, s := range subs {
		node, err := a.store.GetNode(s.NodeID)
		if err != nil || s.NodeTenantID == "" {
			continue
		}
		cl := a.clientForNode(node)
		if enable {
			_ = cl.Resume(ctx, s.NodeTenantID)
			_ = a.store.UpdateSubscriptionStatus(s.ID, "active")
		} else {
			_ = cl.Suspend(ctx, s.NodeTenantID)
			_ = a.store.UpdateSubscriptionStatus(s.ID, "suspended")
		}
	}

	status := "disabled"
	if enable {
		status = "active"
	}
	if err := a.store.SetCustomerStatus(id, status); err != nil {
		writeNotFoundOr500(w, err)
		return
	}
	cust, err := a.store.GetCustomer(id)
	if err != nil {
		writeNotFoundOr500(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cust)
}
