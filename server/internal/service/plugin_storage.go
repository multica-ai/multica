package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Plugin storage scopes. There are exactly two, and adding a third is a product
// decision, not a configuration knob.
const (
	// PluginStorageWorkspace is team-shared state. scope_id is the workspace id.
	PluginStorageWorkspace = "workspace"
	// PluginStorageUser is per-member state, including that member's own
	// credentials for the plugin's external service. scope_id is the user id.
	PluginStorageUser = "user"
)

// Storage quotas. Enforced on write and never by eviction: a plugin that hits
// its ceiling gets an explicit error, because silently dropping the oldest key
// would corrupt state the plugin believes it stored.
const (
	MaxPluginStorageKeyBytes   = 1024
	MaxPluginStorageValueBytes = 100 * 1024
	MaxPluginStorageKeys       = 1000
	MaxPluginStorageTotalBytes = 5 * 1024 * 1024
)

// PluginStorageKey is one key in a listing. Values are deliberately absent:
// listing is for a plugin to discover what it wrote, not to bulk-read state.
type PluginStorageKey struct {
	Key       string `json:"key"`
	SizeBytes int64  `json:"size_bytes"`
	UpdatedAt string `json:"updated_at"`
}

// ResolveStorageScope maps a scope name to the row's scope_id.
func ResolveStorageScope(scopeType string, workspaceID, userID pgtype.UUID) (pgtype.UUID, error) {
	switch scopeType {
	case PluginStorageWorkspace:
		return workspaceID, nil
	case PluginStorageUser:
		return userID, nil
	default:
		return pgtype.UUID{}, pluginErrf(PluginErrorInvalid, "storage scope must be %q or %q", PluginStorageWorkspace, PluginStorageUser)
	}
}

func validateStorageKey(key string) error {
	if key == "" {
		return pluginErrf(PluginErrorInvalid, "storage key is required")
	}
	if len(key) > MaxPluginStorageKeyBytes {
		return pluginErrf(PluginErrorQuota, "storage key exceeds %d bytes", MaxPluginStorageKeyBytes)
	}
	return nil
}

// GetStorageValue reads one value. This path can never reach plugin_secret:
// secrets live in a different table with no read query that returns ciphertext
// to a request handler.
func (s *PluginService) GetStorageValue(ctx context.Context, installationID pgtype.UUID, scopeType string, scopeID pgtype.UUID, key string) (string, error) {
	if err := validateStorageKey(key); err != nil {
		return "", err
	}
	row, err := s.Queries.GetPluginStorageValue(ctx, db.GetPluginStorageValueParams{
		InstallationID: installationID,
		ScopeType:      scopeType,
		ScopeID:        scopeID,
		Key:            key,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", pluginErrf(PluginErrorNotFound, "storage key not found")
	}
	if err != nil {
		return "", &PluginError{Kind: PluginErrorUnavailable, Message: "read plugin storage", Err: err}
	}
	return row.Value, nil
}

func (s *PluginService) ListStorageKeys(ctx context.Context, installationID pgtype.UUID, scopeType string, scopeID pgtype.UUID) ([]PluginStorageKey, error) {
	rows, err := s.Queries.ListPluginStorageKeys(ctx, db.ListPluginStorageKeysParams{
		InstallationID: installationID,
		ScopeType:      scopeType,
		ScopeID:        scopeID,
	})
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "list plugin storage", Err: err}
	}
	keys := make([]PluginStorageKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, PluginStorageKey{
			Key:       row.Key,
			SizeBytes: row.SizeBytes,
			UpdatedAt: row.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return keys, nil
}

// EnforceStorageQuota decides whether one write fits, given the scope's usage
// with the candidate key excluded. Kept pure and separate from the write so the
// three limits have one canonical test, and so it is obvious that every bound
// is compared in bytes — the usage query reports octet_length for the same
// reason.
//
// Known bound: usage is read before the write without a lock, so concurrent
// writers to the same scope can overshoot by their own sizes. Serializing every
// plugin KV write to close that gap costs more than the overshoot is worth; the
// limits exist to stop runaway growth, not to be exact to the byte.
func EnforceStorageQuota(usage db.GetPluginStorageUsageRow, valueBytes int) error {
	if valueBytes > MaxPluginStorageValueBytes {
		return pluginErrf(PluginErrorQuota, "storage value exceeds %d bytes", MaxPluginStorageValueBytes)
	}
	if usage.KeyCount+1 > MaxPluginStorageKeys {
		return pluginErrf(PluginErrorQuota, "storage scope already holds the maximum of %d keys", MaxPluginStorageKeys)
	}
	if usage.TotalBytes+int64(valueBytes) > MaxPluginStorageTotalBytes {
		return pluginErrf(PluginErrorQuota, "storage scope exceeds its %d byte budget", MaxPluginStorageTotalBytes)
	}
	return nil
}

// SetStorageValue writes one value after checking every quota. The usage query
// excludes the key being written, so replacing a value is measured as a
// replacement and cannot fail a limit the existing row already occupies.
func (s *PluginService) SetStorageValue(ctx context.Context, installationID pgtype.UUID, scopeType string, scopeID pgtype.UUID, key, value string) error {
	if err := validateStorageKey(key); err != nil {
		return err
	}
	usage, err := s.Queries.GetPluginStorageUsage(ctx, db.GetPluginStorageUsageParams{
		InstallationID: installationID,
		ScopeType:      scopeType,
		ScopeID:        scopeID,
		Key:            key,
	})
	if err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "read plugin storage usage", Err: err}
	}
	if err := EnforceStorageQuota(usage, len(value)); err != nil {
		return err
	}
	if _, err := s.Queries.UpsertPluginStorageValue(ctx, db.UpsertPluginStorageValueParams{
		InstallationID: installationID,
		ScopeType:      scopeType,
		ScopeID:        scopeID,
		Key:            key,
		Value:          value,
	}); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "write plugin storage", Err: err}
	}
	return nil
}

func (s *PluginService) DeleteStorageValue(ctx context.Context, installationID pgtype.UUID, scopeType string, scopeID pgtype.UUID, key string) error {
	if err := validateStorageKey(key); err != nil {
		return err
	}
	rows, err := s.Queries.DeletePluginStorageValue(ctx, db.DeletePluginStorageValueParams{
		InstallationID: installationID,
		ScopeType:      scopeType,
		ScopeID:        scopeID,
		Key:            key,
	})
	if err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "delete plugin storage", Err: err}
	}
	if rows == 0 {
		return pluginErrf(PluginErrorNotFound, "storage key not found")
	}
	return nil
}
