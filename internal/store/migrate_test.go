package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A database created by the old schema has usage_records referencing tenants —
// a table nothing ever inserts into. With foreign keys on, every usage sample
// therefore failed to insert and the history was silently always empty. Opening
// such a database must rebuild the table so recording works from then on.
func TestMigrateUsageRecordsDropsTenantsForeignKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Build a legacy-shaped database directly.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	legacy := []string{
		`CREATE TABLE customers (id TEXT PRIMARY KEY, name TEXT NOT NULL, external_ref TEXT, status TEXT, created_at INTEGER NOT NULL)`,
		`CREATE TABLE nodes (id TEXT PRIMARY KEY, name TEXT NOT NULL, address TEXT NOT NULL, master_key TEXT NOT NULL,
			cert_pem TEXT, config_json TEXT, status TEXT, version TEXT, capacity_score INTEGER NOT NULL DEFAULT 0,
			last_seen_at INTEGER, created_at INTEGER NOT NULL)`,
		`CREATE TABLE tenants (id TEXT PRIMARY KEY, customer_id TEXT NOT NULL, node_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'provisioning', created_at INTEGER NOT NULL)`,
		`CREATE TABLE usage_records (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			node_id TEXT NOT NULL,
			period_id INTEGER NOT NULL,
			ts INTEGER NOT NULL,
			used_bytes_cumulative INTEGER NOT NULL)`,
	}
	for _, stmt := range legacy {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("legacy schema %q: %v", firstLine(stmt), err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy db: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var ddl string
	if err := st.db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='usage_records'`,
	).Scan(&ddl); err != nil {
		t.Fatalf("read ddl: %v", err)
	}
	if strings.Contains(ddl, "REFERENCES tenants") {
		t.Fatalf("usage_records still references tenants after migration:\n%s", ddl)
	}

	// The whole point: a sample for a node-side tenant id now inserts.
	if err := st.RecordUsage("node-tenant-1", "node-1", 1, 4242); err != nil {
		t.Fatalf("RecordUsage after migration: %v", err)
	}
	n, err := st.CountUsageRecords()
	if err != nil || n != 1 {
		t.Fatalf("usage records = %d err=%v, want 1", n, err)
	}

	// Re-opening is a no-op, not a repeated rebuild.
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	if n, err := st2.CountUsageRecords(); err != nil || n != 1 {
		t.Fatalf("after reopen usage records = %d err=%v, want the row preserved", n, err)
	}
}

// A fresh database gets the corrected shape straight from schema.sql and can
// record usage immediately.
func TestFreshDatabaseRecordsUsage(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if err := st.RecordUsage("node-tenant-1", "node-1", 1, 100); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if n, err := st.CountUsageRecords(); err != nil || n != 1 {
		t.Fatalf("usage records = %d err=%v, want 1", n, err)
	}
}
