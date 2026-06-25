-- Main Panel schema (control plane).
-- The panel stores bytes/time/status only. Money, wallet, pricing and payments
-- live in the external sales bot. external_ref maps a panel customer to the bot.

CREATE TABLE IF NOT EXISTS admins (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS customers (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'active',   -- active|disabled
    external_ref TEXT,                              -- customer id inside the sales bot
    created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS plans (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    quota_bytes   INTEGER NOT NULL,
    duration_days INTEGER NOT NULL,
    max_users     INTEGER NOT NULL DEFAULT 0,       -- 0 = unlimited
    created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id                 TEXT PRIMARY KEY,
    customer_id        TEXT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    plan_id            TEXT REFERENCES plans(id),
    node_id            TEXT REFERENCES nodes(id),
    node_tenant_id     TEXT,                            -- tenant id on the node
    status             TEXT NOT NULL DEFAULT 'active', -- active|suspended|expired
    period_id          INTEGER NOT NULL DEFAULT 1,
    start_at           INTEGER NOT NULL,
    end_at             INTEGER NOT NULL,
    quota_bytes        INTEGER NOT NULL,
    used_bytes         INTEGER NOT NULL DEFAULT 0,
    credit_limit_bytes INTEGER NOT NULL DEFAULT 0,    -- extra allowed bytes before auto-suspend (set by bot)
    notified_threshold INTEGER NOT NULL DEFAULT 0,    -- highest usage threshold already sent (0/80/95/100)
    created_at         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_subscriptions_customer ON subscriptions(customer_id);

CREATE TABLE IF NOT EXISTS api_keys (
    id          TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    key_hash    TEXT NOT NULL UNIQUE,
    prefix      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',       -- active|revoked
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_api_keys_customer ON api_keys(customer_id);

CREATE TABLE IF NOT EXISTS nodes (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    address        TEXT NOT NULL,
    master_key     TEXT NOT NULL,                     -- credential the panel uses to control the node
    cert_pem       TEXT,                              -- node's pinned self-signed certificate (PEM)
    config_json    TEXT,                              -- last fixed Xray config pushed to the node
    status         TEXT NOT NULL DEFAULT 'unknown',   -- online|offline|unknown
    version        TEXT,
    capacity_score INTEGER NOT NULL DEFAULT 0,
    last_seen_at   INTEGER,
    created_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS tenants (
    id          TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    node_id     TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'provisioning', -- provisioning|active|suspended|deleting
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tenants_customer ON tenants(customer_id);
CREATE INDEX IF NOT EXISTS idx_tenants_node ON tenants(node_id);

-- Absolute cumulative usage per tenant per node per period (idempotent: take max).
CREATE TABLE IF NOT EXISTS usage_records (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id               TEXT NOT NULL,
    period_id             INTEGER NOT NULL,
    ts                    INTEGER NOT NULL,
    used_bytes_cumulative INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_usage_tenant ON usage_records(tenant_id, period_id);

CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id      TEXT PRIMARY KEY,
    url     TEXT NOT NULL,
    secret  TEXT NOT NULL,
    events  TEXT NOT NULL DEFAULT '*',                -- comma-separated event names or *
    status  TEXT NOT NULL DEFAULT 'active'
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id          TEXT PRIMARY KEY,
    endpoint_id TEXT NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event       TEXT NOT NULL,
    payload     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',       -- pending|delivered|failed
    attempts    INTEGER NOT NULL DEFAULT 0,
    ts          INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id     TEXT PRIMARY KEY,
    actor  TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT,
    ts     INTEGER NOT NULL
);
