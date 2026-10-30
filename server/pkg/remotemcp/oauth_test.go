package remotemcp

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"testing"
)

func TestProtectedResourceMetadataURLPreservesResourcePath(t *testing.T) {
	resource, _ := url.Parse("https://api.example.com/mcp")
	if got, want := protectedResourceMetadataURL(resource), "https://api.example.com/.well-known/oauth-protected-resource/mcp"; got != want {
		t.Fatalf("protectedResourceMetadataURL = %q, want %q", got, want)
	}
	root, _ := url.Parse("https://api.example.com/")
	if got, want := protectedResourceMetadataURL(root), "https://api.example.com/.well-known/oauth-protected-resource"; got != want {
		t.Fatalf("root protectedResourceMetadataURL = %q, want %q", got, want)
	}
}

func TestAuthorizationMetadataURLsPreserveIssuerPath(t *testing.T) {
	issuer, _ := url.Parse("https://login.example.com/tenant")
	got := authorizationMetadataURLs(issuer)
	if len(got) != 2 || got[0] != "https://login.example.com/.well-known/oauth-authorization-server/tenant" || got[1] != "https://login.example.com/.well-known/openid-configuration/tenant" {
		t.Fatalf("authorizationMetadataURLs = %#v", got)
	}
}

func TestBuildAuthorizationURLPinsPKCEStateAndResource(t *testing.T) {
	metadata := OAuthMetadata{
		ResourceEndpoint:      "https://api.example.com/mcp",
		AuthorizationEndpoint: "https://login.example.com/authorize?audience=existing",
	}
	registration := OAuthClientRegistration{ClientID: "client-1"}
	got, err := BuildAuthorizationURL(metadata, registration, "https://multica.example/api/callback", "state-1", "verifier-1", "search read")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(got)
	query := parsed.Query()
	wantChallenge := sha256.Sum256([]byte("verifier-1"))
	checks := map[string]string{
		"audience":              "existing",
		"response_type":         "code",
		"client_id":             "client-1",
		"redirect_uri":          "https://multica.example/api/callback",
		"state":                 "state-1",
		"code_challenge":        base64.RawURLEncoding.EncodeToString(wantChallenge[:]),
		"code_challenge_method": "S256",
		"resource":              "https://api.example.com/mcp",
		"scope":                 "search read",
	}
	for key, want := range checks {
		if value := query.Get(key); value != want {
			t.Errorf("query %s = %q, want %q", key, value, want)
		}
	}
}

func TestValidateOAuthMetadataRejectsUnsafeAdvancedOverride(t *testing.T) {
	err := ValidateOAuthMetadata(t.Context(), OAuthMetadata{
		AuthorizationEndpoint: "http://127.0.0.1/authorize",
		TokenEndpoint:         "https://8.8.8.8/token",
	})
	if err == nil {
		t.Fatal("unsafe authorization endpoint was accepted")
	}
}
