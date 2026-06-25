package api

import (
	"context"
	"log"
	"time"

	"github.com/pasarguard/panel/internal/domain"
	"github.com/pasarguard/panel/internal/nodeclient"
	"github.com/pasarguard/panel/internal/webhook"
)

// RunUsageCollector periodically pulls absolute cumulative usage for each active
// subscription from its node and stores it. This is the panel's source of truth
// for billing exposure; enforcement itself happens locally on the node.
func (a *API) RunUsageCollector(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.collectUsageOnce(ctx)
		}
	}
}

func (a *API) collectUsageOnce(ctx context.Context) {
	subs, err := a.store.ListActiveSubscriptions()
	if err != nil {
		log.Printf("usage collector: list subscriptions: %v", err)
		return
	}

	clients := make(map[string]*nodeclient.Client)
	for _, sub := range subs {
		client, ok := clients[sub.NodeID]
		if !ok {
			node, err := a.store.GetNode(sub.NodeID)
			if err != nil {
				continue
			}
			client = a.clientForNode(node)
			clients[sub.NodeID] = client
		}

		uv, err := client.TenantUsage(ctx, sub.NodeTenantID)
		if err != nil {
			log.Printf("usage collector: tenant %s on node %s: %v", sub.NodeTenantID, sub.NodeID, err)
			continue
		}

		_ = a.store.UpdateSubscriptionUsage(sub.ID, uv.UsedBytes)
		_ = a.store.RecordUsage(sub.NodeTenantID, sub.NodeID, uv.PeriodID, uv.UsedBytes)

		// Emit usage threshold / over-quota events (once each per period).
		a.emitUsageEvents(ctx, sub, uv.UsedBytes)

		// Reflect the node's enforcement decision back into the panel and notify.
		if uv.Status != "" && uv.Status != sub.Status {
			_ = a.store.UpdateSubscriptionStatus(sub.ID, uv.Status)
			a.emitStatusEvent(ctx, sub, uv.Status)
		}
	}
}

// emitUsageEvents fires usage.threshold (80/95) and usage.over_quota (100) once
// each per period, tracked via the subscription's notified_threshold.
func (a *API) emitUsageEvents(ctx context.Context, sub domain.Subscription, used int64) {
	level := thresholdLevel(used, sub.QuotaBytes)
	if level <= sub.NotifiedThreshold {
		return
	}

	overage := used - sub.QuotaBytes
	if overage < 0 {
		overage = 0
	}
	data := map[string]any{
		"customer_id":     sub.CustomerID,
		"subscription_id": sub.ID,
		"period_id":       sub.PeriodID,
		"used_bytes":      used,
		"quota_bytes":     sub.QuotaBytes,
		"overage_bytes":   overage,
		"threshold":       level,
	}

	if level >= 100 {
		a.wh.Emit(ctx, webhook.EventUsageOverQuota, data)
	} else {
		a.wh.Emit(ctx, webhook.EventUsageThreshold, data)
	}
	_ = a.store.UpdateSubscriptionNotified(sub.ID, level)
}

func (a *API) emitStatusEvent(ctx context.Context, sub domain.Subscription, newStatus string) {
	data := map[string]any{
		"customer_id":     sub.CustomerID,
		"subscription_id": sub.ID,
		"status":          newStatus,
	}
	switch newStatus {
	case "suspended":
		a.wh.Emit(ctx, webhook.EventSuspended, data)
	case "active":
		a.wh.Emit(ctx, webhook.EventResumed, data)
	case "expired":
		a.wh.Emit(ctx, webhook.EventExpired, data)
	}
}

// thresholdLevel maps a usage ratio to a notification level: 0, 80, 95 or 100.
func thresholdLevel(used, quota int64) int {
	if quota <= 0 {
		if used > 0 {
			return 100
		}
		return 0
	}
	pct := used * 100 / quota
	switch {
	case pct >= 100:
		return 100
	case pct >= 95:
		return 95
	case pct >= 80:
		return 80
	default:
		return 0
	}
}
