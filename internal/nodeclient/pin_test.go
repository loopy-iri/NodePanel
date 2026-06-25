package nodeclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// genSelfSigned creates a fresh, unique self-signed EC certificate and returns
// both the tls.Certificate (for the server) and its public PEM (for pinning).
func genSelfSigned(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "test-node"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEMBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, _ := x509.MarshalPKCS8PrivateKey(priv)
	keyPEMBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEMBytes, keyPEMBytes)
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	return cert, string(certPEMBytes)
}

func tlsServerWith(t *testing.T, cert tls.Certificate) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Health{Status: "ok", CoreStarted: true, CoreVersion: "26.3.27"})
	})
	srv := httptest.NewUnstartedServer(mux)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	return srv
}

func TestPinnedCertAccepted(t *testing.T) {
	cert, certPEM := genSelfSigned(t)
	srv := tlsServerWith(t, cert)
	defer srv.Close()

	c := New(srv.URL, "k", certPEM) // pin the server's exact cert
	h, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("pinned https request failed: %v", err)
	}
	if !h.CoreStarted {
		t.Fatalf("unexpected health: %+v", h)
	}
}

func TestWrongPinRejected(t *testing.T) {
	serverCert, _ := genSelfSigned(t)
	_, otherPEM := genSelfSigned(t) // a different, unrelated cert
	srv := tlsServerWith(t, serverCert)
	defer srv.Close()

	c := New(srv.URL, "k", otherPEM)
	if _, err := c.Health(context.Background()); err == nil {
		t.Fatal("expected TLS pin mismatch to fail, but request succeeded")
	}
}

func TestNoPinAcceptsSelfSignedHTTPS(t *testing.T) {
	cert, _ := genSelfSigned(t)
	srv := tlsServerWith(t, cert)
	defer srv.Close()

	// Pinning is optional: without a pin the client accepts the self-signed cert
	// (verification skipped). This is the "pin removable" behaviour.
	c := New(srv.URL, "k", "")
	if _, err := c.Health(context.Background()); err != nil {
		t.Fatalf("unpinned self-signed https should be accepted, got: %v", err)
	}
}
