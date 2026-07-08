package featureflag

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeTempFile(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestLoadRulesFromYAMLFileSimple(t *testing.T) {
	t.Parallel()
	path := writeTempFile(t, "flags.yaml", `
billing_new_invoice_email:
  default: true
ops_disable_recommendations:
  default: false
`)
	rules, err := LoadRulesFromYAMLFile(path)
	if err != nil {
		t.Fatalf("LoadRulesFromYAMLFile: %v", err)
	}
	if got := rules["billing_new_invoice_email"].Default; got != true {
		t.Fatalf("billing_new_invoice_email Default = %v, want true", got)
	}
	if got := rules["ops_disable_recommendations"].Default; got != false {
		t.Fatalf("ops_disable_recommendations Default = %v, want false", got)
	}
}

func TestLoadRulesFromYAMLFileFullShape(t *testing.T) {
	t.Parallel()
	path := writeTempFile(t, "flags.yaml", `
checkout_algo:
  default: false
  variant: experiment-v2
  allow: ["user-internal"]
  allow_by: user_id
  deny: ["banned-tenant"]
  deny_by: workspace_id
  percent:
    percent: 25
    by: user_id
`)
	rules, err := LoadRulesFromYAMLFile(path)
	if err != nil {
		t.Fatalf("LoadRulesFromYAMLFile: %v", err)
	}
	r := rules["checkout_algo"]
	if r.Default != false {
		t.Fatalf("Default = %v, want false", r.Default)
	}
	if r.Variant != "experiment-v2" {
		t.Fatalf("Variant = %q, want experiment-v2", r.Variant)
	}
	if len(r.Allow) != 1 || r.Allow[0] != "user-internal" {
		t.Fatalf("Allow = %#v", r.Allow)
	}
	if r.AllowBy != "user_id" {
		t.Fatalf("AllowBy = %q", r.AllowBy)
	}
	if len(r.Deny) != 1 || r.Deny[0] != "banned-tenant" {
		t.Fatalf("Deny = %#v", r.Deny)
	}
	if r.DenyBy != "workspace_id" {
		t.Fatalf("DenyBy = %q", r.DenyBy)
	}
	if r.Percent == nil || r.Percent.Percent != 25 || r.Percent.By != "user_id" {
		t.Fatalf("Percent = %#v", r.Percent)
	}
}

func TestLoadRulesFromYAMLFileEmpty(t *testing.T) {
	t.Parallel()
	// An empty file is a valid "no flags yet" state — server must still
	// boot. Same for a whitespace-only file.
	for _, body := range []string{"", "   \n\n   "} {
		path := writeTempFile(t, "flags.yaml", body)
		rules, err := LoadRulesFromYAMLFile(path)
		if err != nil {
			t.Fatalf("empty file should not error, got %v", err)
		}
		if rules == nil {
			t.Fatalf("empty file should return non-nil empty map")
		}
		if len(rules) != 0 {
			t.Fatalf("empty file should return empty map, got %d entries", len(rules))
		}
	}
}

func TestLoadRulesFromYAMLFileMissing(t *testing.T) {
	t.Parallel()
	_, err := LoadRulesFromYAMLFile("/no/such/path/flags.yaml")
	if err == nil {
		t.Fatalf("missing file must error")
	}
}

func TestLoadRulesFromYAMLFileMalformed(t *testing.T) {
	t.Parallel()
	// Invalid YAML (unmatched bracket) — must surface a parse error so
	// operators see the misconfig instead of silently losing the file.
	path := writeTempFile(t, "flags.yaml", "billing: { default: true")
	_, err := LoadRulesFromYAMLFile(path)
	if err == nil {
		t.Fatalf("malformed YAML must error")
	}
}

func TestNewServiceFromEnvNoFile(t *testing.T) {
	// Service must still work when the file env var is unset; that's the
	// "framework adopted but no flags yet" path. Use t.Setenv so the
	// state is restored after the test.
	t.Setenv(EnvFlagFile, "")
	svc, err := NewServiceFromEnv()
	if err != nil {
		t.Fatalf("NewServiceFromEnv: %v", err)
	}
	if svc == nil {
		t.Fatalf("expected non-nil Service")
	}
	// No file, no env override → default flows through.
	if !svc.IsEnabled(context.Background(), "any_flag", true) {
		t.Fatalf("no provider config must honor the caller default")
	}
}

func TestNewServiceFromEnvWithFile(t *testing.T) {
	path := writeTempFile(t, "flags.yaml", `
demo_flag:
  default: true
`)
	t.Setenv(EnvFlagFile, path)
	svc, err := NewServiceFromEnv()
	if err != nil {
		t.Fatalf("NewServiceFromEnv: %v", err)
	}
	if !svc.IsEnabled(context.Background(), "demo_flag", false) {
		t.Fatalf("file rule must override the false default")
	}
}

func TestNewServiceFromEnvEnvBeatsFile(t *testing.T) {
	// The chain is `env -> file`, so FF_<KEY> must win over the YAML.
	// This is the Ops kill-switch path documented in .env.example.
	path := writeTempFile(t, "flags.yaml", `
demo_flag:
  default: true
`)
	t.Setenv(EnvFlagFile, path)
	t.Setenv("FF_DEMO_FLAG", "false")
	svc, err := NewServiceFromEnv()
	if err != nil {
		t.Fatalf("NewServiceFromEnv: %v", err)
	}
	if svc.IsEnabled(context.Background(), "demo_flag", true) {
		t.Fatalf("env override must beat the YAML file (file=true, env=false)")
	}
}

func TestNewServiceFromEnvBadFileSurfacesError(t *testing.T) {
	t.Setenv(EnvFlagFile, "/no/such/file.yaml")
	_, err := NewServiceFromEnv()
	if err == nil {
		t.Fatalf("missing file must surface as an error so operators see misconfig")
	}
}
