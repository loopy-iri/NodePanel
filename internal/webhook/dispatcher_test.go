package webhook

import "testing"

func TestSubscribed(t *testing.T) {
	cases := []struct {
		events string
		event  string
		want   bool
	}{
		{"*", EventUsageOverQuota, true},
		{"", EventResumed, true},
		{"usage.threshold,usage.over_quota", EventUsageOverQuota, true},
		{"usage.threshold, subscription.suspended", EventSuspended, true},
		{"usage.threshold", EventUsageOverQuota, false},
		{"subscription.suspended", EventResumed, false},
	}
	for _, c := range cases {
		if got := subscribed(c.events, c.event); got != c.want {
			t.Errorf("subscribed(%q, %q) = %v, want %v", c.events, c.event, got, c.want)
		}
	}
}

func TestSignDeterministic(t *testing.T) {
	body := []byte(`{"type":"x"}`)
	a := sign("secret", body)
	b := sign("secret", body)
	if a != b || a == "" {
		t.Fatalf("sign not deterministic: %q vs %q", a, b)
	}
	if sign("other", body) == a {
		t.Fatal("different secret produced same signature")
	}
}
