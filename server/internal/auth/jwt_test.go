package auth

import (
	"os"
	"os/exec"
	"testing"
)

func TestJWTSecret_DefaultInDev(t *testing.T) {
	// In non-production mode with no JWT_SECRET, the default is used.
	// JWTSecret() uses sync.Once so we can only test the first-call path
	// in a subprocess.
	cmd := exec.Command(os.Args[0], "-test.run=TestJWTSecret_Subprocess_DefaultInDev")
	cmd.Env = append(os.Environ(), "APP_ENV=development", "JWT_SECRET=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected no panic in dev mode, got error: %v\noutput:\n%s", err, out)
	}
}

func TestJWTSecret_Subprocess_DefaultInDev(t *testing.T) {
	if os.Getenv("APP_ENV") == "" {
		t.Skip("not running as subprocess")
	}
	_ = JWTSecret()
}

func TestJWTSecret_CustomSecret(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestJWTSecret_Subprocess_CustomSecret")
	cmd.Env = append(os.Environ(), "APP_ENV=development", "JWT_SECRET=my-custom-secret")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected no panic with custom secret, got error: %v\noutput:\n%s", err, out)
	}
}

func TestJWTSecret_Subprocess_CustomSecret(t *testing.T) {
	if os.Getenv("JWT_SECRET") != "my-custom-secret" {
		t.Skip("not running as subprocess")
	}
	secret := JWTSecret()
	if string(secret) != "my-custom-secret" {
		t.Fatalf("expected custom secret, got %q", string(secret))
	}
}

func TestJWTSecret_PanicsInProductionWithDefault(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestJWTSecret_Subprocess_PanicsDefault")
	cmd.Env = append(os.Environ(), "APP_ENV=production", "JWT_SECRET="+defaultJWTSecret)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected panic in production with default JWT_SECRET, but process exited cleanly")
	}
	if e, ok := err.(*exec.ExitError); ok {
		if e.ExitCode() == 0 {
			t.Fatal("expected non-zero exit code from panic")
		}
	}
	_ = out // panic output goes to stderr
}

func TestJWTSecret_Subprocess_PanicsDefault(t *testing.T) {
	if os.Getenv("APP_ENV") != "production" {
		t.Skip("not running as subprocess")
	}
	_ = JWTSecret()
}

func TestJWTSecret_PanicsInProductionWithEmpty(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestJWTSecret_Subprocess_PanicsEmpty")
	cmd.Env = append(os.Environ(), "APP_ENV=production", "JWT_SECRET=")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected panic in production with empty JWT_SECRET, but process exited cleanly")
	}
	if e, ok := err.(*exec.ExitError); ok {
		if e.ExitCode() == 0 {
			t.Fatal("expected non-zero exit code from panic")
		}
	}
	_ = out
}

func TestJWTSecret_Subprocess_PanicsEmpty(t *testing.T) {
	if os.Getenv("APP_ENV") != "production" {
		t.Skip("not running as subprocess")
	}
	_ = JWTSecret()
}

func TestJWTSecret_SucceedsInProductionWithCustomSecret(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestJWTSecret_Subprocess_ProdCustom")
	cmd.Env = append(os.Environ(), "APP_ENV=production", "JWT_SECRET=some-unique-production-key-12345")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected no panic in production with custom secret, got error: %v\noutput:\n%s", err, out)
	}
}

func TestJWTSecret_Subprocess_ProdCustom(t *testing.T) {
	if os.Getenv("JWT_SECRET") != "some-unique-production-key-12345" {
		t.Skip("not running as subprocess")
	}
	secret := JWTSecret()
	if string(secret) != "some-unique-production-key-12345" {
		t.Fatalf("expected custom production secret, got %q", string(secret))
	}
}
