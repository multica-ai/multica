// +build integration

package sandbox

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Run with: E2B_API_KEY=xxx go test ./internal/sandbox/ -tags integration -run TestE2BIntegration -v
func TestE2BIntegration(t *testing.T) {
	apiKey := os.Getenv("E2B_API_KEY")
	if apiKey == "" {
		t.Skip("E2B_API_KEY not set, skipping integration test")
	}

	templateID := os.Getenv("E2B_TEMPLATE_ID")
	if templateID == "" {
		templateID = "9q4awrmowr11d4qpxuu3" // today-agent-runtime
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	provider := NewE2BProvider(apiKey)

	// 1. Create sandbox
	t.Log("Creating sandbox...")
	sb, err := provider.CreateOrConnect(ctx, "", CreateOpts{
		TemplateID: templateID,
		Timeout:    5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("CreateOrConnect failed: %v", err)
	}
	t.Logf("Sandbox created: ID=%s", sb.ID)
	defer func() {
		t.Log("Destroying sandbox...")
		provider.Destroy(context.Background(), sb.ID)
	}()

	// 2. Exec simple command
	t.Log("Exec: echo hello...")
	stdout, err := provider.Exec(ctx, sb, []string{"echo", "hello from e2b"})
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	t.Logf("Exec output: %q", stdout)
	if stdout == "" {
		t.Fatal("Expected non-empty stdout")
	}

	// 3. Write file
	t.Log("WriteFile: /tmp/test.txt...")
	err = provider.WriteFile(ctx, sb, "/tmp/test.txt", []byte("hello world"))
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// 4. Read file
	t.Log("ReadFile: /tmp/test.txt...")
	content, err := provider.ReadFile(ctx, sb, "/tmp/test.txt")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(content) != "hello world" {
		t.Fatalf("Expected 'hello world', got %q", string(content))
	}
	t.Log("File roundtrip OK")

	// 5. Check opencode is installed
	t.Log("Checking opencode...")
	version, err := provider.Exec(ctx, sb, []string{"opencode", "--version"})
	if err != nil {
		t.Logf("opencode not found (expected if not using agent template): %v", err)
	} else {
		t.Logf("opencode version: %s", version)
	}

	// 6. Reconnect to existing sandbox
	t.Log("Reconnecting...")
	sb2, err := provider.CreateOrConnect(ctx, sb.ID, CreateOpts{})
	if err != nil {
		t.Fatalf("Reconnect failed: %v", err)
	}
	if sb2.ID != sb.ID {
		t.Fatalf("Expected same sandbox ID %s, got %s", sb.ID, sb2.ID)
	}

	// 7. Test command with exit code
	t.Log("Testing non-zero exit code...")
	_, err = provider.Exec(ctx, sb, []string{"bash", "-c", "exit 42"})
	if err == nil {
		t.Fatal("Expected error for non-zero exit code")
	}
	t.Logf("Non-zero exit: %v (expected)", err)

	fmt.Println("\n=== E2B Integration Test PASSED ===")
}
