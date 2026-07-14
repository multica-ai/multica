package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/authority"
	"github.com/multica-ai/multica/server/internal/cli"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newIssueUpsertExternalTestCmd() *cobra.Command {
	cmd := testCmd()
	registerIssueUpsertExternalFlags(cmd)
	return cmd
}

func seedAuthorityPinnedConfig(t *testing.T, serverURL string, pub ed25519.PublicKey) authority.DBIdentity {
	t.Helper()
	dbID := authority.DBIdentity{SystemIdentifier: "7420934553282556881", DatabaseOID: 16384, DatabaseName: "multica_test"}
	if err := cli.SaveCLIConfig(cli.CLIConfig{
		ServerURL:   serverURL,
		WorkspaceID: "ws-123",
		Token:       "mul_token",
		AuthorityPin: &authority.Pin{
			ServerURL:   serverURL,
			PublicKey:   authority.EncodePublicKey(pub),
			AuthorityID: "local-dev-authority",
			DBIdentity:  dbID,
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return dbID
}

func TestIssueUpsertExternalVerifiesAuthorityBeforeWriteAndOmitsAuthOnAttest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var writes int
	var attests int
	var dbID authority.DBIdentity
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/authority/attest":
			attests++
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("attestation Authorization = %q, want empty", got)
			}
			if got := r.Header.Get("X-Workspace-ID"); got != "" {
				t.Fatalf("attestation X-Workspace-ID = %q, want empty", got)
			}
			var req struct {
				Nonce string `json:"nonce"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode attest req: %v", err)
			}
			att, err := authority.Sign(priv, authority.Statement{
				Protocol:     authority.ProtocolVersion,
				Nonce:        req.Nonce,
				AuthorityID:  "local-dev-authority",
				DBIdentity:   dbID,
				IssuedAt:     time.Now().UTC(),
				ServerCommit: "test-commit",
			})
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			data, _ := json.Marshal(att)
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}, nil
		case "/api/issues/upsert-external":
			writes++
			if got := r.Header.Get("Authorization"); got != "Bearer mul_token" {
				t.Fatalf("write Authorization = %q", got)
			}
			if got := r.Header.Get("X-Workspace-ID"); got != "ws-123" {
				t.Fatalf("write workspace = %q", got)
			}
			data, _ := json.Marshal(map[string]any{
				"id":         "11111111-1111-1111-1111-111111111111",
				"title":      "Imported",
				"status":     "todo",
				"priority":   "none",
				"identifier": "EXT-1",
				"metadata":   map[string]any{},
			})
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}, nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return nil, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	dbID = seedAuthorityPinnedConfig(t, "http://multica.test", pub)

	cmd := newIssueUpsertExternalTestCmd()
	_ = cmd.Flags().Set("alias", "github=123")
	_ = cmd.Flags().Set("title", "Imported")
	_, err = captureStdout(t, func() error { return runIssueUpsertExternal(cmd, nil) })
	if err != nil {
		t.Fatalf("runIssueUpsertExternal: %v", err)
	}
	if attests != 1 || writes != 1 {
		t.Fatalf("attests=%d writes=%d, want 1/1", attests, writes)
	}
}

func TestIssueUpsertExternalDoesNotWriteAfterAuthorityFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var writes int
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/issues/upsert-external" {
			writes++
		}
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("attest failed"))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	seedAuthorityPinnedConfig(t, "http://multica.test", pub)

	cmd := newIssueUpsertExternalTestCmd()
	_ = cmd.Flags().Set("alias", "github=123")
	_ = cmd.Flags().Set("title", "Imported")
	_, err = captureStdout(t, func() error { return runIssueUpsertExternal(cmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "authority") {
		t.Fatalf("runIssueUpsertExternal err = %v, want authority failure", err)
	}
	if writes != 0 {
		t.Fatalf("writes = %d, want zero after authority failure", writes)
	}
}
