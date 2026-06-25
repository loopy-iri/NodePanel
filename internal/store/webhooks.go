package store

import (
	"time"

	"github.com/google/uuid"
	"github.com/pasarguard/panel/internal/domain"
)

// CreateWebhook registers a webhook destination.
func (s *Store) CreateWebhook(url, secret, events string) (*domain.WebhookEndpoint, error) {
	if events == "" {
		events = "*"
	}
	wh := &domain.WebhookEndpoint{
		ID:     uuid.NewString(),
		URL:    url,
		Secret: secret,
		Events: events,
		Status: "active",
	}
	_, err := s.db.Exec(
		`INSERT INTO webhook_endpoints (id, url, secret, events, status) VALUES (?, ?, ?, ?, ?)`,
		wh.ID, wh.URL, wh.Secret, wh.Events, wh.Status,
	)
	if err != nil {
		return nil, err
	}
	return wh, nil
}

// ListActiveWebhooks returns enabled webhook endpoints.
func (s *Store) ListActiveWebhooks() ([]domain.WebhookEndpoint, error) {
	rows, err := s.db.Query(`SELECT id, url, secret, events, status FROM webhook_endpoints WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.WebhookEndpoint, 0)
	for rows.Next() {
		var wh domain.WebhookEndpoint
		if err := rows.Scan(&wh.ID, &wh.URL, &wh.Secret, &wh.Events, &wh.Status); err != nil {
			return nil, err
		}
		out = append(out, wh)
	}
	return out, rows.Err()
}

// ListWebhooks returns all webhook endpoints (for the admin list view).
func (s *Store) ListWebhooks() ([]domain.WebhookEndpoint, error) {
	rows, err := s.db.Query(`SELECT id, url, secret, events, status FROM webhook_endpoints ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.WebhookEndpoint, 0)
	for rows.Next() {
		var wh domain.WebhookEndpoint
		if err := rows.Scan(&wh.ID, &wh.URL, &wh.Secret, &wh.Events, &wh.Status); err != nil {
			return nil, err
		}
		out = append(out, wh)
	}
	return out, rows.Err()
}

// RecordWebhookDelivery stores the result of a delivery attempt.
func (s *Store) RecordWebhookDelivery(endpointID, event, payload, status string, attempts int) error {
	_, err := s.db.Exec(
		`INSERT INTO webhook_deliveries (id, endpoint_id, event, payload, status, attempts, ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), endpointID, event, payload, status, attempts, time.Now().Unix(),
	)
	return err
}
