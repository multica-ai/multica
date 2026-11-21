// claude-managed-runtime runs a server-hosted Multica to Claude Managed Agents bridge.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/multica-ai/multica/server/internal/claudemanagedruntime"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	configPath := flag.String("config", "", "JSON configuration file; supplied fields override environment variables")
	checkOnly := flag.Bool("check", false, "Verify API access without registering a runtime or running tasks")
	flag.Parse()
	cfg, err := loadConfig(*configPath, os.Getenv)
	if err != nil {
		log.Error("read bridge configuration", "error", err)
		os.Exit(1)
	}
	b, err := claudemanagedruntime.New(cfg, log)
	if err != nil {
		log.Error("invalid bridge configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *checkOnly {
		if err := b.Check(ctx); err != nil {
			log.ErrorContext(ctx, "bridge check failed", "error", err)
			os.Exit(1)
		}
		log.InfoContext(ctx, "bridge API access verified")
		return
	}
	if err := b.Run(ctx); err != nil {
		log.ErrorContext(ctx, "Claude Managed Agents bridge stopped", "error", err)
		os.Exit(1)
	}
}

func loadConfig(path string, getenv func(string) string) (claudemanagedruntime.Config, error) {
	cfg := claudemanagedruntime.Config{
		MulticaURL: getenv("MULTICA_SERVER_URL"), MulticaToken: getenv("MULTICA_TOKEN"), WorkspaceID: getenv("MULTICA_WORKSPACE_ID"),
		AnthropicURL: getenv("ANTHROPIC_BASE_URL"), AnthropicToken: getenv("ANTHROPIC_API_KEY"), EnvironmentID: getenv("CLAUDE_MANAGED_ENVIRONMENT_ID"),
		StateDir: getenv("CLAUDE_MANAGED_STATE_DIR"), Name: getenv("CLAUDE_MANAGED_NAME"),
	}
	for _, id := range strings.Split(getenv("CLAUDE_MANAGED_VAULT_IDS"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			cfg.VaultIDs = append(cfg.VaultIDs, id)
		}
	}
	if path == "" {
		return cfg, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return cfg, fmt.Errorf("cannot open configuration file")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return cfg, fmt.Errorf("cannot read configuration or file exceeds 1 MiB")
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return cfg, fmt.Errorf("configuration must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("invalid configuration JSON or unknown field")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return cfg, fmt.Errorf("configuration must contain one JSON object")
	}
	return cfg, nil
}
