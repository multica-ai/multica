package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/client-go/kubernetes"
)

// keychainPayload mirrors Claude Code's expected credentials.json shape.
type keychainPayload struct {
	ClaudeAiOauth oauthBlob `json:"claudeAiOauth"`
}

type oauthBlob struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"` // millis since epoch
	Scopes           []string `json:"scopes"`
	SubscriptionType string   `json:"subscriptionType"`
}

// Scopes the Claude Code CLI uses; stable across rotations.
var defaultScopes = []string{
	"user:profile",
	"user:inference",
	"user:sessions:claude_code",
	"user:mcp_servers",
}

// SyncResult captures the per-sync outcome for the caller / logs.
type SyncResult struct {
	Wrote          bool
	OldFingerprint string // sha256 hex of the prior Keychain blob (empty if absent)
	NewFingerprint string // sha256 hex of the freshly built blob
}

// SyncOnce executes the read → transform → diff → write pipeline once.
func SyncOnce(ctx context.Context, cfg *Config, k kubernetes.Interface, kc Keychain, logger *slog.Logger) (*SyncResult, error) {
	state, err := ReadBrokerState(ctx, k, cfg.Namespace, cfg.SecretName)
	if err != nil {
		return nil, err
	}

	payload := keychainPayload{ClaudeAiOauth: oauthBlob{
		AccessToken:      state.AccessToken,
		RefreshToken:     state.RefreshToken,
		ExpiresAt:        state.ExpiresAt.UnixMilli(),
		Scopes:           defaultScopes,
		SubscriptionType: "max",
	}}
	newBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	newFP := fingerprint(newBytes)

	result := &SyncResult{NewFingerprint: newFP}
	if existing, err := kc.Read(cfg.KeychainService, cfg.KeychainAccount); err == nil {
		result.OldFingerprint = fingerprint(existing)
		if result.OldFingerprint == newFP {
			logger.Info("keychain already current", "fingerprint", newFP)
			return result, nil
		}
		logger.Info("keychain out of date, rotating",
			"from", result.OldFingerprint, "to", newFP)
	} else {
		logger.Info("keychain entry missing, creating", "to", newFP)
	}

	if cfg.DryRun {
		logger.Info("dry-run; not writing keychain")
		return result, nil
	}
	if err := kc.Write(cfg.KeychainService, cfg.KeychainAccount, newBytes); err != nil {
		return nil, fmt.Errorf("keychain write: %w", err)
	}
	result.Wrote = true
	logger.Info("keychain updated",
		"service", cfg.KeychainService,
		"account", cfg.KeychainAccount,
		"expires_at", state.ExpiresAt.Format(time.RFC3339))
	return result, nil
}

// SyncLoop runs SyncOnce on cfg.Interval until ctx is cancelled. Transient
// errors are logged but never terminate the loop — a long-running daemon
// shouldn't die because the cluster blipped.
func SyncLoop(ctx context.Context, cfg *Config, k kubernetes.Interface, kc Keychain, logger *slog.Logger) {
	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	if _, err := SyncOnce(ctx, cfg, k, kc, logger); err != nil {
		logger.Error("initial sync failed", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := SyncOnce(ctx, cfg, k, kc, logger); err != nil {
				logger.Error("sync tick failed", "error", err)
			}
		}
	}
}

func fingerprint(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
