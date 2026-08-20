package tagaccess_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/tagaccess"
)

const vibesRepositoryEnv = "VIBES_REPO_PATH"

func TestVIBESTagProjectionCrossServiceContract(t *testing.T) {
	vibesRepository := strings.TrimSpace(os.Getenv(vibesRepositoryEnv))
	if vibesRepository == "" {
		t.Skipf("set %s to run the cross-service contract", vibesRepositoryEnv)
	}
	if info, err := os.Stat(filepath.Join(vibesRepository, "package.json")); err != nil || info.IsDir() {
		t.Fatalf("%s does not point to a VIBES checkout", vibesRepositoryEnv)
	}
	conn := openDisposableTagAccessDatabase(t)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	const keyID = "cross-service-ephemeral"
	newIngress := func() http.Handler {
		t.Helper()
		access, err := tagaccess.NewAuthenticatedAccess(
			tagaccess.NewPostgresStore(conn),
			fixedClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)},
			map[string][]byte{keyID: key},
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		ingress, err := tagaccess.NewHTTPIngress(access)
		if err != nil {
			t.Fatal(err)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/internal/tag-authority/workspace-projections", ingress.Workspace)
		mux.HandleFunc("/internal/tag-authority/identity-restrictions", ingress.Identity)
		mux.HandleFunc("/internal/tag-authority/session-workspace-supersessions", ingress.SessionWorkspace)
		return mux
	}
	var droppedWorkspaceResponse atomic.Bool
	var duplicateWorkspaceReceipt atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		newIngress().ServeHTTP(recorder, r)
		if r.URL.Path == "/internal/tag-authority/workspace-projections" &&
			droppedWorkspaceResponse.CompareAndSwap(false, true) {
			http.Error(w, "injected response loss after durable apply", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/internal/tag-authority/workspace-projections" &&
			bytes.Contains(recorder.Body.Bytes(), []byte(`"result":"duplicate"`)) {
			duplicateWorkspaceReceipt.Store(true)
		}
		for name, values := range recorder.Header() {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(recorder.Code)
		_, _ = io.Copy(w, recorder.Body)
	}))
	t.Cleanup(server.Close)

	certificatePath := filepath.Join(t.TempDir(), "multica-loopback-ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(certificatePath, certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	pnpm, err := exec.LookPath("pnpm")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(pnpm, "exec", "tsx", "tests/integration/tag-projection-cross-service.ts")
	command.Dir = vibesRepository
	command.Env = contractEnvironment(map[string]string{
		"NODE_EXTRA_CA_CERTS":          certificatePath,
		"TAG_CONTRACT_ORIGIN":          server.URL,
		"TAG_CONTRACT_HMAC_KEY_ID":     keyID,
		"TAG_CONTRACT_HMAC_KEY_BASE64": base64.StdEncoding.EncodeToString(key),
	})
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("VIBES cross-service driver failed: %v\n%s", err, output.String())
	}
	if !duplicateWorkspaceReceipt.Load() {
		t.Fatal("Multica restart did not return its durable duplicate receipt")
	}
	t.Log(strings.TrimSpace(output.String()))
}

func contractEnvironment(values map[string]string) []string {
	environment := make([]string, 0, len(values)+4)
	for _, name := range []string{"HOME", "PATH", "TMPDIR", "PNPM_HOME"} {
		if value := os.Getenv(name); value != "" {
			environment = append(environment, name+"="+value)
		}
	}
	for name, value := range values {
		environment = append(environment, name+"="+value)
	}
	return environment
}
