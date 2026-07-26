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

func (s *Store) CreateNode(name, address, masterKey, certPEM, configJSON string, grpcPort int, coreKey, hostInfo string) (*domain.Node, error) {
	if grpcPort <= 0 {
		grpcPort = 62050
	}
	n := &domain.Node{
		ID: uuid.NewString(), Name: name, Address: address, MasterKey: masterKey,
		CoreKey: coreKey, CertPEM: certPEM, ConfigJSON: configJSON, HostInfo: hostInfo,
		GRPCPort: grpcPort, Status: "unknown", CreatedAt: time.Now().Unix(),
	}
	_, err := s.db.Exec(
		`INSERT INTO nodes (id, name, address, master_key, core_key, cert_pem, config_json, host_info, grpc_port, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Name, n.Address, n.MasterKey, n.CoreKey, n.CertPEM, n.ConfigJSON, n.HostInfo, n.GRPCPort, n.Status, n.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return n, nil
}

// NodeUpdate carries a partial node edit. A nil field means "leave unchanged";
// a non-nil pointer to "" means "clear it".
//
// Pointers, not plain strings: the route is a PATCH, and with plain strings an
// absent field is indistinguishable from an empty one. That silently wiped
// core_key and host_info for any client that sent only the fields it meant to
// change — the buyer-facing host note would vanish with nothing reporting an
// error.
type NodeUpdate struct {
	Name      *string
	Address   *string
	GRPCPort  *int
	MasterKey *string
	CoreKey   *string
	CertPEM   *string
	HostInfo  *string
}

// UpdateNode applies a partial edit to a node.
func (s *Store) UpdateNode(id string, upd NodeUpdate) error {
	cur, err := s.GetNode(id)
	if err != nil {
		return err
	}

	name := pick(upd.Name, cur.Name)
	address := pick(upd.Address, cur.Address)
	masterKey := pick(upd.MasterKey, cur.MasterKey)
	coreKey := pick(upd.CoreKey, cur.CoreKey)
	certPEM := pick(upd.CertPEM, cur.CertPEM)
	hostInfo := pick(upd.HostInfo, cur.HostInfo)
	grpcPort := cur.GRPCPort
	if upd.GRPCPort != nil && *upd.GRPCPort > 0 {
		grpcPort = *upd.GRPCPort
	}

	_, err = s.db.Exec(
		`UPDATE nodes SET name = ?, address = ?, grpc_port = ?, master_key = ?, core_key = ?, cert_pem = ?, host_info = ? WHERE id = ?`,
		name, address, grpcPort, masterKey, coreKey, certPEM, hostInfo, id,
	)
	return err
}

// pick returns *v when set, otherwise the current value.
func pick(v *string, current string) string {
	if v == nil {
		return current
	}
	return *v
}

func (s *Store) SetNodeHostInfo(id, hostInfo string) error {
	_, err := s.db.Exec(`UPDATE nodes SET host_info = ? WHERE id = ?`, hostInfo, id)
	return err
}

func scanNode(row interface{ Scan(...any) error }) (*domain.Node, error) {
	var n domain.Node
	var coreKey, certPEM, configJSON, hostInfo, version sql.NullString
	var lastSeen sql.NullInt64
	err := row.Scan(&n.ID, &n.Name, &n.Address, &n.MasterKey, &coreKey, &certPEM, &configJSON, &hostInfo, &n.GRPCPort, &n.Status, &version, &n.CapacityScore, &lastSeen, &n.CreatedAt)
	if err != nil {
		return nil, err
	}
	n.CoreKey = coreKey.String
	n.CertPEM = certPEM.String
	n.ConfigJSON = configJSON.String
	n.HostInfo = hostInfo.String
	n.Version = version.String
	n.LastSeenAt = lastSeen.Int64
	return &n, nil
}

const nodeColumns = `id, name, address, master_key, core_key, cert_pem, config_json, host_info, grpc_port, status, version, capacity_score, last_seen_at, created_at`

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
