package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pasarguard/panel/internal/domain"
	"github.com/pasarguard/panel/internal/nodeclient"
)

func (a *API) clientForNode(n *domain.Node) *nodeclient.Client {
	return nodeclient.New(n.Address, n.MasterKey, n.CertPEM)
}

type registerNodeRequest struct {
	Name      string          `json:"name"`
	Address   string          `json:"address"`
	MasterKey string          `json:"master_key"`
	CertPEM   string          `json:"cert_pem"`         // node's self-signed cert (PEM) to pin
	GRPCPort  int             `json:"grpc_port"`        // PasarGuard-compat gRPC port (default 62050)
	Config    json.RawMessage `json:"config,omitempty"` // optional fixed Xray config to push
}

func (a *API) registerNode(w http.ResponseWriter, r *http.Request) {
	var req registerNodeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Address) == "" || req.MasterKey == "" {
		writeError(w, http.StatusBadRequest, "name, address and master_key are required")
		return
	}

	// Trust-on-first-use: if no cert was provided, try to fetch and pin the
	// node's current certificate automatically. If that fails, the node is still
	// registered (TLS verification is skipped for it).
	if strings.TrimSpace(req.CertPEM) == "" {
		if fetched, err := nodeclient.FetchCert(req.Address); err == nil {
			req.CertPEM = fetched
		}
	}

	node, err := a.store.CreateNode(req.Name, req.Address, req.MasterKey, req.CertPEM, string(req.Config), req.GRPCPort)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create node")
		return
	}

	client := a.clientForNode(node)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	if len(req.Config) > 0 {
		if err := client.ApplyConfig(ctx, string(req.Config)); err != nil {
			writeError(w, http.StatusBadGateway, "node rejected config: "+err.Error())
			return
		}
	}

	// Probe health so the registration reflects reality.
	if h, err := client.Health(ctx); err == nil {
		_ = a.store.UpdateNodeHealth(node.ID, "online", h.CoreVersion, time.Now().Unix())
		node.Status = "online"
		node.Version = h.CoreVersion
	} else {
		_ = a.store.UpdateNodeHealth(node.ID, "offline", "", time.Now().Unix())
		node.Status = "offline"
	}

	writeJSON(w, http.StatusCreated, node)
}

func (a *API) listNodes(w http.ResponseWriter, _ *http.Request) {
	nodes, err := a.store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list nodes")
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (a *API) nodeHealth(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	node, err := a.getNodeOr404(w, id)
	if node == nil {
		return
	}
	_ = err

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	h, err := a.clientForNode(node).Health(ctx)
	if err != nil {
		_ = a.store.UpdateNodeHealth(id, "offline", "", time.Now().Unix())
		writeError(w, http.StatusBadGateway, "node unreachable: "+err.Error())
		return
	}
	_ = a.store.UpdateNodeHealth(id, "online", h.CoreVersion, time.Now().Unix())
	writeJSON(w, http.StatusOK, h)
}

func (a *API) getNodeOr404(w http.ResponseWriter, id string) (*domain.Node, error) {
	node, err := a.store.GetNode(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return nil, err
	}
	return node, nil
}

// nodeDetailResponse exposes the full connection details of a node for the
// operator: addresses, ports and the certificate (needed to register the node
// in a customer's panel). The master key is never returned.
type nodeDetailResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Address     string `json:"address"`      // HTTP control address (panel -> node)
	ServicePort int    `json:"service_port"` // HTTP control port
	GRPCAddress string `json:"grpc_address"` // customer's panel -> node
	GRPCPort    int    `json:"grpc_port"`
	Protocol    string `json:"protocol"`
	CertPEM     string `json:"cert_pem"`
	Status      string `json:"status"`
	Version     string `json:"version,omitempty"`
	LastSeenAt  int64  `json:"last_seen_at,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

// getNodeDetail returns a node's full connection info (host, service/gRPC ports,
// certificate) so the operator can copy them and configure a customer's panel.
func (a *API) getNodeDetail(w http.ResponseWriter, r *http.Request) {
	node, err := a.store.GetNode(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	grpcPort := node.GRPCPort
	if grpcPort <= 0 {
		grpcPort = 62050
	}
	writeJSON(w, http.StatusOK, nodeDetailResponse{
		ID:          node.ID,
		Name:        node.Name,
		Host:        hostOf(node.Address),
		Address:     node.Address,
		ServicePort: servicePortOf(node.Address),
		GRPCAddress: grpcAddressFor(node.Address, grpcPort),
		GRPCPort:    grpcPort,
		Protocol:    "grpc",
		CertPEM:     node.CertPEM,
		Status:      node.Status,
		Version:     node.Version,
		LastSeenAt:  node.LastSeenAt,
		CreatedAt:   node.CreatedAt,
	})
}

// servicePortOf extracts the HTTP control port from a node address (default 8090).
func servicePortOf(address string) int {
	if u, err := url.Parse(address); err == nil && u.Port() != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			return p
		}
	}
	return 8090
}

func (a *API) deleteNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.store.DeleteNode(id); err != nil {
		writeNotFoundOr500(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getNodeConfig returns the node's currently running Xray config. It queries the
// node live (so the GUI/operator sees the real inbound to share with customers)
// and falls back to the last config the panel pushed if the node is unreachable.
func (a *API) getNodeConfig(w http.ResponseWriter, r *http.Request) {
	node, err := a.store.GetNode(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")
	if live, err := a.clientForNode(node).GetConfig(ctx); err == nil && len(bytes.TrimSpace(live)) > 0 {
		_, _ = w.Write(live)
		return
	}
	if node.ConfigJSON == "" {
		_, _ = w.Write([]byte("{}"))
		return
	}
	_, _ = w.Write([]byte(node.ConfigJSON))
}

// getNodeInbounds returns the customer-shareable inbound definitions of a node
// ({"inbounds":[...]}). Prefers the live node; falls back to the panel's stored
// config so it still works with an older/unreachable node.
func (a *API) getNodeInbounds(w http.ResponseWriter, r *http.Request) {
	node, err := a.store.GetNode(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(a.resolveInbounds(ctx, node))
}

// resolveInbounds returns a node's customer-shareable inbounds. It prefers the
// live node (which redacts secrets and adds the Reality public key); if the node
// is unreachable or older (no /admin/inbounds) or returns nothing, it falls back
// to the panel's stored fixed config with the Reality private key stripped.
func (a *API) resolveInbounds(ctx context.Context, node *domain.Node) json.RawMessage {
	if ib, err := a.clientForNode(node).GetInbounds(ctx); err == nil && hasInbounds(ib) {
		return ib
	}
	if fb := inboundsFromConfig(node.ConfigJSON); fb != nil {
		return fb
	}
	return json.RawMessage(`{"inbounds":[]}`)
}

func hasInbounds(raw json.RawMessage) bool {
	var d struct {
		Inbounds []json.RawMessage `json:"inbounds"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return false
	}
	return len(d.Inbounds) > 0
}

// inboundsFromConfig extracts {"inbounds":[...]} from a full Xray config and
// strips server-only secrets (Reality privateKey) before sharing.
func inboundsFromConfig(cfg string) json.RawMessage {
	if cfg == "" {
		return nil
	}
	var doc struct {
		Inbounds []json.RawMessage `json:"inbounds"`
	}
	if json.Unmarshal([]byte(cfg), &doc) != nil || len(doc.Inbounds) == 0 {
		return nil
	}
	cleaned := make([]json.RawMessage, 0, len(doc.Inbounds))
	for _, ib := range doc.Inbounds {
		cleaned = append(cleaned, redactInboundJSON(ib))
	}
	out, err := json.Marshal(struct {
		Inbounds []json.RawMessage `json:"inbounds"`
	}{Inbounds: cleaned})
	if err != nil {
		return nil
	}
	return out
}

func redactInboundJSON(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if ss, ok := m["streamSettings"].(map[string]any); ok {
		if rs, ok := ss["realitySettings"].(map[string]any); ok {
			delete(rs, "privateKey")
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// updateNodeConfig pushes a new fixed Xray config to the node and stores it.
func (a *API) updateNodeConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	node, err := a.store.GetNode(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		writeError(w, http.StatusBadRequest, "config body is empty")
		return
	}
	if !json.Valid(body) {
		writeError(w, http.StatusBadRequest, "config must be valid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := a.clientForNode(node).ApplyConfig(ctx, string(body)); err != nil {
		writeError(w, http.StatusBadGateway, "node rejected config: "+err.Error())
		return
	}
	if err := a.store.UpdateNodeConfig(id, string(body)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store config")
		return
	}

	if h, err := a.clientForNode(node).Health(ctx); err == nil {
		_ = a.store.UpdateNodeHealth(id, "online", h.CoreVersion, time.Now().Unix())
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}
