package store

import "time"

// --- Deletes ---

func (s *Store) DeleteNode(id string) error {
	res, err := s.db.Exec(`DELETE FROM nodes WHERE id = ?`, id)
	return checkAffected(res, err)
}

func (s *Store) DeletePlan(id string) error {
	res, err := s.db.Exec(`DELETE FROM plans WHERE id = ?`, id)
	return checkAffected(res, err)
}

// DeleteCustomer removes a customer; subscriptions and api_keys cascade via FK.
func (s *Store) DeleteCustomer(id string) error {
	res, err := s.db.Exec(`DELETE FROM customers WHERE id = ?`, id)
	return checkAffected(res, err)
}

func (s *Store) DeleteSubscription(id string) error {
	res, err := s.db.Exec(`DELETE FROM subscriptions WHERE id = ?`, id)
	return checkAffected(res, err)
}

// --- Status / lifecycle ---

func (s *Store) SetCustomerStatus(id, status string) error {
	res, err := s.db.Exec(`UPDATE customers SET status = ? WHERE id = ?`, status, id)
	return checkAffected(res, err)
}

// UpdateNodeCert stores/updates a node's pinned certificate (e.g. after TOFU).
func (s *Store) UpdateNodeCert(id, certPEM string) error {
	_, err := s.db.Exec(`UPDATE nodes SET cert_pem = ? WHERE id = ?`, certPEM, id)
	return err
}

// UpdateNodeConfig stores the last fixed Xray config pushed to a node.
func (s *Store) UpdateNodeConfig(id, configJSON string) error {
	_, err := s.db.Exec(`UPDATE nodes SET config_json = ? WHERE id = ?`, configJSON, id)
	return err
}

// RenewSubscription starts a new period: usage zeroed, period incremented,
// notifications re-armed, reactivated, and expiry set to endAt.
func (s *Store) RenewSubscription(id string, endAt int64) error {
	res, err := s.db.Exec(
		`UPDATE subscriptions
		 SET used_bytes = 0, period_id = period_id + 1, notified_threshold = 0,
		     status = 'active', end_at = ?
		 WHERE id = ?`,
		endAt, id,
	)
	return checkAffected(res, err)
}

func checkAffected(res interface {
	RowsAffected() (int64, error)
}, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// now is a small helper for handlers that need a timestamp.
func Now() int64 { return time.Now().Unix() }
