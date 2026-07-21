package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/authority"
)

func TestValidateExternalUpsertStartupConfigRequiresCompleteTrustedConfiguration(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer := &authority.Signer{AuthorityID: "equanauts-mac", PrivateKey: privateKey}

	tests := []struct {
		name       string
		principal  string
		namespaces string
		signer     *authority.Signer
		wantErr    string
	}{
		{name: "disabled", signer: nil},
		{name: "principal only", principal: "10bda411-0768-4843-826a-a12ec117b58e", signer: signer, wantErr: "both"},
		{name: "namespaces only", namespaces: "github-node", signer: signer, wantErr: "both"},
		{name: "missing signer", principal: "10bda411-0768-4843-826a-a12ec117b58e", namespaces: "github-node", wantErr: "signer"},
		{name: "complete", principal: "10bda411-0768-4843-826a-a12ec117b58e", namespaces: "github-node", signer: signer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				switch key {
				case "MULTICA_EXTERNAL_UPSERT_PRINCIPAL_ID":
					return tt.principal
				case "MULTICA_EXTERNAL_UPSERT_NAMESPACES":
					return tt.namespaces
				default:
					return ""
				}
			}
			err := validateExternalUpsertStartupConfig(getenv, tt.signer)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
