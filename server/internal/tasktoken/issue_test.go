package tasktoken

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"sort"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testKeyPEM(t *testing.T) (string, *ecdsa.PublicKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), &key.PublicKey
}

func TestEmailLocal(t *testing.T) {
	cases := map[string]string{
		"alice@example.com": "alice",
		"Alice@Example.COM": "alice",
		"a+bug@example.com": "a",
		"A+B+C@example.com": "a",
		"noatsign":          "noatsign",
		"":                  "",
	}
	for in, want := range cases {
		if got := EmailLocal(in); got != want {
			t.Errorf("EmailLocal(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewIssuerDisabledWhenUnconfigured(t *testing.T) {
	iss, err := NewIssuer("", "", "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v, want nil", err)
	}
	if iss != nil {
		t.Fatal("NewIssuer() = non-nil, want nil when unconfigured")
	}
}

func TestNewIssuerRejectsHalfConfiguration(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[{"id":"a","label":"A","env":"TOKEN_A","claims":{"sub":"{{identity.id}}"}}]`

	if _, err := NewIssuer(catalog, "", ""); err == nil {
		t.Error("NewIssuer(catalog, no key) error = nil, want error")
	}
	if _, err := NewIssuer("", keyPEM, ""); err == nil {
		t.Error("NewIssuer(no catalog, key) error = nil, want error")
	}
}

func TestIssueSignsVerifiableToken(t *testing.T) {
	keyPEM, pub := testKeyPEM(t)
	catalog := `[{
		"id": "erp", "label": "ERP", "env": "BOT_TOKEN_ERP",
		"key_id": "bot-2024", "ttl": "2h",
		"claims": {"scope":"erp","sub":"{{identity.email_local}}","name":"{{identity.name}}","src":"{{identity.source}}"}
	}]`
	iss, err := NewIssuer(catalog, keyPEM, "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}

	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	out := iss.Issue([]string{"erp"}, Context{
		Identity:    Identity{Email: "Alice@Example.com", Name: "Alice", UserID: "u-1", Source: "direct_human"},
		WorkspaceID: "ws-1", WorkspaceSlug: "erp",
	}, now)

	raw, ok := out["BOT_TOKEN_ERP"]
	if !ok {
		t.Fatalf("Issue() = %v, want key BOT_TOKEN_ERP", out)
	}

	// Validate against the same instant the token was signed at: this keeps
	// exp/iat validation real (a token invalid at its own issuing moment
	// still fails) without tying the test to wall-clock time.
	parsed, err := jwt.Parse(raw,
		func(*jwt.Token) (any, error) { return pub, nil },
		jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("token failed verification: %v", err)
	}
	if kid := parsed.Header["kid"]; kid != "bot-2024" {
		t.Errorf("kid = %v, want bot-2024", kid)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["sub"] != "alice" {
		t.Errorf("sub = %v, want alice", claims["sub"])
	}
	if claims["scope"] != "erp" {
		t.Errorf("scope = %v, want erp", claims["scope"])
	}
	if claims["src"] != "direct_human" {
		t.Errorf("src = %v, want direct_human", claims["src"])
	}
	if got := int64(claims["iat"].(float64)); got != now.Unix() {
		t.Errorf("iat = %d, want %d", got, now.Unix())
	}
	if got := int64(claims["exp"].(float64)); got != now.Add(2*time.Hour).Unix() {
		t.Errorf("exp = %d, want %d", got, now.Add(2*time.Hour).Unix())
	}
	if claims["jti"] == "" || claims["jti"] == nil {
		t.Error("jti is empty, want a unique id")
	}
}

func TestIssueSkipsUnknownAndDisabledTemplates(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[{"id":"a","label":"A","env":"TOKEN_A","claims":{"sub":"{{identity.id}}"}}]`
	iss, err := NewIssuer(catalog, keyPEM, "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}

	now := time.Now()
	tctx := Context{Identity: Identity{UserID: "u-1", Email: "a@b.c"}}

	// An id no longer in the catalog must not fail the whole batch.
	out := iss.Issue([]string{"a", "gone"}, tctx, now)
	if len(out) != 1 {
		t.Fatalf("Issue() = %v, want only TOKEN_A", out)
	}
	if _, ok := out["TOKEN_A"]; !ok {
		t.Errorf("Issue() = %v, want key TOKEN_A", out)
	}

	if got := iss.Issue(nil, tctx, now); len(got) != 0 {
		t.Errorf("Issue(nil) = %v, want empty", got)
	}
}

func TestIssueUniqueJTI(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[{"id":"a","label":"A","env":"TOKEN_A","claims":{"sub":"{{identity.id}}"}}]`
	iss, err := NewIssuer(catalog, keyPEM, "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}
	now := time.Now()
	tctx := Context{Identity: Identity{UserID: "u-1"}}

	first := iss.Issue([]string{"a"}, tctx, now)["TOKEN_A"]
	second := iss.Issue([]string{"a"}, tctx, now)["TOKEN_A"]
	if first == second {
		t.Error("two issues at the same instant produced identical tokens, want unique jti")
	}
}

func TestNilIssuerIsSafe(t *testing.T) {
	var iss *Issuer
	if got := iss.Issue([]string{"a"}, Context{}, time.Now()); len(got) != 0 {
		t.Errorf("nil issuer Issue() = %v, want empty", got)
	}
	if iss.Catalog() != nil {
		t.Error("nil issuer Catalog() != nil")
	}
}

func TestIssueEmitsManifestForEnabledTemplates(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[
	  {"id":"erp","label":"ERP","env":"BOT_TOKEN_ERP","claims":{"sub":"{{identity.email_local}}"},
	   "manifest":{"key":"erp","name":"ERP","base_url":"https://erp.example.com","env_var":"BOT_TOKEN_ERP"}},
	  {"id":"app","label":"APP","env":"BOT_TOKEN_APP","claims":{"sub":"{{identity.email_local}}"},
	   "manifest":{"key":"app","name":"APP","base_url":"https://app.example.com","env_var":"BOT_TOKEN_APP"}}
	]`
	iss, err := NewIssuer(catalog, keyPEM, "BOT_SYSTEMS_CONFIG")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}

	out := iss.Issue([]string{"erp"}, Context{Identity: Identity{Email: "a@b.c"}}, time.Now())

	raw, ok := out["BOT_SYSTEMS_CONFIG"]
	if !ok {
		t.Fatalf("Issue() = %v, want a BOT_SYSTEMS_CONFIG entry", keysOf(out))
	}
	var systems []map[string]any
	if err := json.Unmarshal([]byte(raw), &systems); err != nil {
		t.Fatalf("manifest is not a JSON array: %v (%s)", err, raw)
	}
	// Only the enabled template may appear: the manifest must describe exactly
	// what the agent actually holds a token for.
	if len(systems) != 1 {
		t.Fatalf("manifest = %s, want only the enabled template", raw)
	}
	if systems[0]["key"] != "erp" {
		t.Errorf("manifest[0].key = %v, want erp", systems[0]["key"])
	}
}

func TestIssueOmitsManifestWhenUnconfigured(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[{"id":"a","label":"A","env":"TOKEN_A","claims":{"sub":"x"},"manifest":{"key":"a"}}]`
	// No manifest env configured -> nothing extra is injected.
	iss, err := NewIssuer(catalog, keyPEM, "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}
	out := iss.Issue([]string{"a"}, Context{}, time.Now())
	if len(out) != 1 {
		t.Errorf("Issue() = %v, want only the token", keysOf(out))
	}
}

func TestIssueSkipsManifestEntryForTemplateWithoutOne(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[{"id":"a","label":"A","env":"TOKEN_A","claims":{"sub":"x"}}]`
	iss, err := NewIssuer(catalog, keyPEM, "BOT_SYSTEMS_CONFIG")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}
	out := iss.Issue([]string{"a"}, Context{}, time.Now())
	if _, present := out["BOT_SYSTEMS_CONFIG"]; present {
		t.Errorf("Issue() = %v, want no manifest when no template declares one", keysOf(out))
	}
}

func TestNewIssuerRejectsBadManifestEnvName(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[{"id":"a","label":"A","env":"TOKEN_A","claims":{"sub":"x"}}]`
	for _, bad := range []string{"lower_case", "MULTICA_X", "PATH", "1BAD"} {
		if _, err := NewIssuer(catalog, keyPEM, bad); err == nil {
			t.Errorf("NewIssuer(manifestEnv=%q) error = nil, want rejection", bad)
		}
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
