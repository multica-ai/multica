package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/authority"
	"github.com/multica-ai/multica/server/internal/cli"
)

func TestRunAuthorityPinStoresExplicitOperatorInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := testCmd()
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("authority-id", "", "")
	cmd.Flags().String("public-key", "", "")
	cmd.Flags().String("db-system-identifier", "", "")
	cmd.Flags().Int64("db-oid", 0, "")
	cmd.Flags().String("db-name", "", "")
	_ = cmd.Flags().Set("server-url", "https://API.Multica.Test/")
	_ = cmd.Flags().Set("authority-id", "local-dev-authority")
	_ = cmd.Flags().Set("public-key", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	_ = cmd.Flags().Set("db-system-identifier", "7420934553282556881")
	_ = cmd.Flags().Set("db-oid", "16384")
	_ = cmd.Flags().Set("db-name", "multica_test")

	if err := runAuthorityPin(cmd, nil); err != nil {
		t.Fatalf("runAuthorityPin: %v", err)
	}

	cfg, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.AuthorityPin == nil {
		t.Fatal("AuthorityPin is nil")
	}
	if cfg.AuthorityPin.ServerURL != "https://api.multica.test" {
		t.Fatalf("server_url = %q", cfg.AuthorityPin.ServerURL)
	}
	if cfg.AuthorityPin.AuthorityID != "local-dev-authority" || cfg.AuthorityPin.DBIdentity.DatabaseName != "multica_test" {
		t.Fatalf("pin = %#v", cfg.AuthorityPin)
	}
}

func TestRunAuthorityPinRejectsMissingExplicitInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := testCmd()
	cmd.Flags().String("server-url", "https://api.multica.test", "")
	cmd.Flags().String("authority-id", "local-dev-authority", "")
	cmd.Flags().String("public-key", "", "")
	cmd.Flags().String("db-system-identifier", "7420934553282556881", "")
	cmd.Flags().Int64("db-oid", 16384, "")
	cmd.Flags().String("db-name", "multica_test", "")

	err := runAuthorityPin(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "public-key") {
		t.Fatalf("runAuthorityPin error = %v, want missing public-key", err)
	}
}

func TestVerifyAuthorityRejectsTrailingJSONResponse(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	dbID := authority.DBIdentity{SystemIdentifier: "7420934553282556881", DatabaseOID: 16384, DatabaseName: "multica_test"}
	issuedAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Nonce string `json:"nonce"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		att, err := authority.Sign(priv, authority.Statement{
			Protocol:     authority.ProtocolVersion,
			Nonce:        req.Nonce,
			AuthorityID:  "local-dev-authority",
			DBIdentity:   dbID,
			IssuedAt:     issuedAt,
			ServerCommit: "test-commit",
		})
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if err := json.NewEncoder(w).Encode(att); err != nil {
			t.Fatalf("encode attestation: %v", err)
		}
		_, _ = w.Write([]byte(`{"unexpected":true}`))
	}))
	defer server.Close()

	client := cli.NewAPIClient(server.URL, "", "")
	_, err = verifyAuthorityWithClient(context.Background(), client, authority.Pin{
		ServerURL:   server.URL,
		PublicKey:   authority.EncodePublicKey(pub),
		AuthorityID: "local-dev-authority",
		DBIdentity:  dbID,
	}, func() time.Time { return issuedAt.Add(time.Second) })
	if err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("verifyAuthorityWithClient error = %v, want trailing JSON rejection", err)
	}
}

var _ = authority.ProtocolVersion
