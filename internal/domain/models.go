package domain

// Customer is a buyer of node access. Money/wallet is tracked by the external
// sales bot; ExternalRef links this customer to the bot's record.
type Customer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	ExternalRef string `json:"external_ref,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

// Plan is a sellable package. It carries bytes/time/limits only, never price.
type Plan struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	QuotaBytes   int64  `json:"quota_bytes"`
	DurationDays int    `json:"duration_days"`
	MaxUsers     int    `json:"max_users"`
	CreatedAt    int64  `json:"created_at"`
}

// Node is a server in the fleet running a node agent. MasterKey is the panel's
// credential to control it and is never returned to API clients. CertPEM is the
// node's pinned self-signed certificate.
type Node struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Address       string `json:"address"`
	MasterKey     string `json:"-"`
	CertPEM       string `json:"-"`
	ConfigJSON    string `json:"-"`
	Status        string `json:"status"`
	Version       string `json:"version,omitempty"`
	CapacityScore int    `json:"capacity_score"`
	LastSeenAt    int64  `json:"last_seen_at,omitempty"`
	CreatedAt     int64  `json:"created_at"`
}

// Subscription ties a customer+plan to a tenant provisioned on a node.
type Subscription struct {
	ID                string `json:"id"`
	CustomerID        string `json:"customer_id"`
	PlanID            string `json:"plan_id"`
	NodeID            string `json:"node_id"`
	NodeTenantID      string `json:"node_tenant_id"`
	Status            string `json:"status"`
	PeriodID          uint64 `json:"period_id"`
	StartAt           int64  `json:"start_at"`
	EndAt             int64  `json:"end_at"`
	QuotaBytes        int64  `json:"quota_bytes"`
	UsedBytes         int64  `json:"used_bytes"`
	CreditLimitBytes  int64  `json:"credit_limit_bytes"`
	NotifiedThreshold int    `json:"notified_threshold"`
	CreatedAt         int64  `json:"created_at"`
}

// WebhookEndpoint is a destination the panel notifies (e.g. the sales bot).
type WebhookEndpoint struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Secret string `json:"-"`
	Events string `json:"events"` // comma-separated event names, or "*"
	Status string `json:"status"`
}
