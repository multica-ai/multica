package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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

func TestIssueUpsertExternalVerifiesReceiptBoundToExactWrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var writes int
	var dbID authority.DBIdentity
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/issues/upsert-external":
			writes++
			if got := r.Header.Get("Authorization"); got != "Bearer mul_token" {
				t.Fatalf("write Authorization = %q", got)
			}
			if got := r.Header.Get("X-Workspace-ID"); got != "ws-123" {
				t.Fatalf("write workspace = %q", got)
			}
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read request: %v", err)
			}
			var request struct {
				Nonce string `json:"nonce"`
			}
			if err := json.Unmarshal(raw, &request); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if _, err := authority.ValidateNonce(request.Nonce); err != nil {
				t.Fatalf("request nonce: %v", err)
			}
			issue := map[string]any{
				"id":           "11111111-1111-1111-1111-111111111111",
				"workspace_id": "ws-123",
				"title":        "Imported",
				"status":       "todo",
				"priority":     "none",
				"identifier":   "EXT-1",
				"metadata":     map[string]any{},
			}
			digest := sha256.Sum256(raw)
			receipt, err := authority.SignWriteReceipt(priv, authority.WriteReceiptStatement{
				Protocol: authority.WriteReceiptProtocolVersion, Operation: "issue.upsert-external",
				RequestSHA256: fmt.Sprintf("%x", digest), ResourceID: issue["id"].(string), WorkspaceID: "ws-123", Nonce: request.Nonce,
				AuthorityID: "local-dev-authority", DBIdentity: dbID, IssuedAt: time.Now().UTC(), ServerCommit: "test-commit",
			})
			if err != nil {
				t.Fatalf("sign receipt: %v", err)
			}
			data, _ := json.Marshal(map[string]any{"issue": issue, "receipt": receipt})
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
	if writes != 1 {
		t.Fatalf("writes=%d, want 1", writes)
	}
}

func TestIssueUpsertExternalRejectsUnboundOrMalformedReceipt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	dbID := seedAuthorityPinnedConfig(t, "http://multica.test", pub)
	for _, tc := range []struct {
		name   string
		mutate func(*authority.WriteReceipt, *map[string]any)
		suffix string
	}{
		{name: "nonce", mutate: func(r *authority.WriteReceipt, _ *map[string]any) { r.Nonce = strings.Repeat("A", 43) }},
		{name: "digest", mutate: func(r *authority.WriteReceipt, _ *map[string]any) { r.RequestSHA256 = strings.Repeat("0", 64) }},
		{name: "resource", mutate: func(r *authority.WriteReceipt, _ *map[string]any) {
			r.ResourceID = "22222222-2222-2222-2222-222222222222"
		}},
		{name: "receipt workspace", mutate: func(r *authority.WriteReceipt, _ *map[string]any) { r.WorkspaceID = "ws-999" }},
		{name: "issue workspace", mutate: func(_ *authority.WriteReceipt, env *map[string]any) {
			(*env)["issue"].(map[string]any)["workspace_id"] = "ws-999"
		}},
		{name: "unknown field", mutate: func(_ *authority.WriteReceipt, env *map[string]any) { (*env)["unexpected"] = true }},
		{name: "unknown issue field", mutate: func(_ *authority.WriteReceipt, env *map[string]any) {
			(*env)["issue"].(map[string]any)["unexpected"] = true
		}},
		{name: "trailing json", mutate: func(_ *authority.WriteReceipt, _ *map[string]any) {}, suffix: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldTransport := http.DefaultTransport
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/issues/upsert-external" {
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
				raw, _ := io.ReadAll(r.Body)
				var request struct {
					Nonce string `json:"nonce"`
				}
				_ = json.Unmarshal(raw, &request)
				issue := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "workspace_id": "ws-123", "title": "Imported", "status": "todo", "priority": "none", "identifier": "EXT-1", "metadata": map[string]any{}}
				digest := sha256.Sum256(raw)
				receipt, signErr := authority.SignWriteReceipt(priv, authority.WriteReceiptStatement{Protocol: authority.WriteReceiptProtocolVersion, Operation: "issue.upsert-external", RequestSHA256: fmt.Sprintf("%x", digest), ResourceID: issue["id"].(string), WorkspaceID: "ws-123", Nonce: request.Nonce, AuthorityID: "local-dev-authority", DBIdentity: dbID, IssuedAt: time.Now().UTC(), ServerCommit: "test-commit"})
				if signErr != nil {
					t.Fatal(signErr)
				}
				env := map[string]any{"issue": issue, "receipt": receipt}
				tc.mutate(&receipt, &env)
				env["receipt"] = receipt
				data, _ := json.Marshal(env)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data) + tc.suffix))}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = oldTransport })
			cmd := newIssueUpsertExternalTestCmd()
			_ = cmd.Flags().Set("alias", "github=123")
			_ = cmd.Flags().Set("title", "Imported")
			if _, runErr := captureStdout(t, func() error { return runIssueUpsertExternal(cmd, nil) }); runErr == nil {
				t.Fatal("accepted invalid receipt response")
			}
		})
	}
}
