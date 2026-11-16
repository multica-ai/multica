package handler

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestHandlerTestMainRejectsConfiguredDatabaseFailure(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	// Own the endpoint for the entire test and reject PostgreSQL connections.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	const password = "handler-bootstrap-password-canary"
	dbURL := "postgres://handler_bootstrap:" + password + "@" + listener.Addr().String() + "/handler_bootstrap?sslmode=disable&connect_timeout=2"
	tests := []struct {
		name    string
		url     string
		message string
	}{
		{
			name:    "malformed URL",
			url:     dbURL + "&application_name=%zz",
			message: "Failed to configure handler test database from DATABASE_URL",
		},
		{
			name:    "invalid configuration",
			url:     dbURL + "&pool_max_conns=invalid",
			message: "Failed to configure handler test database from DATABASE_URL",
		},
		{
			name:    "connection failure",
			url:     dbURL,
			message: "Failed to connect to handler test database configured by DATABASE_URL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCreateSkillSkipsSkillMdFile$", "-test.v")
			cmd.Env = append(os.Environ(), "DATABASE_URL="+tt.url)
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("test bootstrap timed out: %v", ctx.Err())
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected a nonzero bootstrap exit, got %v; output: %s", err, output)
			}
			text := string(output)
			if !strings.Contains(text, tt.message) {
				t.Errorf("missing database failure diagnostic; output: %s", text)
			}
			if strings.Contains(text, "=== RUN") {
				t.Errorf("bootstrap reached a test case; output: %s", text)
			}
			if strings.Contains(text, password) {
				t.Error("database failure diagnostic exposed the password")
			}
		})
	}
}
