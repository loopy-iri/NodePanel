package store

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestNodeHostInfoRoundTrip(t *testing.T) {
	st := openTestStore(t)
	n, err := st.CreateNode("n1", "https://1.2.3.4:8090", "mk", "", "", 62050, "", "SNI: example.com")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if n.HostInfo != "SNI: example.com" {
		t.Fatalf("HostInfo = %q", n.HostInfo)
	}
	got, err := st.GetNode(n.ID)
	if err != nil || got.HostInfo != "SNI: example.com" {
		t.Fatalf("GetNode HostInfo = %q err=%v", got.HostInfo, err)
	}
	if err := st.SetNodeHostInfo(n.ID, "new host"); err != nil {
		t.Fatalf("SetNodeHostInfo: %v", err)
	}
	got, _ = st.GetNode(n.ID)
	if got.HostInfo != "new host" {
		t.Fatalf("after set HostInfo = %q", got.HostInfo)
	}
}
