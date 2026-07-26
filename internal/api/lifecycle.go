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
//
// Every node-side delete must succeed before anything local is removed.
// Deleting the customer discards the node_tenant_id values, so proceeding past
// a failure would leave live tenants on nodes with no record anywhere that they
// exist — still serving traffic, unbillable, and unreachable for cleanup.
func (a *API) deleteCustomer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	subs, err := a.store.ListSubscriptionsByCustomer(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list subscriptions: "+err.Error())
		return
	}
	for _, s := range subs {
		if s.NodeTenantID == "" {
			continue
		}
		node, err := a.store.GetNode(s.NodeID)
		if err != nil {
			writeError(w, http.StatusBadGateway,
				"cannot load node "+s.NodeID+" to deprovision subscription "+s.ID+": "+err.Error())
			return
		}
		if err := a.clientForNode(node).Delete(ctx, s.NodeTenantID); err != nil {
			writeError(w, http.StatusBadGateway,
				"node refused to remove the tenant for subscription "+s.ID+
					", so the customer was kept: "+err.Error())
			return
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
