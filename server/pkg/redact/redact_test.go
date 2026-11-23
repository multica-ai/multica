package redact

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRedactAWSAccessKey(t *testing.T) {
	t.Parallel()
	input := "Found key AKIAIOSFODNN7EXAMPLE in config"
	got := Text(input)
	if strings.Contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("AWS key not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED AWS KEY]") {
		t.Fatalf("expected [REDACTED AWS KEY] placeholder, got: %s", got)
	}
}

func TestRedactAWSSecretKey(t *testing.T) {
	t.Parallel()
	input := "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	got := Text(input)
	if strings.Contains(got, "wJalrXUtnFEMI") {
		t.Fatalf("AWS secret not redacted: %s", got)
	}
}

func TestRedactPrivateKey(t *testing.T) {
	t.Parallel()
	input := "Here is the key:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEow...\n-----END RSA PRIVATE KEY-----\nDone."
	got := Text(input)
	if strings.Contains(got, "MIIEow") {
		t.Fatalf("private key content not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED PRIVATE KEY]") {
		t.Fatalf("expected [REDACTED PRIVATE KEY] placeholder, got: %s", got)
	}
}

func TestRedactGitHubToken(t *testing.T) {
	t.Parallel()
	input := "export GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn"
	got := Text(input)
	if strings.Contains(got, "ghp_") {
		t.Fatalf("GitHub token not redacted: %s", got)
	}
}

// asm joins fragments into a credential-shaped test fixture. The token is kept
// split across fragments so the full value never appears as a contiguous
// literal in source — otherwise secret scanners (including GitHub push
// protection) flag these redaction-test fixtures as real credentials. The
// runtime string is identical, so the patterns are exercised exactly the same.
func asm(parts ...string) string { return strings.Join(parts, "") }

// TestRedactGitHubFineGrainedToken guards the github_pat_ prefix used by
// GitHub fine-grained personal access tokens. The classic-token pattern only
// covers ghp_/gho_/ghu_/ghs_/ghr_, so before the dedicated pattern was added a
// fine-grained PAT reached the DB / WS broadcast unredacted.
func TestRedactGitHubFineGrainedToken(t *testing.T) {
	t.Parallel()
	input := "cloning with token " + asm("github_", "pat_", "11ABCDE0Q0abcdefghijkl_MNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyzABCD")
	got := Text(input)
	if strings.Contains(got, asm("github_", "pat_", "11ABCDE0Q0")) {
		t.Fatalf("fine-grained GitHub PAT not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED GITHUB TOKEN]") {
		t.Fatalf("expected [REDACTED GITHUB TOKEN] placeholder, got: %s", got)
	}
}

func TestRedactGoogleAPIKeyEndingWithDash(t *testing.T) {
	t.Parallel()
	input := `the config file still had "` + asm("AIza", "SyB1cD3fGhIjKlMnOpQrStUvWxYz012345-") + `" in it`
	got := Text(input)
	if strings.Contains(got, asm("AIza", "SyB1cD3f")) {
		t.Fatalf("Google API key ending with dash not redacted: %s", got)
	}
	if !strings.Contains(got, `"[REDACTED GOOGLE API KEY]" in it`) {
		t.Fatalf("expected delimiter to be preserved, got: %s", got)
	}
}

func TestRedactOpenAIKey(t *testing.T) {
	t.Parallel()
	input := "OPENAI_API_KEY=sk-proj-abc123def456ghi789jkl012mno345"
	got := Text(input)
	if strings.Contains(got, "sk-proj-abc123") {
		t.Fatalf("OpenAI key not redacted: %s", got)
	}
}

func TestRedactSlackToken(t *testing.T) {
	t.Parallel()
	input := "token: xoxb-123456789012-1234567890123-AbCdEfGhIjKl"
	got := Text(input)
	if strings.Contains(got, "xoxb-") {
		t.Fatalf("Slack token not redacted: %s", got)
	}
}

// TestRedactSlackAppAndConfigTokens guards the xapp- (app-level) and xoxe-
// (config/refresh) prefixes that the original xox[bporas]- rule did not cover.
func TestRedactSlackAppAndConfigTokens(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, in, leak string }{
		{"app-level xapp-", "connecting with " + asm("xa", "pp-1-A0000000000-1111111111-abcdefdeadbeefcafe") + " now", asm("xa", "pp-1-A0000000000")},
		{"config xoxe-", "refresh with " + asm("xo", "xe-1-My0abcdefghijklmnopqrstuvwx") + " now", asm("xo", "xe-1-My0abcdef")},
	} {
		got := Text(tc.in)
		if strings.Contains(got, tc.leak) {
			t.Fatalf("%s not redacted: %s", tc.name, got)
		}
		if !strings.Contains(got, "[REDACTED SLACK TOKEN]") {
			t.Fatalf("%s: expected [REDACTED SLACK TOKEN], got: %s", tc.name, got)
		}
	}
}

// TestRedactGoogleAPIKey guards the AIza-prefixed Google API key format, which
// no prior pattern covered.
func TestRedactGoogleAPIKey(t *testing.T) {
	t.Parallel()
	input := "calling gemini with " + asm("AIza", "SyD1234567890abcdefghijklmnopqrstuv") + " now"
	got := Text(input)
	if strings.Contains(got, asm("AIza", "SyD1234567890")) {
		t.Fatalf("Google API key not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED GOOGLE API KEY]") {
		t.Fatalf("expected [REDACTED GOOGLE API KEY], got: %s", got)
	}
}

// TestRedactStripeLiveKey guards Stripe secret/restricted live keys, which use
// an underscore (sk_live_) and so are missed by the hyphen-form sk- rule.
// Publishable keys (pk_live_) are intentionally NOT redacted — they are public.
func TestRedactStripeLiveKey(t *testing.T) {
	t.Parallel()
	if got := Text("STRIPE_SECRET_KEY=" + asm("sk_", "live_", "51Abcdef0000000000000000")); strings.Contains(got, asm("sk_", "live_51Abcdef")) {
		t.Fatalf("Stripe live key not redacted: %s", got)
	}
	if got := Text("restricted " + asm("rk_", "live_", "51Abcdef0000000000000000")); !strings.Contains(got, "[REDACTED STRIPE KEY]") {
		t.Fatalf("Stripe restricted key not redacted: %s", got)
	}
	// Publishable keys are public and must survive untouched.
	if got := Text(asm("pk_", "live_", "51Abcdef0000000000000000")); !strings.Contains(got, asm("pk_", "live_51Abcdef")) {
		t.Fatalf("publishable key should NOT be redacted: %s", got)
	}
}

func TestRedactBearerToken(t *testing.T) {
	t.Parallel()
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123"
	got := Text(input)
	if strings.Contains(got, "eyJhbGci") {
		t.Fatalf("Bearer token not redacted: %s", got)
	}
}

// TestRedactBearerMCPToken is a regression guard for the Composio MCP session
// headers (MUL-3720): the SDK attaches the project key as `Bearer mcp_...` on
// some MCP transports, so the generic Bearer pattern must mask it before it can
// reach a log line or WS broadcast.
func TestRedactBearerMCPToken(t *testing.T) {
	t.Parallel()
	input := "connecting with Authorization: Bearer mcp_AbCdEf0123456789-_token"
	got := Text(input)
	if strings.Contains(got, "mcp_AbCdEf0123456789") {
		t.Fatalf("Bearer mcp_ token not redacted: %s", got)
	}
	if !strings.Contains(got, "Bearer [REDACTED]") {
		t.Fatalf("expected Bearer [REDACTED] placeholder, got: %s", got)
	}
}

func TestRedactGenericCredentials(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"API_KEY", "API_KEY=mysupersecretkey123"},
		{"DATABASE_URL", "DATABASE_URL=postgres://user:pass@host/db"},
		{"DB_PASSWORD", "DB_PASSWORD: hunter2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Text(tc.input)
			if !strings.Contains(got, "[REDACTED CREDENTIAL]") {
				t.Fatalf("expected credential redaction for %s, got: %s", tc.name, got)
			}
		})
	}
}

func TestNoFalsePositivesOnNormalText(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"This is a normal commit message about fixing a bug",
		"The function returns skip-navigation as the class name",
		"Created PR #42 for the authentication feature",
		"Running tests in /tmp/test-workspace/project",
		"The API endpoint /api/issues/123 was updated",
	}
	for _, input := range inputs {
		got := Text(input)
		if got != input {
			t.Fatalf("false positive redaction:\n  input:  %s\n  output: %s", input, got)
		}
	}
}

func TestRedactGitLabToken(t *testing.T) {
	t.Parallel()
	input := "GITLAB_TOKEN=glpat-AbCdEfGhIjKlMnOpQrStUvWx"
	got := Text(input)
	if strings.Contains(got, "glpat-") {
		t.Fatalf("GitLab token not redacted: %s", got)
	}
}

// TestRedactMulticaTokens guards the mat_/mul_/mdt_/mcn_ credentials minted
// by auth.GeneratePATToken and siblings (prefix + 40 hex characters). The
// generic TOKEN=/Bearer rules only cover some surrounding syntax, so a bare
// token in a CLI flag, error string, or JSON field used to reach the
// database and WebSocket broadcast unredacted.
func TestRedactMulticaTokens(t *testing.T) {
	t.Parallel()
	const hex40 = "0123456789abcdef0123456789abcdef01234567"
	prefixes := []string{"mat_", "mul_", "mdt_", "mcn_"}
	for _, prefix := range prefixes {
		token := asm(prefix, hex40)
		got := Text("retrying with --token " + token + " and moving on")
		if strings.Contains(got, token) || strings.Contains(got, hex40) {
			t.Fatalf("%s token not redacted: %s", prefix, got)
		}
		if got != "retrying with --token [REDACTED_MULTICA_TOKEN] and moving on" {
			t.Fatalf("%s: surrounding text not preserved: %s", prefix, got)
		}
	}

	// Nested JSON values are walked by InputMap through Text; a bare
	// auth_token field must be masked the same way as a CLI flag.
	token := asm("mat_", hex40)
	got := InputMap(map[string]any{
		"auth_token": token,
		"args":       []any{"--token", token},
	})
	if got["auth_token"] != "[REDACTED_MULTICA_TOKEN]" {
		t.Fatalf("InputMap auth_token = %#v, want the Multica placeholder", got["auth_token"])
	}
	args, ok := got["args"].([]any)
	if !ok || len(args) != 2 || args[1] != "[REDACTED_MULTICA_TOKEN]" {
		t.Fatalf("InputMap args = %#v, want the flag value redacted", got["args"])
	}

	// Wrong length, leftover test-style prefixes, and ordinary words that
	// only share a letter prefix must stay untouched.
	untouched := []string{
		asm("mat_", "0123456789abcdef0123456789abcdef0123456"),   // 39 hex
		asm("mat_", "0123456789abcdef0123456789abcdef012345678"), // 41 hex
		"mat_self_mention_not_a_real_token",
		"the matrix_0123456789abcdef0123456789abcdef01234567 id",
	}
	for _, input := range untouched {
		if got := Text(input); got != input {
			t.Fatalf("false positive redaction:\n  input:  %s\n  output: %s", input, got)
		}
	}
}

func TestRedactMulticaTokenContext(t *testing.T) {
	t.Parallel()

	const hex40 = "0123456789abcdef0123456789abcdef01234567"
	token := asm("mat_", hex40)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"environment assignment", "MULTICA_TOKEN=" + token, "MULTICA_[REDACTED CREDENTIAL]"},
		{
			name:  "escaped JSON",
			input: `{"value":"line1\n` + token + `\t` + token + `"}`,
			want:  `{"value":"line1\n[REDACTED_MULTICA_TOKEN]\t[REDACTED_MULTICA_TOKEN]"}`,
		},
		{"embedded in an identifier", "prefix_" + token, "prefix_" + token},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Text(tt.input); got != tt.want {
				t.Errorf("Text() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRedactJWT(t *testing.T) {
	t.Parallel()
	input := "token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	got := Text(input)
	if strings.Contains(got, "eyJhbGci") {
		t.Fatalf("JWT not redacted: %s", got)
	}
}

func TestRedactConnectionString(t *testing.T) {
	t.Parallel()
	input := "connecting to postgres://admin:s3cret@db.example.com:5432/mydb"
	got := Text(input)
	if strings.Contains(got, "s3cret") {
		t.Fatalf("connection string password not redacted: %s", got)
	}
}

func TestRedactPasswordEnvVar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"PASSWORD", "PASSWORD=hunter2"},
		{"SECRET", "SECRET=mysecretvalue"},
		{"TOKEN", "TOKEN=abc123xyz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Text(tc.input)
			if !strings.Contains(got, "[REDACTED CREDENTIAL]") {
				t.Fatalf("expected credential redaction for %s, got: %s", tc.name, got)
			}
		})
	}
}

func TestInputMap(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"command":   "echo sk-proj-abc123def456ghi789jkl012mno345",
		"file_path": "/tmp/test.txt",
		"count":     42,
	}
	got := InputMap(m)
	if s, ok := got["command"].(string); ok {
		if strings.Contains(s, "sk-proj") {
			t.Fatalf("API key in input map not redacted: %s", s)
		}
	}
	// Non-string values preserved
	if got["count"] != 42 {
		t.Fatalf("non-string value altered: %v", got["count"])
	}
	// Clean strings unchanged
	if got["file_path"] != "/tmp/test.txt" {
		t.Fatalf("clean string altered: %v", got["file_path"])
	}
}

func TestInputMapNil(t *testing.T) {
	t.Parallel()
	if got := InputMap(nil); got != nil {
		t.Fatalf("expected nil, got: %v", got)
	}
}

// A Codex file edit arrives as changes[]{path, diff, content}. Before the
// nested walk these strings bypassed redaction entirely and reached the DB.
func TestInputMapRedactsNestedSliceOfMaps(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"changes": []any{
			map[string]any{
				"path": "cfg.go",
				"diff": "+api_key: sk-proj-abc123def456ghi789jkl012mno345",
			},
			map[string]any{
				"path":    ".env",
				"content": "GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn",
			},
		},
	}
	got := InputMap(m)

	changes, ok := got["changes"].([]any)
	if !ok || len(changes) != 2 {
		t.Fatalf("changes not preserved as a 2-element slice: %#v", got["changes"])
	}
	first, _ := changes[0].(map[string]any)
	if diff, _ := first["diff"].(string); strings.Contains(diff, "sk-proj") {
		t.Fatalf("API key nested in changes[0].diff not redacted: %s", diff)
	}
	if first["path"] != "cfg.go" {
		t.Fatalf("clean nested string altered: %v", first["path"])
	}
	second, _ := changes[1].(map[string]any)
	if content, _ := second["content"].(string); strings.Contains(content, "ghp_ABCDEFGH") {
		t.Fatalf("token nested in changes[1].content not redacted: %s", content)
	}
}

func TestInputMapRedactsDeeplyNestedMaps(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": []any{"leak sk-proj-abc123def456ghi789jkl012mno345"},
			},
		},
	}
	got := InputMap(m)
	a, _ := got["a"].(map[string]any)
	b, _ := a["b"].(map[string]any)
	c, _ := b["c"].([]any)
	if len(c) != 1 {
		t.Fatalf("nested slice not preserved: %#v", b["c"])
	}
	if s, _ := c[0].(string); strings.Contains(s, "sk-proj") {
		t.Fatalf("API key at depth 4 not redacted: %s", s)
	}
}

// Home directory paths are intentionally left intact — see Text's doc comment.
// This guards the removal: a path under $HOME must survive verbatim, so nobody
// reintroduces host-path masking as an incidental side effect of another fix.
func TestTextLeavesHomePathIntact(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory resolved in this environment")
	}
	path := home + "/secret/app.go"

	if got := Text(path); got != path {
		t.Fatalf("home path was rewritten:\n  input:  %s\n  output: %s", path, got)
	}

	m := map[string]any{"changes": []any{map[string]any{"path": path}}}
	changes, _ := InputMap(m)["changes"].([]any)
	first, _ := changes[0].(map[string]any)
	if got, _ := first["path"].(string); got != path {
		t.Fatalf("nested home path was rewritten:\n  input:  %s\n  output: %s", path, got)
	}
}

func TestInputMapRedactsStringSliceAndStringMap(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"argv": []string{"curl", "-H", "Authorization: Bearer abc123def456"},
		"env":  map[string]string{"TOKEN": "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn"},
	}
	got := InputMap(m)

	argv, ok := got["argv"].([]string)
	if !ok || len(argv) != 3 {
		t.Fatalf("argv not preserved as []string: %#v", got["argv"])
	}
	if strings.Contains(argv[2], "abc123def456") {
		t.Fatalf("bearer token in []string not redacted: %s", argv[2])
	}
	env, ok := got["env"].(map[string]string)
	if !ok {
		t.Fatalf("env not preserved as map[string]string: %#v", got["env"])
	}
	if strings.Contains(env["TOKEN"], "ghp_ABCDEFGH") {
		t.Fatalf("token in map[string]string not redacted: %s", env["TOKEN"])
	}
}

// The caller keeps using the map it passed in, so redaction must not write
// through the shared nested references.
func TestInputMapDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	secret := "sk-proj-abc123def456ghi789jkl012mno345"
	inner := map[string]any{"diff": secret}
	m := map[string]any{"changes": []any{inner}}

	_ = InputMap(m)

	if inner["diff"] != secret {
		t.Fatalf("input map was mutated in place: %v", inner["diff"])
	}
}

// Depth is attacker-influenced, so the walk must bottom out instead of
// exhausting the stack — and must not hand back a raw string when it does.
func TestInputMapBoundsRecursionDepth(t *testing.T) {
	t.Parallel()
	leaf := map[string]any{"leak": "sk-proj-abc123def456ghi789jkl012mno345"}
	cur := leaf
	for i := 0; i < 5000; i++ {
		cur = map[string]any{"next": cur}
	}

	got := InputMap(cur) // must not stack overflow

	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal redacted map: %v", err)
	}
	if strings.Contains(string(blob), "sk-proj") {
		t.Fatalf("secret past the depth limit was returned unredacted")
	}
	if !strings.Contains(string(blob), depthLimitPlaceholder) {
		t.Fatalf("expected depth-limit placeholder in output: %s", truncateForLog(string(blob)))
	}
}

func truncateForLog(s string) string {
	if len(s) <= 200 {
		return s
	}
	return s[:200] + "…"
}

func TestRedactMultipleSecrets(t *testing.T) {
	t.Parallel()
	input := "Keys: AKIAIOSFODNN7EXAMPLE and ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn"
	got := Text(input)
	if strings.Contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatal("AWS key not redacted in multi-secret text")
	}
	if strings.Contains(got, "ghp_") {
		t.Fatal("GitHub token not redacted in multi-secret text")
	}
}
