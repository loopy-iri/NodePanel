package store

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pasarguard/panel/internal/domain"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Store wraps the SQLite database used by the panel.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database and applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// SQLite works best with a single writer connection.
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}

	if err := applySchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db}, nil
}

// migrate applies idempotent, additive migrations for databases created by an
// older schema. Adding a column that already exists is ignored.
func migrate(db *sql.DB) error {
	addColumns := []string{
		`ALTER TABLE nodes ADD COLUMN grpc_port INTEGER NOT NULL DEFAULT 62050`,
		`ALTER TABLE nodes ADD COLUMN core_key TEXT`,
		`ALTER TABLE nodes ADD COLUMN host_info TEXT`,
		`ALTER TABLE subscriptions ADD COLUMN sub_token TEXT`,
		`ALTER TABLE subscriptions ADD COLUMN api_key TEXT`,
	}
	for _, stmt := range addColumns {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("statement %q: %w", firstLine(stmt), err)
		}
	}
	if err := migrateUsageRecords(db); err != nil {
		return err
	}
	// Backfill subscription tokens for rows created before this column existed.
	rows, err := db.Query(`SELECT id FROM subscriptions WHERE sub_token IS NULL OR sub_token = ''`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	for _, id := range ids {
		if _, err := db.Exec(`UPDATE subscriptions SET sub_token = ? WHERE id = ?`, newSubToken(), id); err != nil {
			return err
		}
	}
	return nil
}

// newSubToken returns an unguessable token (32 hex chars) for a public sub page.
func newSubToken() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

// migrateUsageRecords rebuilds usage_records without its foreign key to
// tenants.
//
// tenant_id holds the tenant id on the NODE, which is not a row in the local
// tenants table — and nothing ever inserts into tenants. With
// PRAGMA foreign_keys=ON every insert therefore failed the FK check, and
// because the caller discarded the error, usage history was silently empty for
// the life of the database. SQLite cannot drop a constraint in place, so the
// table is rebuilt; existing rows are copied, though on affected databases
// there are none by definition.
func migrateUsageRecords(db *sql.DB) error {
	var ddl string
	err := db.QueryRow(
		`SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name='usage_records'`,
	).Scan(&ddl)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // fresh database: schema.sql already created the good shape
	}
	if err != nil {
		return err
	}
	if !strings.Contains(ddl, "REFERENCES tenants") {
		return nil // already migrated
	}

	stmts := []string{
		`CREATE TABLE usage_records_new (
			id                    TEXT PRIMARY KEY,
			tenant_id             TEXT NOT NULL,
			node_id               TEXT NOT NULL,
			period_id             INTEGER NOT NULL,
			ts                    INTEGER NOT NULL,
			used_bytes_cumulative INTEGER NOT NULL
		)`,
		`INSERT INTO usage_records_new (id, tenant_id, node_id, period_id, ts, used_bytes_cumulative)
		 SELECT id, tenant_id, node_id, period_id, ts, used_bytes_cumulative FROM usage_records`,
		`DROP TABLE usage_records`,
		`ALTER TABLE usage_records_new RENAME TO usage_records`,
		`CREATE INDEX IF NOT EXISTS idx_usage_tenant ON usage_records(tenant_id, period_id)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("usage_records migration %q: %w", firstLine(stmt), err)
		}
	}
	return nil
}

// applySchema executes each statement in schema.sql individually so the code
// does not depend on the driver supporting multi-statement Exec. Line comments
// are stripped first: the file is split on ";", and a semicolon inside a
// comment would otherwise cut a statement in half.
func applySchema(db *sql.DB) error {
	for _, stmt := range strings.Split(stripSQLComments(schema), ";") {
		s := strings.TrimSpace(stmt)
		if s == "" {
			continue
		}
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("statement %q: %w", firstLine(s), err)
		}
	}
	return nil
}

func stripSQLComments(sql string) string {
	lines := strings.Split(sql, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func (s *Store) Close() error { return s.db.Close() }

// --- Customers ---

func (s *Store) CreateCustomer(name, externalRef string) (*domain.Customer, error) {
	c := &domain.Customer{
		ID:          uuid.NewString(),
		Name:        name,
		Status:      "active",
		ExternalRef: externalRef,
		CreatedAt:   time.Now().Unix(),
	}
	_, err := s.db.Exec(
		`INSERT INTO customers (id, name, status, external_ref, created_at) VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Status, c.ExternalRef, c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ListCustomers() ([]domain.Customer, error) {
	rows, err := s.db.Query(
		`SELECT id, name, status, COALESCE(external_ref, ''), created_at FROM customers ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Customer, 0)
	for rows.Next() {
		var c domain.Customer
		if err := rows.Scan(&c.ID, &c.Name, &c.Status, &c.ExternalRef, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- Plans ---

func (s *Store) CreatePlan(name string, quotaBytes int64, durationDays, maxUsers int) (*domain.Plan, error) {
	p := &domain.Plan{
		ID:           uuid.NewString(),
		Name:         name,
		QuotaBytes:   quotaBytes,
		DurationDays: durationDays,
		MaxUsers:     maxUsers,
		CreatedAt:    time.Now().Unix(),
	}
	_, err := s.db.Exec(
		`INSERT INTO plans (id, name, quota_bytes, duration_days, max_users, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.QuotaBytes, p.DurationDays, p.MaxUsers, p.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) ListPlans() ([]domain.Plan, error) {
	rows, err := s.db.Query(
		`SELECT id, name, quota_bytes, duration_days, max_users, created_at FROM plans ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Plan, 0)
	for rows.Next() {
		var p domain.Plan
		if err := rows.Scan(&p.ID, &p.Name, &p.QuotaBytes, &p.DurationDays, &p.MaxUsers, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
