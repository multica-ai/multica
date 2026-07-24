package handler

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/internalbrowserqa"
)

type recordingBrowserSecretReader struct {
	keys   []string
	values map[string]string
}

func (reader *recordingBrowserSecretReader) RevealCredential(_ context.Context, _ pgtype.UUID, _ string, key string) (string, error) {
	reader.keys = append(reader.keys, key)
	return reader.values[key], nil
}

func TestLoadInternalBrowserVaultCredentialUsesRoleSpecificAccessKeysWithoutFormLogin(t *testing.T) {
	target, err := internalbrowserqa.TargetFor("data-catalog-reader")
	if err != nil {
		t.Fatalf("TargetFor(data-catalog-reader): %v", err)
	}
	reader := &recordingBrowserSecretReader{values: map[string]string{
		"READER_CF_ACCESS_CLIENT_ID":     "reader.access",
		"READER_CF_ACCESS_CLIENT_SECRET": "reader-secret",
	}}

	credential, err := loadInternalBrowserVaultCredential(context.Background(), reader, pgtype.UUID{}, target)
	if err != nil {
		t.Fatalf("loadInternalBrowserVaultCredential: %v", err)
	}
	if !reflect.DeepEqual(reader.keys, []string{"READER_CF_ACCESS_CLIENT_ID", "READER_CF_ACCESS_CLIENT_SECRET"}) {
		t.Fatalf("revealed keys = %v", reader.keys)
	}
	if credential.Username != "" || credential.Password != "" || credential.LoginCode != "" {
		t.Fatal("access-only target received a form credential")
	}
	if credential.AccessClientID != "reader.access" || credential.AccessClientSecret != "reader-secret" {
		t.Fatal("access-only target did not receive the role-specific service token")
	}
}

func TestLoadInternalBrowserVaultCredentialKeepsCerebroLoginAndAccessKeys(t *testing.T) {
	target, err := internalbrowserqa.TargetFor("cerebro")
	if err != nil {
		t.Fatalf("TargetFor(cerebro): %v", err)
	}
	reader := &recordingBrowserSecretReader{values: map[string]string{
		"USERNAME":                "browser@example.com",
		"LOGIN_CODE":              "staging-code",
		"CF_ACCESS_CLIENT_ID":     "cerebro.access",
		"CF_ACCESS_CLIENT_SECRET": "cerebro-secret",
	}}

	credential, err := loadInternalBrowserVaultCredential(context.Background(), reader, pgtype.UUID{}, target)
	if err != nil {
		t.Fatalf("loadInternalBrowserVaultCredential: %v", err)
	}
	wantKeys := []string{"USERNAME", "LOGIN_CODE", "CF_ACCESS_CLIENT_ID", "CF_ACCESS_CLIENT_SECRET"}
	if !reflect.DeepEqual(reader.keys, wantKeys) {
		t.Fatalf("revealed keys = %v, want %v", reader.keys, wantKeys)
	}
	if credential.Username != "browser@example.com" || credential.LoginCode != "staging-code" {
		t.Fatal("cerebro form credential was not loaded")
	}
	if credential.AccessClientID != "cerebro.access" || credential.AccessClientSecret != "cerebro-secret" {
		t.Fatal("cerebro access credential was not loaded")
	}
}
