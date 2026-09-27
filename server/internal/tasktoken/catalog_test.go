package tasktoken

import (
	"strings"
	"testing"
	"time"
)

func TestParseCatalogEmptyIsDisabled(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n"} {
		c, err := ParseCatalog(raw)
		if err != nil {
			t.Fatalf("ParseCatalog(%q) error = %v, want nil", raw, err)
		}
		if c != nil {
			t.Fatalf("ParseCatalog(%q) = %v, want nil catalog (feature disabled)", raw, c)
		}
	}
}

func TestParseCatalogValid(t *testing.T) {
	raw := `[{
		"id": "erp",
		"label": "ERP",
		"description": "erp.example.com",
		"env": "BOT_TOKEN_ERP",
		"algorithm": "ES256",
		"key_id": "bot-2024",
		"ttl": "8h",
		"claims": {"scope": "erp", "sub": "{{identity.email_local}}"}
	}]`

	c, err := ParseCatalog(raw)
	if err != nil {
		t.Fatalf("ParseCatalog() error = %v", err)
	}
	tpl, ok := c.Get("erp")
	if !ok {
		t.Fatal("Get(\"erp\") not found")
	}
	if tpl.Env != "BOT_TOKEN_ERP" {
		t.Errorf("Env = %q, want BOT_TOKEN_ERP", tpl.Env)
	}
	if tpl.TTL != 8*time.Hour {
		t.Errorf("TTL = %v, want 8h", tpl.TTL)
	}
	if tpl.KeyID != "bot-2024" {
		t.Errorf("KeyID = %q, want bot-2024", tpl.KeyID)
	}
	if got := len(c.List()); got != 1 {
		t.Errorf("len(List()) = %d, want 1", got)
	}
}

func TestParseCatalogAgentAndTaskVariables(t *testing.T) {
	raw := `[{"id":"a","label":"A","env":"TOKEN_A","claims":{
		"sub":"{{identity.email}}",
		"act_sub":"{{agent.id}}",
		"act_name":"{{agent.name}}",
		"task_id":"{{task.id}}"
	}}]`
	if _, err := ParseCatalog(raw); err != nil {
		t.Fatalf("ParseCatalog() error = %v, want agent.*/task.* accepted", err)
	}
}

func TestParseCatalogAllowedDomains(t *testing.T) {
	raw := `[{"id":"a","label":"A","env":"TOKEN_A",
		"allowed_domains":[" Corp.COM ","other.example"],
		"claims":{"sub":"{{identity.email}}"}}]`
	c, err := ParseCatalog(raw)
	if err != nil {
		t.Fatalf("ParseCatalog() error = %v", err)
	}
	tpl, _ := c.Get("a")
	if len(tpl.AllowedDomains) != 2 || tpl.AllowedDomains[0] != "corp.com" || tpl.AllowedDomains[1] != "other.example" {
		t.Errorf("AllowedDomains = %v, want normalized [corp.com other.example]", tpl.AllowedDomains)
	}
}

func TestParseCatalogDefaults(t *testing.T) {
	raw := `[{"id":"a","label":"A","env":"TOKEN_A","claims":{"sub":"{{identity.id}}"}}]`
	c, err := ParseCatalog(raw)
	if err != nil {
		t.Fatalf("ParseCatalog() error = %v", err)
	}
	tpl, _ := c.Get("a")
	if tpl.Algorithm != "ES256" {
		t.Errorf("Algorithm = %q, want ES256 (default)", tpl.Algorithm)
	}
	if tpl.TTL != DefaultTTL {
		t.Errorf("TTL = %v, want %v (default)", tpl.TTL, DefaultTTL)
	}
}

func TestParseCatalogRejects(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantSub string
	}{
		{"malformed json", `not json`, "parse catalog"},
		{"missing id", `[{"label":"A","env":"TOKEN_A","claims":{"sub":"x"}}]`, "id is required"},
		{"duplicate id", `[{"id":"a","label":"A","env":"TOKEN_A","claims":{"sub":"x"}},{"id":"a","label":"B","env":"TOKEN_B","claims":{"sub":"x"}}]`, "duplicate id"},
		{"duplicate env", `[{"id":"a","label":"A","env":"TOKEN_A","claims":{"sub":"x"}},{"id":"b","label":"B","env":"TOKEN_A","claims":{"sub":"x"}}]`, "already used by template"},
		{"missing label", `[{"id":"a","env":"TOKEN_A","claims":{"sub":"x"}}]`, "label is required"},
		{"missing env", `[{"id":"a","label":"A","claims":{"sub":"x"}}]`, "env is required"},
		{"lowercase env", `[{"id":"a","label":"A","env":"token_a","claims":{"sub":"x"}}]`, "env must match"},
		{"env leading digit", `[{"id":"a","label":"A","env":"1TOKEN","claims":{"sub":"x"}}]`, "env must match"},
		{"reserved env prefix", `[{"id":"a","label":"A","env":"MULTICA_X","claims":{"sub":"x"}}]`, "reserved"},
		{"reserved env name", `[{"id":"a","label":"A","env":"PATH","claims":{"sub":"x"}}]`, "reserved"},
		// From the shared daemon list (pkg/protocol): the daemon would drop
		// this at injection, so it must fail at boot instead.
		{"reserved env name shared with daemon", `[{"id":"a","label":"A","env":"CURSOR_MCP_AUTH_SOURCE","claims":{"sub":"x"}}]`, "reserved"},
		// Not on the daemon blocklist, but overwritten by the Hermes overlay
		// after tokens are layered — a token there never reaches the agent.
		{"reserved env name hermes home", `[{"id":"a","label":"A","env":"HERMES_HOME","claims":{"sub":"x"}}]`, "reserved"},
		{"empty claims", `[{"id":"a","label":"A","env":"TOKEN_A","claims":{}}]`, "claims is required"},
		{"bad algorithm", `[{"id":"a","label":"A","env":"TOKEN_A","algorithm":"HS256","claims":{"sub":"x"}}]`, "unsupported algorithm"},
		{"bad ttl", `[{"id":"a","label":"A","env":"TOKEN_A","ttl":"soon","claims":{"sub":"x"}}]`, "invalid ttl"},
		{"ttl over max", `[{"id":"a","label":"A","env":"TOKEN_A","ttl":"48h","claims":{"sub":"x"}}]`, "exceeds max"},
		{"ttl non-positive", `[{"id":"a","label":"A","env":"TOKEN_A","ttl":"0s","claims":{"sub":"x"}}]`, "must be positive"},
		{"unknown variable", `[{"id":"a","label":"A","env":"TOKEN_A","claims":{"sub":"{{identity.salary}}"}}]`, "unknown variable"},
		{"reserved claim", `[{"id":"a","label":"A","env":"TOKEN_A","claims":{"sub":"x","exp":"1"}}]`, "reserved claim"},
		{"empty allowed domain", `[{"id":"a","label":"A","env":"TOKEN_A","allowed_domains":[""],"claims":{"sub":"x"}}]`, "allowed_domains"},
		{"allowed domain with @", `[{"id":"a","label":"A","env":"TOKEN_A","allowed_domains":["a@b.c"],"claims":{"sub":"x"}}]`, "allowed_domains"},
		{"allowed domain with space", `[{"id":"a","label":"A","env":"TOKEN_A","allowed_domains":["corp .com"],"claims":{"sub":"x"}}]`, "allowed_domains"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseCatalog(tc.raw)
			if err == nil {
				t.Fatalf("ParseCatalog() error = nil, want error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("ParseCatalog() error = %q, want it to contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}
