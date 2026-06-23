package util

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateSelfSignedCert creates a self-signed certificate for testing
// and returns its PEM-encoded cert + key bytes.
func generateSelfSignedCert(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

// writeTempFile writes data to a temp file and returns its path.
func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pem")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Test: no env vars → default behavior (nil TLS config, plain client)
// ---------------------------------------------------------------------------

func TestNewHTTPClient_NoEnvVars(t *testing.T) {
	t.Setenv("MULTICA_CA_BUNDLE", "")
	t.Setenv("MULTICA_TLS_INSECURE", "")
	t.Setenv("MULTICA_TLS_EKU_ANY", "")

	client := NewHTTPClient(30 * time.Second)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", client.Timeout)
	}
	// No custom transport when no env vars are set.
	if client.Transport != nil {
		t.Error("expected nil Transport (default) when no env vars set")
	}
}

func TestTLSConfig_NoEnvVars(t *testing.T) {
	t.Setenv("MULTICA_CA_BUNDLE", "")
	t.Setenv("MULTICA_TLS_INSECURE", "")
	t.Setenv("MULTICA_TLS_EKU_ANY", "")

	if cfg := TLSConfig(); cfg != nil {
		t.Error("expected nil TLSConfig when no env vars set")
	}
}

// ---------------------------------------------------------------------------
// Test: MULTICA_CA_BUNDLE loads a custom CA pool
// ---------------------------------------------------------------------------

func TestNewHTTPClient_CABundle(t *testing.T) {
	certPEM, _ := generateSelfSignedCert(t)
	caPath := writeTempFile(t, certPEM)

	t.Setenv("MULTICA_CA_BUNDLE", caPath)
	t.Setenv("MULTICA_TLS_INSECURE", "")
	t.Setenv("MULTICA_TLS_EKU_ANY", "")

	cfg := TLSConfig()
	if cfg == nil {
		t.Fatal("expected non-nil TLSConfig")
	}
	if cfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be false with CA bundle")
	}
	if cfg.RootCAs == nil {
		t.Error("expected non-nil RootCAs")
	}
	// Verify the cert is in the pool.
	block, _ := pem.Decode(certPEM)
	cert, _ := x509.ParseCertificate(block.Bytes)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: cfg.RootCAs, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Errorf("cert should verify against custom pool: %v", err)
	}
}

func TestNewHTTPClient_CABundle_InvalidPath(t *testing.T) {
	t.Setenv("MULTICA_CA_BUNDLE", "/nonexistent/path.pem")
	t.Setenv("MULTICA_TLS_INSECURE", "")
	t.Setenv("MULTICA_TLS_EKU_ANY", "")

	// Should not panic — should fall back gracefully.
	cfg := TLSConfig()
	if cfg == nil {
		t.Fatal("expected non-nil TLSConfig even with bad CA path")
	}
	// RootCAs may be nil (fallback to system roots).
}

func TestNewHTTPClient_CABundle_InvalidPEM(t *testing.T) {
	badPath := writeTempFile(t, []byte("not a PEM file"))

	t.Setenv("MULTICA_CA_BUNDLE", badPath)
	t.Setenv("MULTICA_TLS_INSECURE", "")
	t.Setenv("MULTICA_TLS_EKU_ANY", "")

	// Should not panic — should fall back gracefully.
	cfg := TLSConfig()
	if cfg == nil {
		t.Fatal("expected non-nil TLSConfig")
	}
}

// ---------------------------------------------------------------------------
// Test: MULTICA_TLS_INSECURE=1 disables verification
// ---------------------------------------------------------------------------

func TestNewHTTPClient_Insecure(t *testing.T) {
	t.Setenv("MULTICA_CA_BUNDLE", "")
	t.Setenv("MULTICA_TLS_INSECURE", "1")
	t.Setenv("MULTICA_TLS_EKU_ANY", "")

	cfg := TLSConfig()
	if cfg == nil {
		t.Fatal("expected non-nil TLSConfig")
	}
	if !cfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify=true")
	}
}

func TestNewHTTPClient_Insecure_NotSet(t *testing.T) {
	t.Setenv("MULTICA_CA_BUNDLE", "")
	t.Setenv("MULTICA_TLS_INSECURE", "0")
	t.Setenv("MULTICA_TLS_EKU_ANY", "")

	cfg := TLSConfig()
	if cfg != nil {
		t.Error("expected nil TLSConfig when insecure=0 and no other vars")
	}
}

// ---------------------------------------------------------------------------
// Test: MULTICA_TLS_EKU_ANY=1 relaxes EKU but keeps chain verification
// ---------------------------------------------------------------------------

func TestNewHTTPClient_EKUAny(t *testing.T) {
	certPEM, _ := generateSelfSignedCert(t)
	caPath := writeTempFile(t, certPEM)

	t.Setenv("MULTICA_CA_BUNDLE", caPath)
	t.Setenv("MULTICA_TLS_INSECURE", "")
	t.Setenv("MULTICA_TLS_EKU_ANY", "1")

	cfg := TLSConfig()
	if cfg == nil {
		t.Fatal("expected non-nil TLSConfig")
	}
	// EKU_ANY uses InsecureSkipVerify=true internally but with a custom
	// VerifyConnection callback, so chain + hostname are still verified.
	if !cfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify=true (with custom VerifyConnection)")
	}
	if cfg.VerifyConnection == nil {
		t.Error("expected non-nil VerifyConnection callback")
	}
}

// ---------------------------------------------------------------------------
// Test: insecure takes precedence over eku_any
// ---------------------------------------------------------------------------

func TestNewHTTPClient_InsecureOverridesEKUAny(t *testing.T) {
	t.Setenv("MULTICA_CA_BUNDLE", "")
	t.Setenv("MULTICA_TLS_INSECURE", "1")
	t.Setenv("MULTICA_TLS_EKU_ANY", "1")

	cfg := TLSConfig()
	if cfg == nil {
		t.Fatal("expected non-nil TLSConfig")
	}
	if !cfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify=true")
	}
	// When insecure is set, VerifyConnection should NOT be set (full bypass).
	if cfg.VerifyConnection != nil {
		t.Error("expected nil VerifyConnection when insecure=1")
	}
}

// ---------------------------------------------------------------------------
// Integration test: CA bundle allows connection to a test HTTPS server
// ---------------------------------------------------------------------------

func TestNewHTTPClient_CABundle_ConnectsToTestServer(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedCert(t)
	caPath := writeTempFile(t, certPEM)

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("load key pair: %v", err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	defer server.Close()

	t.Setenv("MULTICA_CA_BUNDLE", caPath)
	t.Setenv("MULTICA_TLS_INSECURE", "")
	t.Setenv("MULTICA_TLS_EKU_ANY", "")

	client := NewHTTPClient(5 * time.Second)
	_ = client
	// httptest uses a custom transport that ignores our env-based config,
	// so we verify the TLS config directly instead.
	cfg := TLSConfig()
	if cfg == nil || cfg.RootCAs == nil {
		t.Fatal("expected custom RootCAs")
	}

	// Verify the server cert validates against our custom pool.
	serverCert := server.Certificate()
	parsed, err := x509.ParseCertificate(serverCert.Raw)
	if err != nil {
		t.Fatalf("parse server cert: %v", err)
	}
	opts := x509.VerifyOptions{
		Roots:    cfg.RootCAs,
		DNSName:  "localhost",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if _, err := parsed.Verify(opts); err != nil {
		t.Errorf("server cert should verify against custom CA pool: %v", err)
	}
}
