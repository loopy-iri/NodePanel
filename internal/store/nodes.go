package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/pasarguard/panel/internal/domain"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

func (s *Store) CreateNode(name, address, masterKey, certPEM, configJSON string, grpcPort int) (*domain.Node, error) {
	if grpcPort <= 0 {
		grpcPort = 62050
	}
	n := &domain.Node{
		ID:         uuid.NewString(),
		Name:       name,
		Address:    address,
		MasterKey:  masterKey,
		CertPEM:    certPEM,
		ConfigJSON: configJSON,
		GRPCPort:   grpcPort,
		Status:     "unknown",
		CreatedAt:  time.Now().Unix(),
	}
	_, err := s.db.Exec(
		`INSERT INTO nodes (id, name, address, master_key, cert_pem, config_json, grpc_port, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Name, n.Address, n.MasterKey, n.CertPEM, n.ConfigJSON, n.GRPCPort, n.Status, n.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func scanNode(row interface{ Scan(...any) error }) (*domain.Node, error) {
	var n domain.Node
	var certPEM, configJSON, version sql.NullString
	var lastSeen sql.NullInt64
	err := row.Scan(&n.ID, &n.Name, &n.Address, &n.MasterKey, &certPEM, &configJSON, &n.GRPCPort, &n.Status, &version, &n.CapacityScore, &lastSeen, &n.CreatedAt)
	if err != nil {
		return nil, err
	}
	n.CertPEM = certPEM.String
	n.ConfigJSON = configJSON.String
	n.Version = version.String
	n.LastSeenAt = lastSeen.Int64
	return &n, nil
}

const nodeColumns = `id, name, address, master_key, cert_pem, config_json, grpc_port, status, version, capacity_score, last_seen_at, created_at`

func (s *Store) GetNode(id string) (*domain.Node, error) {
	row := s.db.QueryRow(`SELECT `+nodeColumns+` FROM nodes WHERE id = ?`, id)
	n, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

func (s *Store) ListNodes() ([]domain.Node, error) {
	rows, err := s.db.Query(`SELECT ` + nodeColumns + ` FROM nodes ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Node, 0)
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func (s *Store) UpdateNodeHealth(id, status, version string, lastSeen int64) error {
	_, err := s.db.Exec(
		`UPDATE nodes SET status = ?, version = ?, last_seen_at = ? WHERE id = ?`,
		status, version, lastSeen, id,
	)
	return err
}

// GetPlan loads a plan by id.
func (s *Store) GetPlan(id string) (*domain.Plan, error) {
	row := s.db.QueryRow(
		`SELECT id, name, quota_bytes, duration_days, max_users, created_at FROM plans WHERE id = ?`, id,
	)
	var p domain.Plan
	err := row.Scan(&p.ID, &p.Name, &p.QuotaBytes, &p.DurationDays, &p.MaxUsers, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// GetCustomer loads a customer by id.
func (s *Store) GetCustomer(id string) (*domain.Customer, error) {
	row := s.db.QueryRow(
		`SELECT id, name, status, COALESCE(external_ref, ''), created_at FROM customers WHERE id = ?`, id,
	)
	var c domain.Customer
	err := row.Scan(&c.ID, &c.Name, &c.Status, &c.ExternalRef, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}
