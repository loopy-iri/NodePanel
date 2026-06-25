package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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

	node, err := a.store.CreateNode(req.Name, req.Address, req.MasterKey, req.CertPEM, string(req.Config))
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

func (a *API) deleteNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.store.DeleteNode(id); err != nil {
		writeNotFoundOr500(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getNodeConfig returns the last fixed Xray config the panel pushed to a node,
// so the GUI editor can prefill it.
func (a *API) getNodeConfig(w http.ResponseWriter, r *http.Request) {
	node, err := a.store.GetNode(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if node.ConfigJSON == "" {
		_, _ = w.Write([]byte("{}"))
		return
	}
	_, _ = w.Write([]byte(node.ConfigJSON))
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
