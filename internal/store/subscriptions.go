package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/pasarguard/panel/internal/domain"
)

const subColumns = `id, customer_id, plan_id, node_id, node_tenant_id, status, period_id,
	start_at, end_at, quota_bytes, used_bytes, credit_limit_bytes, notified_threshold, created_at, sub_token, api_key`

// CreateSubscription persists a new subscription row.
func (s *Store) CreateSubscription(sub *domain.Subscription) error {
	if sub.ID == "" {
		sub.ID = uuid.NewString()
	}
	if sub.CreatedAt == 0 {
		sub.CreatedAt = time.Now().Unix()
	}
	if sub.PeriodID == 0 {
		sub.PeriodID = 1
	}
	if sub.Status == "" {
		sub.Status = "active"
	}
	if sub.SubToken == "" {
		sub.SubToken = newSubToken()
	}
	_, err := s.db.Exec(
		`INSERT INTO subscriptions (`+subColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sub.ID, sub.CustomerID, sub.PlanID, sub.NodeID, sub.NodeTenantID, sub.Status, sub.PeriodID,
		sub.StartAt, sub.EndAt, sub.QuotaBytes, sub.UsedBytes, sub.CreditLimitBytes, sub.NotifiedThreshold, sub.CreatedAt, sub.SubToken, sub.APIKey,
	)
	return err
}

func scanSubscription(row interface{ Scan(...any) error }) (*domain.Subscription, error) {
	var sub domain.Subscription
	var planID, nodeID, nodeTenantID, subToken, apiKey sql.NullString
	err := row.Scan(
		&sub.ID, &sub.CustomerID, &planID, &nodeID, &nodeTenantID, &sub.Status, &sub.PeriodID,
		&sub.StartAt, &sub.EndAt, &sub.QuotaBytes, &sub.UsedBytes, &sub.CreditLimitBytes, &sub.NotifiedThreshold, &sub.CreatedAt, &subToken, &apiKey,
	)
	if err != nil {
		return nil, err
	}
	sub.PlanID = planID.String
	sub.NodeID = nodeID.String
	sub.NodeTenantID = nodeTenantID.String
	sub.SubToken = subToken.String
	sub.APIKey = apiKey.String
	return &sub, nil
}

func (s *Store) GetSubscription(id string) (*domain.Subscription, error) {
	row := s.db.QueryRow(`SELECT `+subColumns+` FROM subscriptions WHERE id = ?`, id)
	sub, err := scanSubscription(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sub, err
}

// GetSubscriptionByToken loads a subscription by its public page token.
func (s *Store) GetSubscriptionByToken(token string) (*domain.Subscription, error) {
	row := s.db.QueryRow(`SELECT `+subColumns+` FROM subscriptions WHERE sub_token = ?`, token)
	sub, err := scanSubscription(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sub, err
}

func (s *Store) ListSubscriptionsByCustomer(customerID string) ([]domain.Subscription, error) {
	return s.querySubscriptions(`SELECT `+subColumns+` FROM subscriptions WHERE customer_id = ? ORDER BY created_at DESC`, customerID)
}

// ListActiveSubscriptions returns subscriptions that are provisioned on a node
// and not expired, for the usage collector.
func (s *Store) ListActiveSubscriptions() ([]domain.Subscription, error) {
	return s.querySubscriptions(`SELECT ` + subColumns + ` FROM subscriptions WHERE node_tenant_id != '' AND status = 'active'`)
}

func (s *Store) querySubscriptions(query string, args ...any) ([]domain.Subscription, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Subscription, 0)
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sub)
	}
	return out, rows.Err()
}

// UpdateSubscriptionUsage sets the absolute cumulative used bytes.
func (s *Store) UpdateSubscriptionUsage(id string, usedBytes int64) error {
	_, err := s.db.Exec(`UPDATE subscriptions SET used_bytes = ? WHERE id = ?`, usedBytes, id)
	return err
}

// UpdateSubscriptionStatus updates the subscription lifecycle status.
func (s *Store) UpdateSubscriptionStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE subscriptions SET status = ? WHERE id = ?`, status, id)
	return err
}

// SetSubscriptionQuota updates quota/credit/expiry on the panel side.
func (s *Store) SetSubscriptionQuota(id string, quotaBytes, creditLimitBytes, endAt int64) error {
	_, err := s.db.Exec(
		`UPDATE subscriptions SET quota_bytes = ?, credit_limit_bytes = ?, end_at = ? WHERE id = ?`,
		quotaBytes, creditLimitBytes, endAt, id,
	)
	return err
}

// UpdateSubscriptionNotified records the highest usage threshold already sent.
func (s *Store) UpdateSubscriptionNotified(id string, level int) error {
	_, err := s.db.Exec(`UPDATE subscriptions SET notified_threshold = ? WHERE id = ?`, level, id)
	return err
}

// RecordUsage appends a cumulative usage sample for auditing/history.
func (s *Store) RecordUsage(tenantID, nodeID string, periodID uint64, usedCumulative int64) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_records (id, tenant_id, node_id, period_id, ts, used_bytes_cumulative)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), tenantID, nodeID, periodID, time.Now().Unix(), usedCumulative,
	)
	return err
}

// CreateAPIKey stores the hash of a customer key (the raw key is shown once).
func (s *Store) CreateAPIKey(customerID, keyHash, prefix string) error {
	_, err := s.db.Exec(
		`INSERT INTO api_keys (id, customer_id, key_hash, prefix, status, created_at) VALUES (?, ?, ?, ?, 'active', ?)`,
		uuid.NewString(), customerID, keyHash, prefix, time.Now().Unix(),
	)
	return err
}
