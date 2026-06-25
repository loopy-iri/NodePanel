package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pasarguard/panel/internal/domain"
	"github.com/pasarguard/panel/internal/nodeclient"
)

type createSubscriptionRequest struct {
	PlanID           string `json:"plan_id"`
	NodeID           string `json:"node_id"`
	CreditLimitBytes int64  `json:"credit_limit_bytes"`
}

type subscriptionResponse struct {
	Subscription domain.Subscription `json:"subscription"`
	APIKey       string              `json:"api_key"` // shown only once
	NodeAddress  string              `json:"node_address"`
	SubToken     string              `json:"sub_token"` // public subscription page token
}

// createSubscription provisions a tenant on a node for a customer+plan and
// returns the customer's API key once. The customer uses this key against the
// node directly.
func (a *API) createSubscription(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "id")

	var req createSubscriptionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	customer, err := a.store.GetCustomer(customerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "customer not found")
		return
	}
	plan, err := a.store.GetPlan(req.PlanID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "plan not found")
		return
	}
	node, err := a.store.GetNode(req.NodeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "node not found")
		return
	}

	now := time.Now().Unix()
	endAt := now + int64(plan.DurationDays)*86400
	rawKey := uuid.NewString()
	tenantID := uuid.NewString()

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	if _, err := a.clientForNode(node).CreateTenant(ctx, nodeclient.CreateTenantRequest{
		ID:               tenantID,
		APIKey:           rawKey,
		QuotaBytes:       plan.QuotaBytes,
		CreditLimitBytes: req.CreditLimitBytes,
		ExpireAt:         endAt,
	}); err != nil {
		writeError(w, http.StatusBadGateway, "failed to provision tenant on node: "+err.Error())
		return
	}

	sub := &domain.Subscription{
		CustomerID:       customer.ID,
		PlanID:           plan.ID,
		NodeID:           node.ID,
		NodeTenantID:     tenantID,
		APIKey:           rawKey,
		Status:           "active",
		PeriodID:         1,
		StartAt:          now,
		EndAt:            endAt,
		QuotaBytes:       plan.QuotaBytes,
		CreditLimitBytes: req.CreditLimitBytes,
	}
	if err := a.store.CreateSubscription(sub); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store subscription")
		return
	}
	if err := a.store.CreateAPIKey(customer.ID, hashKey(rawKey), keyPrefix(rawKey)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store api key")
		return
	}

	writeJSON(w, http.StatusCreated, subscriptionResponse{
		Subscription: *sub,
		APIKey:       rawKey,
		NodeAddress:  node.Address,
		SubToken:     sub.SubToken,
	})
}

// grpcAddressFor derives the node's gRPC endpoint from its stored control
// address and configured gRPC port (e.g. https://1.2.3.4:8090 + 62050 ->
// 1.2.3.4:62050).
func grpcAddressFor(address string, grpcPort int) string {
	if grpcPort <= 0 {
		grpcPort = 62050
	}
	return fmt.Sprintf("%s:%d", hostOf(address), grpcPort)
}

// hostOf extracts the bare host/IP from a node address.
func hostOf(address string) string {
	if u, err := url.Parse(address); err == nil && u.Host != "" {
		return u.Hostname()
	}
	host := strings.TrimPrefix(strings.TrimPrefix(address, "https://"), "http://")
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host
}

type connectionInfoResponse struct {
	NodeName    string          `json:"node_name"`
	GRPCAddress string          `json:"grpc_address"`
	Protocol    string          `json:"protocol"`
	CertPEM     string          `json:"cert_pem"`
	Inbounds    json.RawMessage `json:"inbounds"`
	SubToken    string          `json:"sub_token"`
	Note        string          `json:"note"`
}

// subscriptionConnection returns everything a customer needs to add this node in
// their own PasarGuard panel: the gRPC address, the node certificate, and the
// shareable inbound definitions (so their inbound matches the node's real one).
// The customer's API key is NOT included — it is shown only once at creation.
func (a *API) subscriptionConnection(w http.ResponseWriter, r *http.Request) {
	sub, node, ok := a.loadSubAndNode(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	inbounds := a.resolveInbounds(ctx, node)
	_ = sub
	writeJSON(w, http.StatusOK, connectionInfoResponse{
		NodeName:    node.Name,
		GRPCAddress: grpcAddressFor(node.Address, node.GRPCPort),
		Protocol:    "grpc",
		CertPEM:     node.CertPEM,
		Inbounds:    inbounds,
		SubToken:    sub.SubToken,
		Note:        "Add this node in your PasarGuard panel with the gRPC address, protocol grpc, the certificate, and your customer API key. Replicate the inbound(s) exactly (port/protocol/network/TLS/SNI) so user links work.",
	})
}

func (a *API) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "id")
	subs, err := a.store.ListSubscriptionsByCustomer(customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list subscriptions")
		return
	}
	writeJSON(w, http.StatusOK, subs)
}

func (a *API) suspendSubscription(w http.ResponseWriter, r *http.Request) {
	a.subscriptionLifecycle(w, r, "suspended")
}

func (a *API) resumeSubscription(w http.ResponseWriter, r *http.Request) {
	a.subscriptionLifecycle(w, r, "active")
}

func (a *API) subscriptionLifecycle(w http.ResponseWriter, r *http.Request, target string) {
	sub, node, ok := a.loadSubAndNode(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	client := a.clientForNode(node)
	var err error
	if target == "suspended" {
		err = client.Suspend(ctx, sub.NodeTenantID)
	} else {
		err = client.Resume(ctx, sub.NodeTenantID)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "node error: "+err.Error())
		return
	}
	if err := a.store.UpdateSubscriptionStatus(sub.ID, target); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update status")
		return
	}
	sub.Status = target
	a.emitStatusEvent(r.Context(), *sub, target)
	writeJSON(w, http.StatusOK, sub)
}

func (a *API) loadSubAndNode(w http.ResponseWriter, r *http.Request) (*domain.Subscription, *domain.Node, bool) {
	id := chi.URLParam(r, "id")
	sub, err := a.store.GetSubscription(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "subscription not found")
		return nil, nil, false
	}
	node, err := a.store.GetNode(sub.NodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "subscription node missing")
		return nil, nil, false
	}
	return sub, node, true
}

type topupQuotaRequest struct {
	AddBytes int64 `json:"add_bytes"`
}

func (a *API) topupQuota(w http.ResponseWriter, r *http.Request) {
	sub, node, ok := a.loadSubAndNode(w, r)
	if !ok {
		return
	}
	var req topupQuotaRequest
	if err := decodeJSON(r, &req); err != nil || req.AddBytes <= 0 {
		writeError(w, http.StatusBadRequest, "add_bytes must be positive")
		return
	}

	newQuota := sub.QuotaBytes + req.AddBytes
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if _, err := a.clientForNode(node).SetQuota(ctx, sub.NodeTenantID, nodeclient.SetQuotaRequest{
		QuotaBytes:       newQuota,
		CreditLimitBytes: sub.CreditLimitBytes,
		ExpireAt:         sub.EndAt,
	}); err != nil {
		writeError(w, http.StatusBadGateway, "node error: "+err.Error())
		return
	}
	// Topping up reactivates a quota-suspended tenant on the node side too.
	_ = a.clientForNode(node).Resume(ctx, sub.NodeTenantID)

	if err := a.store.SetSubscriptionQuota(sub.ID, newQuota, sub.CreditLimitBytes, sub.EndAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update quota")
		return
	}
	_ = a.store.UpdateSubscriptionStatus(sub.ID, "active")
	// Re-arm usage notifications for the new quota so future crossings re-fire.
	_ = a.store.UpdateSubscriptionNotified(sub.ID, thresholdLevel(sub.UsedBytes, newQuota))
	sub.QuotaBytes = newQuota
	sub.Status = "active"
	writeJSON(w, http.StatusOK, sub)
}

type customerUsageResponse struct {
	CustomerID   string `json:"customer_id"`
	QuotaBytes   int64  `json:"quota_bytes"`
	UsedBytes    int64  `json:"used_bytes"`
	OverageBytes int64  `json:"overage_bytes"`
}

// customerUsage aggregates usage across all of a customer's subscriptions.
func (a *API) customerUsage(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "id")
	subs, err := a.store.ListSubscriptionsByCustomer(customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load usage")
		return
	}
	var quota, used int64
	for _, s := range subs {
		quota += s.QuotaBytes
		used += s.UsedBytes
	}
	overage := used - quota
	if overage < 0 {
		overage = 0
	}
	writeJSON(w, http.StatusOK, customerUsageResponse{
		CustomerID:   customerID,
		QuotaBytes:   quota,
		UsedBytes:    used,
		OverageBytes: overage,
	})
}

// deleteSubscription deprovisions the tenant from its node and removes the row.
func (a *API) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sub, err := a.store.GetSubscription(id)
	if err != nil {
		writeNotFoundOr500(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if sub.NodeTenantID != "" {
		if node, err := a.store.GetNode(sub.NodeID); err == nil {
			_ = a.clientForNode(node).Delete(ctx, sub.NodeTenantID)
		}
	}
	if err := a.store.DeleteSubscription(id); err != nil {
		writeNotFoundOr500(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// renewSubscription starts a new period: zeroes usage, extends expiry by the
// plan duration, and reactivates the tenant on its node.
func (a *API) renewSubscription(w http.ResponseWriter, r *http.Request) {
	sub, node, ok := a.loadSubAndNode(w, r)
	if !ok {
		return
	}

	days := 30
	if plan, err := a.store.GetPlan(sub.PlanID); err == nil && plan.DurationDays > 0 {
		days = plan.DurationDays
	}
	endAt := time.Now().Unix() + int64(days)*86400

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	cl := a.clientForNode(node)
	if err := cl.ResetPeriod(ctx, sub.NodeTenantID); err != nil {
		writeError(w, http.StatusBadGateway, "node error: "+err.Error())
		return
	}
	if _, err := cl.SetQuota(ctx, sub.NodeTenantID, nodeclient.SetQuotaRequest{
		QuotaBytes:       sub.QuotaBytes,
		CreditLimitBytes: sub.CreditLimitBytes,
		ExpireAt:         endAt,
	}); err != nil {
		writeError(w, http.StatusBadGateway, "node error: "+err.Error())
		return
	}

	if err := a.store.RenewSubscription(sub.ID, endAt); err != nil {
		writeNotFoundOr500(w, err)
		return
	}
	updated, _ := a.store.GetSubscription(sub.ID)
	writeJSON(w, http.StatusOK, updated)
}
