// Package nodeclient is the panel's HTTP client for a node agent's master-scope
// admin API. It authenticates with the node's master key (X-API-Key) and is used
// to provision tenants, push config, control lifecycle and pull usage.
package nodeclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to a single node agent.
type Client struct {
	baseURL   string
	masterKey string
	http      *http.Client
}

// New returns a client for the node at baseURL (e.g. https://1.2.3.4:8090).
//
// TLS behaviour:
//   - certPEM set   -> the node's certificate is pinned (only that exact cert is
//     accepted), independent of CA chains or hostnames.
//   - certPEM empty -> TLS verification is skipped (the node uses a self-signed
//     cert). Pinning is therefore optional; for best security provide the cert
//     (the panel auto-fetches it on registration when possible).
func New(baseURL, masterKey, certPEM string) *Client {
	httpClient := &http.Client{Timeout: 15 * time.Second}
	httpClient.Transport = &http.Transport{TLSClientConfig: tlsConfigFor(certPEM)}
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		masterKey: masterKey,
		http:      httpClient,
	}
}

// IsPinned reports whether a stored certificate actually yields a pin. It is
// false both when no certificate was stored and when the stored PEM is
// unparseable — in either case requests to the node run WITHOUT verification
// and the master key rides on every one of them, so callers should surface this
// rather than let it pass for a healthy node.
func IsPinned(certPEM string) bool {
	certPEM = strings.TrimSpace(certPEM)
	if certPEM == "" {
		return false
	}
	block, _ := pem.Decode([]byte(certPEM))
	return block != nil
}

// tlsConfigFor builds a TLS config that pins the exact certificate when one is
// provided, or skips verification (self-signed friendly) when it is empty.
//
// The unpinned fallback is deliberate — nodes use self-signed certificates —
// but it is indistinguishable from success at the transport layer, so the
// panel exposes IsPinned in the node API/UI to make the weaker state visible.
func tlsConfigFor(certPEM string) *tls.Config {
	certPEM = strings.TrimSpace(certPEM)
	if certPEM == "" {
		return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // optional pinning; self-signed nodes
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		log.Printf("nodeclient: stored certificate is not valid PEM; continuing WITHOUT certificate pinning")
		return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // malformed pin -> fall back
	}
	pinned := block.Bytes
	return &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // exact-certificate pin below
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			for _, raw := range rawCerts {
				if bytes.Equal(raw, pinned) {
					return nil
				}
			}
			return errors.New("node certificate does not match pinned certificate")
		},
	}
}

// FetchCert connects to an https node and returns its leaf certificate as PEM
// (trust-on-first-use). Returns an empty string with no error for non-https
// addresses (nothing to pin).
func FetchCert(address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	if u.Scheme != "" && u.Scheme != "https" {
		return "", nil
	}
	host := u.Host
	if host == "" {
		host = address
	}
	if !strings.Contains(host, ":") {
		host += ":443"
	}
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 10 * time.Second}, "tcp", host,
		&tls.Config{InsecureSkipVerify: true}, //nolint:gosec // TOFU: we read and pin the cert
	)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", errors.New("node presented no certificate")
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certs[0].Raw})), nil
}

// Health is the node's health response.
type Health struct {
	Status      string `json:"status"`
	CoreStarted bool   `json:"core_started"`
	CoreVersion string `json:"core_version"`
}

// TenantView mirrors the node's tenant representation.
type TenantView struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	Reason           string `json:"reason,omitempty"`
	PeriodID         uint64 `json:"period_id"`
	QuotaBytes       int64  `json:"quota_bytes"`
	UsedBytes        int64  `json:"used_bytes"`
	CreditLimitBytes int64  `json:"credit_limit_bytes"`
	ExpireAt         int64  `json:"expire_at"`
}

// UsageView mirrors the node's usage representation (absolute cumulative).
type UsageView struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	PeriodID     uint64 `json:"period_id"`
	QuotaBytes   int64  `json:"quota_bytes"`
	UsedBytes    int64  `json:"used_bytes"`
	OverageBytes int64  `json:"overage_bytes"`
	RemainBytes  int64  `json:"remaining_bytes"`
	CreditLimit  int64  `json:"credit_limit_bytes"`
	ExpireAt     int64  `json:"expire_at"`
}

// CreateTenantRequest provisions a tenant on the node.
type CreateTenantRequest struct {
	ID               string `json:"id"`
	APIKey           string `json:"api_key"`
	QuotaBytes       int64  `json:"quota_bytes"`
	CreditLimitBytes int64  `json:"credit_limit_bytes"`
	ExpireAt         int64  `json:"expire_at"`
	PeriodID         uint64 `json:"period_id,omitempty"`
}

// SetQuotaRequest updates a tenant's quota/credit/expiry.
type SetQuotaRequest struct {
	QuotaBytes       int64 `json:"quota_bytes"`
	CreditLimitBytes int64 `json:"credit_limit_bytes"`
	ExpireAt         int64 `json:"expire_at"`
}

func (c *Client) Health(ctx context.Context) (*Health, error) {
	var h Health
	if err := c.do(ctx, http.MethodGet, "/health", nil, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

func (c *Client) ApplyConfig(ctx context.Context, configJSON string) error {
	return c.do(ctx, http.MethodPost, "/admin/config", strings.NewReader(configJSON), nil)
}

// CoreStart/CoreStop/CoreRestart control the shared Xray core lifecycle.
func (c *Client) CoreStart(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/admin/core/start", nil, nil)
}
func (c *Client) CoreStop(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/admin/core/stop", nil, nil)
}
func (c *Client) CoreRestart(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/admin/core/restart", nil, nil)
}

// SetXrayVersion downloads the given Xray-core version on the node and restarts
// the core. Uses a long timeout because the node downloads a release.
func (c *Client) SetXrayVersion(ctx context.Context, version string) error {
	return c.postLong(ctx, "/admin/core/xray-version", map[string]string{"version": version})
}

// UpdateNode downloads the given NodeAgent release on the node and restarts the
// service.
func (c *Client) UpdateNode(ctx context.Context, version string) error {
	return c.postLong(ctx, "/admin/node/update", map[string]string{"version": version})
}

// postLong performs a JSON POST with a long timeout (node-side downloads).
func (c *Client) postLong(ctx context.Context, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.masterKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 180 * time.Second, Transport: c.http.Transport}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("node request POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("node POST %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// GetConfig returns the node's currently running core config as raw JSON.
func (c *Client) GetConfig(ctx context.Context) (json.RawMessage, error) {
	return c.getRaw(ctx, "/admin/config")
}

// GetInbounds returns the customer-shareable inbound definitions as raw JSON
// ({"inbounds":[...]}), so a buyer can replicate the connection in their panel.
func (c *Client) GetInbounds(ctx context.Context) (json.RawMessage, error) {
	return c.getRaw(ctx, "/admin/inbounds")
}

// getRaw performs a GET and returns the raw response body (for endpoints that
// return arbitrary JSON documents rather than a typed struct).
func (c *Client) getRaw(ctx context.Context, path string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.masterKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("node request GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("node GET %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return json.RawMessage(data), nil
}

func (c *Client) CreateTenant(ctx context.Context, req CreateTenantRequest) (*TenantView, error) {
	var tv TenantView
	if err := c.doJSON(ctx, http.MethodPost, "/admin/tenants", req, &tv); err != nil {
		return nil, err
	}
	return &tv, nil
}

func (c *Client) SetQuota(ctx context.Context, tenantID string, req SetQuotaRequest) (*TenantView, error) {
	var tv TenantView
	if err := c.doJSON(ctx, http.MethodPatch, "/admin/tenants/"+tenantID+"/quota", req, &tv); err != nil {
		return nil, err
	}
	return &tv, nil
}

func (c *Client) Suspend(ctx context.Context, tenantID string) error {
	return c.do(ctx, http.MethodPost, "/admin/tenants/"+tenantID+"/suspend", nil, nil)
}

func (c *Client) Resume(ctx context.Context, tenantID string) error {
	return c.do(ctx, http.MethodPost, "/admin/tenants/"+tenantID+"/resume", nil, nil)
}

// ResetPeriod starts a new period on the node (usage zeroed, reactivated).
func (c *Client) ResetPeriod(ctx context.Context, tenantID string) error {
	return c.do(ctx, http.MethodPost, "/admin/tenants/"+tenantID+"/reset", nil, nil)
}

func (c *Client) Delete(ctx context.Context, tenantID string) error {
	return c.do(ctx, http.MethodDelete, "/admin/tenants/"+tenantID, nil, nil)
}

func (c *Client) TenantUsage(ctx context.Context, tenantID string) (*UsageView, error) {
	var uv UsageView
	if err := c.do(ctx, http.MethodGet, "/admin/tenants/"+tenantID+"/usage", nil, &uv); err != nil {
		return nil, err
	}
	return &uv, nil
}

// --- internals ---

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.do(ctx, method, path, bytes.NewReader(data), out)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.masterKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("node request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("node %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
			return fmt.Errorf("decode node response: %w", err)
		}
	}
	return nil
}
