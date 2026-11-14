package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestOpenclawExecuteProtocolBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, tc := range []struct{ name, raw, status, output string }{
		{"accepted", `{"runId":"r","status":"accepted"}`, "failed", ""},
		{"empty-result", `{"runId":"r","status":"ok","result":null}`, "failed", ""},
		{"nested-empty-error", `{"runId":"r","status":"error","result":{"meta":{"error":{}}}}`, "failed", ""},
		{"unknown-event", `{"type":"unknown"}`, "failed", ""},
		{"plain-text", "ordinary reply", "completed", "ordinary reply"},
		{"legacy", `{"payloads":[{"text":"OK"}],"meta":{}}`, "completed", "OK"},
		{"nested", `{"runId":"r","status":"ok","result":{"payloads":[{"text":"OK"}],"meta":{}}}`, "completed", "OK"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "openclaw")
			script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'openclaw 2026.5.5\\n'; exit 0; fi\nprintf '%s\\n' '" + tc.raw + "'\n"
			writeTestExecutable(t, path, []byte(script))
			backend, err := New("openclaw", Config{ExecutablePath: path, Logger: slog.Default()})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "synthetic no-tools", ExecOptions{Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			select {
			case result := <-session.Result:
				if result.Status != tc.status || result.Output != tc.output || (tc.status == "failed" && result.Error == "") {
					t.Fatalf("%+v", result)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
