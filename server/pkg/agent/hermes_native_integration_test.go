package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHermesNativeGatewayACPTransport(t *testing.T) {
	source, python := os.Getenv("MULTICA_HERMES_SOURCE"), os.Getenv("MULTICA_HERMES_TEST_PYTHON")
	if source == "" || python == "" {
		t.Skip("explicit stock Hermes source and isolated test interpreter required")
	}
	fixture, err := filepath.Abs("testdata/hermes_native_gateway.py")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	trace := filepath.Join(dir, "trace.json")
	executable := filepath.Join(dir, "stock-hermes")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	writeTestExecutable(t, executable, []byte("#!/bin/sh\nexec "+quote(python)+" "+quote(fixture)+"\n"))
	const marker = "NATIVE_HERMES_GUIDANCE_8ef422ab"
	b := &hermesBackend{cfg: Config{ExecutablePath: executable, Logger: slog.Default(), Env: map[string]string{"HERMES_HOME": dir, "MULTICA_HERMES_SOURCE": source, "GUIDANCE_TRACE": trace, "GUIDANCE_MARKER": marker}}}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	s, err := b.Execute(ctx, "original objective", ExecOptions{Cwd: dir, EnableTaskSupplement: true})
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range s.Messages {
		}
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !s.SupplementReady() {
		select {
		case <-ticker.C:
		case result := <-s.Result:
			t.Fatalf("primary ended before native steer: %+v", result)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if err := s.Supplement(ctx, marker); err != nil {
		t.Fatal(err)
	}
	var result Result
	select {
	case result = <-s.Result:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	<-drained
	if result.Status != "completed" || strings.TrimSpace(result.Output) != marker {
		t.Fatalf("primary=%+v", result)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	var proof struct {
		Kind                     string
		OwnerPID                 int
		Runs, Steers, ModelCalls int
		Messages                 json.RawMessage
	}
	if err := json.Unmarshal(data, &proof); err != nil {
		t.Fatal(err)
	}
	if proof.Kind != "stock-gateway" || proof.OwnerPID <= 0 || proof.Runs != 1 || proof.Steers != 1 || proof.ModelCalls != 2 || !strings.Contains(string(proof.Messages), marker) || !strings.Contains(string(proof.Messages), "original objective") {
		t.Fatalf("native proof kind=%s owner=%d runs=%d steers=%d primary model calls=%d", proof.Kind, proof.OwnerPID, proof.Runs, proof.Steers, proof.ModelCalls)
	}
	usage := result.Usage["custom:controlled-gateway"]
	if usage.InputTokens != 27 || usage.OutputTokens != 8 {
		t.Fatalf("primary usage changed: %+v", result.Usage)
	}
	t.Log("Unmodified Hermes: native /steer, one admission, two model calls, original objective and guidance preserved, usage 27/8")
}
