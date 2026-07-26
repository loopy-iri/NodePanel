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
	"sync"
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

// Dispatcher sends signed events to all subscribed endpoints. Deliveries run on
// a background worker: Emit only enqueues.
type Dispatcher struct {
	store Store
	http  *http.Client
	queue chan job
	once  sync.Once
}

// job is one queued delivery. The body is rendered at Emit time so the event
// reflects the state that produced it, not the state when it is finally sent.
type job struct {
	event string
	body  []byte
}

// queueDepth bounds memory if a consumer is down for a long time. Beyond it the
// oldest events are dropped with a log line rather than blocking the caller.
const queueDepth = 512

func NewDispatcher(store Store) *Dispatcher {
	return &Dispatcher{
		store: store,
		http:  &http.Client{Timeout: 10 * time.Second},
		queue: make(chan job, queueDepth),
	}
}

// Delivery retry policy: an endpoint that fails gets retried with a short
// backoff before the failure is recorded. Kept small so Emit (called from the
// collector loop) never blocks for long.
const (
	maxAttempts  = 3
	retryBackoff = 2 * time.Second
)

// Emit queues an event for delivery to every subscribed endpoint and returns
// immediately.
//
// It must not block its caller. Emit is called from inside the usage-collector
// loop and from HTTP handlers before they respond; delivering inline meant one
// unreachable consumer stalled the caller for the whole retry budget (~34s per
// endpoint). In the collector that starved usage collection for every other
// subscription — so an over-quota tenant kept running — and in a handler it
// hung an operator action that had in fact already taken effect on the node.
//
// The passed context is deliberately NOT used for delivery: it belongs to the
// request or poll that produced the event and is cancelled as soon as that
// finishes, which would cancel the very delivery it just queued.
func (d *Dispatcher) Emit(_ context.Context, eventType string, data any) {
	body, err := json.Marshal(Envelope{Type: eventType, Timestamp: time.Now().Unix(), Data: data})
	if err != nil {
		log.Printf("webhook: marshal %s: %v", eventType, err)
		return
	}
	d.once.Do(func() { go d.run() })

	select {
	case d.queue <- job{event: eventType, body: body}:
	default:
		log.Printf("webhook: delivery queue full (%d); dropping %s", queueDepth, eventType)
	}
}

// run drains the queue, delivering one event at a time. A single worker keeps
// ordering and avoids stampeding a struggling consumer.
func (d *Dispatcher) run() {
	for j := range d.queue {
		d.dispatch(j)
	}
}

// dispatch delivers one queued event to every subscribed endpoint, with its own
// timeout so a wedged consumer cannot occupy the worker indefinitely.
func (d *Dispatcher) dispatch(j job) {
	endpoints, err := d.store.ListActiveWebhooks()
	if err != nil {
		log.Printf("webhook: list endpoints: %v", err)
		return
	}
	for _, ep := range endpoints {
		if !subscribed(ep.Events, j.event) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), deliveryBudget)
		status, attempts := d.deliverWithRetry(ctx, ep, j.event, j.body)
		cancel()
		if err := d.store.RecordWebhookDelivery(ep.ID, j.event, string(j.body), status, attempts); err != nil {
			log.Printf("webhook: record delivery for %s: %v", ep.ID, err)
		}
	}
}

// deliveryBudget bounds one endpoint's full retry sequence.
const deliveryBudget = 60 * time.Second

// deliverWithRetry attempts delivery up to maxAttempts times, backing off
// between attempts. Returns the final status and the number of attempts made.
func (d *Dispatcher) deliverWithRetry(ctx context.Context, ep domain.WebhookEndpoint, eventType string, body []byte) (string, int) {
	var status string
	for attempt := 1; ; attempt++ {
		status = d.deliver(ctx, ep, eventType, body)
		if status == "delivered" || attempt >= maxAttempts {
			return status, attempt
		}
		select {
		case <-ctx.Done():
			return status, attempt
		case <-time.After(retryBackoff):
		}
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
