package gitca

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestCA(t *testing.T, name string, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func commonNames(certs []*x509.Certificate) []string {
	names := make([]string, 0, len(certs))
	for _, c := range certs {
		names = append(names, c.Subject.CommonName)
	}
	return names
}

func TestCollectSkipsSystemDirectories(t *testing.T) {
	system := t.TempDir()
	extra := t.TempDir()
	writeFile(t, filepath.Join(system, "public.pem"), newTestCA(t, "public", time.Now().Add(time.Hour)))
	writeFile(t, filepath.Join(extra, "internal.crt"), newTestCA(t, "internal", time.Now().Add(time.Hour)))

	certs, warnings := collect(system+":"+extra, []string{system + "/"})
	if got := commonNames(certs); len(got) != 1 || got[0] != "internal" {
		t.Fatalf("certs = %v, want [internal]", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestCollectEmptyEnvReturnsNothing(t *testing.T) {
	if certs, _ := collect("", nil); certs != nil {
		t.Fatalf("certs = %v, want nil", certs)
	}
}

func TestCollectMirrorsAppendCertsFromPEM(t *testing.T) {
	dir := t.TempDir()
	ca := newTestCA(t, "internal", time.Now().Add(time.Hour))
	second := newTestCA(t, "second", time.Now().Add(time.Hour))

	keyBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not a cert")})
	withHeaders := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Headers: map[string]string{"Proc-Type": "4,ENCRYPTED"}, Bytes: []byte("x")})
	broken := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage")})

	writeFile(t, filepath.Join(dir, "bundle.pem"), bytes.Join([][]byte{keyBlock, ca, withHeaders, broken, second}, nil))
	writeFile(t, filepath.Join(dir, "notes.txt"), []byte("no PEM here"))
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	certs, warnings := collect(dir, nil)
	if got := commonNames(certs); len(got) != 2 || got[0] != "internal" || got[1] != "second" {
		t.Fatalf("certs = %v, want [internal second]", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "bundle.pem") {
		t.Fatalf("warnings = %v, want one parse warning for bundle.pem", warnings)
	}
}

func TestCollectDropsSameDirSymlinksAndDuplicates(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	ca := newTestCA(t, "internal", time.Now().Add(time.Hour))
	writeFile(t, filepath.Join(dir, "internal.crt"), ca)
	// c_rehash style alias: same-dir symlink, skipped by crypto/x509.
	if err := os.Symlink("internal.crt", filepath.Join(dir, "abcd1234.0")); err != nil {
		t.Fatal(err)
	}
	// Same certificate in a second directory is shared once.
	writeFile(t, filepath.Join(other, "copy.crt"), ca)

	certs, _ := collect(dir+":"+other, nil)
	if len(certs) != 1 {
		t.Fatalf("got %d certificates, want 1", len(certs))
	}
}

func TestCollectMissingDirectoryIsSilent(t *testing.T) {
	certs, warnings := collect(filepath.Join(t.TempDir(), "missing"), nil)
	if len(certs) != 0 || len(warnings) != 0 {
		t.Fatalf("certs=%d warnings=%v, want none", len(certs), warnings)
	}
}

// crypto/x509 loads the system pool once per process, so the end-to-end check
// runs Load in a child process whose SSL_CERT_DIR is set before that happens.
const loadHelperEnv = "GITCA_TEST_LOAD_HELPER"

func TestLoadHelperProcess(t *testing.T) {
	if os.Getenv(loadHelperEnv) != "1" {
		t.Skip("helper process")
	}
	os.Stdout.Write(Load(slog.New(slog.NewTextHandler(io.Discard, nil))))
}

func runLoad(t *testing.T, certDir string) []byte {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLoadHelperProcess$")
	cmd.Env = append(os.Environ(), loadHelperEnv+"=1", "SSL_CERT_DIR="+certDir)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("helper failed: %v", err)
	}
	// The test framework prints PASS after the bundle.
	if i := bytes.LastIndex(out, []byte("-----END CERTIFICATE-----\n")); i >= 0 {
		return out[:i+len("-----END CERTIFICATE-----\n")]
	}
	return nil
}

func TestLoadSharesOnlyWhatGoTrusts(t *testing.T) {
	dir := t.TempDir()
	valid := newTestCA(t, "internal", time.Now().Add(time.Hour))
	expired := newTestCA(t, "expired", time.Now().Add(-time.Minute))
	writeFile(t, filepath.Join(dir, "internal.crt"), valid)
	writeFile(t, filepath.Join(dir, "expired.crt"), expired)

	got := runLoad(t, "/etc/ssl/certs:"+dir)
	if !bytes.Equal(got, valid) {
		t.Fatalf("Load() = %q, want only the valid internal CA", got)
	}
}

func TestLoadWithoutExtraDirectoriesSharesNothing(t *testing.T) {
	if got := runLoad(t, "/etc/ssl/certs"); len(got) != 0 {
		t.Fatalf("Load() = %q, want nothing", got)
	}
}
