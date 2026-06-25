// Package webhook delivers signed event notifications to external consumers
// (the sales bot). The panel emits usage/lifecycle events; the bot reacts by
// adjusting its wallet and calling the panel API. The panel itself holds no
// money logic.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pasarguard/panel/internal/domain"
)

// Event names emitted by the panel.
const (
	EventUsageThreshold = "usage.threshold"
	EventUsageOverQuota = "usage.over_quota"
	EventSuspended      = "subscription.suspended"
	EventResumed        = "subscription.resumed"
	EventExpired        = "subscription.expired"
)

// Store is the subset of persistence the dispatcher needs.
type Store interface {
	ListActiveWebhooks() ([]domain.WebhookEndpoint, error)
	RecordWebhookDelivery(endpointID, event, payload, status string, attempts int) error
}

// Envelope is the JSON body delivered to webhook endpoints.
type Envelope struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
	Data      any    `json:"data"`
}

// Dispatcher sends signed events to all subscribed endpoints.
type Dispatcher struct {
	store Store
	http  *http.Client
}

func NewDispatcher(store Store) *Dispatcher {
	return &Dispatcher{
		store: store,
		http:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Emit delivers an event to every active endpoint subscribed to it. Delivery is
// best-effort and recorded; failures are logged, not fatal.
func (d *Dispatcher) Emit(ctx context.Context, eventType string, data any) {
	endpoints, err := d.store.ListActiveWebhooks()
	if err != nil {
		log.Printf("webhook: list endpoints: %v", err)
		return
	}
	if len(endpoints) == 0 {
		return
	}

	body, err := json.Marshal(Envelope{Type: eventType, Timestamp: time.Now().Unix(), Data: data})
	if err != nil {
		log.Printf("webhook: marshal %s: %v", eventType, err)
		return
	}

	for _, ep := range endpoints {
		if !subscribed(ep.Events, eventType) {
			continue
		}
		status := d.deliver(ctx, ep, eventType, body)
		_ = d.store.RecordWebhookDelivery(ep.ID, eventType, string(body), status, 1)
	}
}

func (d *Dispatcher) deliver(ctx context.Context, ep domain.WebhookEndpoint, eventType string, body []byte) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(body))
	if err != nil {
		return "failed"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PG-Event", eventType)
	req.Header.Set("X-PG-Signature", "sha256="+sign(ep.Secret, body))

	resp, err := d.http.Do(req)
	if err != nil {
		log.Printf("webhook: deliver %s to %s: %v", eventType, ep.URL, err)
		return "failed"
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "delivered"
	}
	return fmt.Sprintf("failed_%d", resp.StatusCode)
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func subscribed(events, eventType string) bool {
	events = strings.TrimSpace(events)
	if events == "" || events == "*" {
		return true
	}
	for _, e := range strings.Split(events, ",") {
		if strings.TrimSpace(e) == eventType {
			return true
		}
	}
	return false
}
