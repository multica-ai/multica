package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

type RemoteMCPSecretBox interface {
	Seal([]byte) ([]byte, error)
	Open([]byte) ([]byte, error)
}

type RemoteMCPConfigInput struct {
	Endpoint      string
	PublicConfig  json.RawMessage
	AuthType      string
	AuthHeader    string
	Credential    string
	FailurePolicy string
}

type RemoteMCPConfigResult struct {
	Config          db.PluginInstallationConfig
	DiscoveredTools []pluginruntime.RemoteMCPTool
	SchemaDigest    string
}

func (s *PluginService) TestRemoteMCP(ctx context.Context, workspaceID, installationID pgtype.UUID, contributionKey string) (RemoteMCPConfigResult, error) {
	contribution, declaration, err := s.loadRemoteMCPDeclaration(ctx, s.Queries, workspaceID, installationID, contributionKey)
	if err != nil {
		return RemoteMCPConfigResult{}, err
	}
	latest, err := s.Queries.GetLatestPluginInstallationConfig(ctx, db.GetLatestPluginInstallationConfigParams{
		WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contribution.ID,
	})
	if err != nil {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorConflict, "Remote MCP is not configured", err)
	}
	headers, err := s.remoteMCPHeaders(ctx, workspaceID, installationID, contribution.ID, latest.AuthType, latest.AuthHeader, "", latest.SecretRef)
	if err != nil {
		return RemoteMCPConfigResult{}, err
	}
	tools, digest, err := remotemcp.Discover(ctx, latest.Endpoint, declaration.EndpointPolicy.AllowedHosts, declaration.ProtocolVersions, headers)
	if err != nil {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorInvalid, "Remote MCP connection test failed", err)
	}
	applyDeclaredRisk(tools, declaration)
	return RemoteMCPConfigResult{Config: latest, DiscoveredTools: tools, SchemaDigest: digest}, nil
}

var remoteMCPHeaderPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,127}$`)

func (s *PluginService) ConfigureRemoteMCP(ctx context.Context, workspaceID, installationID, actorID pgtype.UUID, contributionKey string, input RemoteMCPConfigInput) (RemoteMCPConfigResult, error) {
	contribution, declaration, err := s.loadRemoteMCPDeclaration(ctx, s.Queries, workspaceID, installationID, contributionKey)
	if err != nil {
		return RemoteMCPConfigResult{}, err
	}
	if _, err := remotemcp.ValidatePublicHTTPSEndpoint(ctx, input.Endpoint, declaration.EndpointPolicy.AllowedHosts, nil); err != nil {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorInvalid, "Remote MCP endpoint is not allowed", err)
	}
	publicConfig, err := validateRemoteMCPPublicConfig(input.PublicConfig, declaration.ConfigurationSchema)
	if err != nil {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorInvalid, "Remote MCP public configuration is invalid", err)
	}
	input.AuthType = strings.TrimSpace(input.AuthType)
	input.AuthHeader = strings.TrimSpace(input.AuthHeader)
	input.FailurePolicy = strings.TrimSpace(input.FailurePolicy)
	if input.FailurePolicy == "" {
		input.FailurePolicy = "required"
	}
	if input.FailurePolicy != "required" && input.FailurePolicy != "optional" {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorInvalid, "failure_policy must be required or optional", nil)
	}
	if input.AuthType != "none" && input.AuthType != "bearer" && input.AuthType != "header" {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorInvalid, "auth_type must be none, bearer, or header", nil)
	}
	if input.AuthType == "bearer" {
		input.AuthHeader = "Authorization"
	}
	if input.AuthType == "header" && !validRemoteMCPAuthHeader(input.AuthHeader) {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorInvalid, "auth_header is invalid", nil)
	}
	if input.AuthType == "none" && input.Credential != "" {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorInvalid, "credential is not allowed with auth_type none", nil)
	}

	var existingSecret pgtype.UUID
	if input.AuthType != "none" && input.Credential == "" {
		latest, latestErr := s.Queries.GetLatestPluginInstallationConfig(ctx, db.GetLatestPluginInstallationConfigParams{
			WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contribution.ID,
		})
		if latestErr != nil || !latest.SecretRef.Valid || latest.AuthType != input.AuthType || latest.AuthHeader != input.AuthHeader {
			return RemoteMCPConfigResult{}, newPluginError(PluginErrorInvalid, "credential is required", nil)
		}
		existingSecret = latest.SecretRef
	}
	headers, err := s.remoteMCPHeaders(ctx, workspaceID, installationID, contribution.ID, input.AuthType, input.AuthHeader, input.Credential, existingSecret)
	if err != nil {
		return RemoteMCPConfigResult{}, err
	}
	discovered, digest, err := remotemcp.Discover(ctx, input.Endpoint, declaration.EndpointPolicy.AllowedHosts, declaration.ProtocolVersions, headers)
	if err != nil {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorInvalid, "Remote MCP connection test failed", err)
	}
	applyDeclaredRisk(discovered, declaration)
	discoveredJSON, _ := json.Marshal(discovered)

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return RemoteMCPConfigResult{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.Queries.WithTx(tx)
	secretRef := existingSecret
	if input.AuthType != "none" && input.Credential != "" {
		if _, lockErr := q.LockPluginRemoteMCPInstallation(ctx, db.LockPluginRemoteMCPInstallationParams{
			WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contribution.ID,
		}); lockErr != nil {
			return RemoteMCPConfigResult{}, lockErr
		}
		if s.RemoteMCPSecrets == nil {
			return RemoteMCPConfigResult{}, newPluginError(PluginErrorIncompatible, "Remote MCP credential encryption is not configured", nil)
		}
		sealed, sealErr := s.RemoteMCPSecrets.Seal([]byte(input.Credential))
		if sealErr != nil {
			return RemoteMCPConfigResult{}, fmt.Errorf("encrypt Remote MCP credential: %w", sealErr)
		}
		if _, revokeErr := q.RevokePluginRemoteMCPSecrets(ctx, db.RevokePluginRemoteMCPSecretsParams{
			WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contribution.ID,
		}); revokeErr != nil {
			return RemoteMCPConfigResult{}, revokeErr
		}
		secret, createErr := q.CreatePluginRemoteMCPSecret(ctx, db.CreatePluginRemoteMCPSecretParams{
			WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contribution.ID,
			Ciphertext: sealed, Hint: secretHint(input.Credential), CreatedBy: actorID,
		})
		if createErr != nil {
			return RemoteMCPConfigResult{}, createErr
		}
		secretRef = secret.ID
	}
	config, err := q.CreatePluginInstallationConfig(ctx, db.CreatePluginInstallationConfigParams{
		WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contribution.ID,
		Endpoint: input.Endpoint, PublicConfig: publicConfig, AuthType: input.AuthType, AuthHeader: input.AuthHeader,
		SecretRef: secretRef, DiscoveredTools: discoveredJSON,
		DiscoveredSchemaDigest: pgtype.Text{String: digest, Valid: true},
		ApprovedTools:          []byte(`[]`), FailurePolicy: input.FailurePolicy, CreatedBy: actorID,
	})
	if err != nil {
		return RemoteMCPConfigResult{}, err
	}
	if _, err := s.bumpPluginConfigurationGeneration(ctx, q, workspaceID, installationID, actorID); err != nil {
		return RemoteMCPConfigResult{}, err
	}
	if _, err := s.reconcileWorkspaceTx(ctx, q, workspaceID); err != nil {
		return RemoteMCPConfigResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RemoteMCPConfigResult{}, err
	}
	return RemoteMCPConfigResult{Config: config, DiscoveredTools: discovered, SchemaDigest: digest}, nil
}

func validRemoteMCPAuthHeader(header string) bool {
	if !remoteMCPHeaderPattern.MatchString(header) {
		return false
	}
	for _, forbidden := range []string{
		"Host", "Connection", "Content-Length", "Content-Type", "Accept",
		"Transfer-Encoding", "Trailer", "Upgrade", "Proxy-Authorization",
		"Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-ID",
	} {
		if strings.EqualFold(header, forbidden) {
			return false
		}
	}
	return true
}

func (s *PluginService) ReviewRemoteMCPTools(ctx context.Context, workspaceID, installationID, actorID pgtype.UUID, contributionKey string, approvedNames []string) (RemoteMCPConfigResult, error) {
	contribution, declaration, err := s.loadRemoteMCPDeclaration(ctx, s.Queries, workspaceID, installationID, contributionKey)
	if err != nil {
		return RemoteMCPConfigResult{}, err
	}
	latest, err := s.Queries.GetLatestPluginInstallationConfig(ctx, db.GetLatestPluginInstallationConfigParams{
		WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contribution.ID,
	})
	if err != nil {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorConflict, "Remote MCP must be configured before tool review", err)
	}
	headers, err := s.remoteMCPHeaders(ctx, workspaceID, installationID, contribution.ID, latest.AuthType, latest.AuthHeader, "", latest.SecretRef)
	if err != nil {
		return RemoteMCPConfigResult{}, err
	}
	discovered, discoveredDigest, err := remotemcp.Discover(ctx, latest.Endpoint, declaration.EndpointPolicy.AllowedHosts, declaration.ProtocolVersions, headers)
	if err != nil {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorInvalid, "Remote MCP tool discovery failed", err)
	}
	applyDeclaredRisk(discovered, declaration)
	approved, err := selectApprovedRemoteMCPTools(discovered, declaration, approvedNames)
	if err != nil {
		return RemoteMCPConfigResult{}, newPluginError(PluginErrorInvalid, "Remote MCP tool approval is invalid", err)
	}
	digest, err := remotemcp.ToolSetDigest(approved)
	if err != nil {
		return RemoteMCPConfigResult{}, err
	}
	approvedJSON, _ := json.Marshal(approved)
	discoveredJSON, _ := json.Marshal(discovered)

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return RemoteMCPConfigResult{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.Queries.WithTx(tx)
	config, err := q.CreatePluginInstallationConfig(ctx, db.CreatePluginInstallationConfigParams{
		WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contribution.ID,
		Endpoint: latest.Endpoint, PublicConfig: latest.PublicConfig, AuthType: latest.AuthType, AuthHeader: latest.AuthHeader,
		SecretRef: latest.SecretRef, DiscoveredTools: discoveredJSON,
		DiscoveredSchemaDigest: pgtype.Text{String: discoveredDigest, Valid: true}, ApprovedTools: approvedJSON,
		SchemaDigest: pgtype.Text{String: digest, Valid: true}, FailurePolicy: latest.FailurePolicy,
		ReviewedBy: actorID, CreatedBy: actorID,
	})
	if err != nil {
		return RemoteMCPConfigResult{}, err
	}
	if _, err := s.bumpPluginConfigurationGeneration(ctx, q, workspaceID, installationID, actorID); err != nil {
		return RemoteMCPConfigResult{}, err
	}
	if _, err := s.reconcileWorkspaceTx(ctx, q, workspaceID); err != nil {
		return RemoteMCPConfigResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RemoteMCPConfigResult{}, err
	}
	return RemoteMCPConfigResult{Config: config, DiscoveredTools: discovered, SchemaDigest: digest}, nil
}

func (s *PluginService) RevokeRemoteMCPCredential(ctx context.Context, workspaceID, installationID, actorID pgtype.UUID, contributionKey string) error {
	contribution, _, err := s.loadRemoteMCPDeclaration(ctx, s.Queries, workspaceID, installationID, contributionKey)
	if err != nil {
		return err
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.Queries.WithTx(tx)
	if _, err := q.LockPluginRemoteMCPInstallation(ctx, db.LockPluginRemoteMCPInstallationParams{
		WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contribution.ID,
	}); err != nil {
		return err
	}
	if _, err := q.RevokePluginRemoteMCPSecrets(ctx, db.RevokePluginRemoteMCPSecretsParams{
		WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contribution.ID,
	}); err != nil {
		return err
	}
	installation, err := q.GetPluginInstallation(ctx, installationID)
	if err != nil {
		return err
	}
	if _, err := q.SetPluginInstallationDesiredState(ctx, db.SetPluginInstallationDesiredStateParams{
		Enabled: false, UpdatedBy: actorID, WorkspaceID: workspaceID,
		DesiredReleaseID: installation.DesiredReleaseID, ID: installationID,
	}); err != nil {
		return err
	}
	if _, err := s.reconcileWorkspaceTx(ctx, q, workspaceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ResolveTaskRemoteMCPConnections returns only pinned connection metadata.
// Credentials stay server-side and are resolved by the daemon broker's
// authenticated, task-scoped endpoint immediately before an upstream call.
func (s *PluginService) ResolveTaskRemoteMCPConnections(ctx context.Context, taskID pgtype.UUID) ([]pluginruntime.RemoteMCPConnection, []string, error) {
	manifest, err := s.Queries.GetPluginExecutionManifestByTask(ctx, taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	entries, err := pluginruntime.ParseEntries(manifest.OrderedContributions)
	if err != nil {
		return nil, nil, err
	}
	connections := make([]pluginruntime.RemoteMCPConnection, 0)
	diagnostics := make([]string, 0)
	for _, entry := range entries {
		if entry.ContributionType != plugincontract.ContributionRemoteMCPV1 {
			continue
		}
		connection, _, resolveErr := s.resolveTaskRemoteMCPEntry(ctx, manifest.WorkspaceID, entry, false)
		if resolveErr != nil {
			if entry.FailurePolicy == "optional" {
				diagnostics = append(diagnostics, fmt.Sprintf("optional Remote MCP %s unavailable", entry.ContributionKey))
				continue
			}
			return nil, diagnostics, fmt.Errorf("required Remote MCP %s is unavailable: %w", entry.ContributionKey, resolveErr)
		}
		connections = append(connections, connection)
	}
	return connections, diagnostics, nil
}

// ResolveTaskRemoteMCPCredential re-checks the active secret row for every
// broker call. This is the revocation boundary for already-running tasks: a
// revoked or rotated pinned credential becomes unusable before another
// upstream request is made.
func (s *PluginService) ResolveTaskRemoteMCPCredential(ctx context.Context, taskID pgtype.UUID, contributionID string) (string, string, error) {
	manifest, err := s.Queries.GetPluginExecutionManifestByTask(ctx, taskID)
	if err != nil {
		return "", "", newPluginError(PluginErrorNotFound, "Remote MCP task credential not found", err)
	}
	entries, err := pluginruntime.ParseEntries(manifest.OrderedContributions)
	if err != nil {
		return "", "", err
	}
	for _, entry := range entries {
		if entry.ContributionType != plugincontract.ContributionRemoteMCPV1 || entry.ContributionID != contributionID {
			continue
		}
		connection, credential, err := s.resolveTaskRemoteMCPEntry(ctx, manifest.WorkspaceID, entry, true)
		if err != nil {
			return "", "", newPluginError(PluginErrorConflict, "Remote MCP credential is revoked or unavailable", err)
		}
		return connection.CredentialHeader, credential, nil
	}
	return "", "", newPluginError(PluginErrorNotFound, "Remote MCP task contribution not found", nil)
}

func (s *PluginService) resolveTaskRemoteMCPEntry(ctx context.Context, workspaceID pgtype.UUID, entry pluginruntime.CompiledEntry, resolveCredential bool) (pluginruntime.RemoteMCPConnection, string, error) {
	installationID, err := parsePluginRuntimeUUID(entry.InstallationID, "installation")
	if err != nil {
		return pluginruntime.RemoteMCPConnection{}, "", err
	}
	contributionID, err := parsePluginRuntimeUUID(entry.ContributionID, "contribution")
	if err != nil {
		return pluginruntime.RemoteMCPConnection{}, "", err
	}
	if entry.ConfigID == "" || entry.ConfigRevision <= 0 || entry.Endpoint == "" || entry.Transport != "streamable-http" || len(entry.ApprovedTools) == 0 {
		return pluginruntime.RemoteMCPConnection{}, "", errors.New("pinned configuration is incomplete")
	}
	if _, err := remotemcp.ValidatePublicHTTPSEndpoint(ctx, entry.Endpoint, entry.EndpointAllowedHosts, nil); err != nil {
		return pluginruntime.RemoteMCPConnection{}, "", err
	}
	digest, err := remotemcp.ToolSetDigest(entry.ApprovedTools)
	if err != nil || digest != entry.ToolSchemaDigest {
		return pluginruntime.RemoteMCPConnection{}, "", errors.New("pinned tool schema digest is invalid")
	}
	credentialHeader := ""
	credential := ""
	if entry.AuthType != "none" {
		secretID, err := parsePluginRuntimeUUID(entry.SecretRef, "credential")
		if err != nil || s.RemoteMCPSecrets == nil {
			return pluginruntime.RemoteMCPConnection{}, "", errors.New("credential is unavailable")
		}
		credentialHeader = entry.AuthHeader
		if entry.AuthType == "oauth" {
			credentialHeader = "Authorization"
			if resolveCredential {
				accessToken, err := s.remoteMCPOAuthAccessToken(ctx, workspaceID, installationID, contributionID, secretID)
				if err != nil {
					return pluginruntime.RemoteMCPConnection{}, "", errors.New("OAuth connection is expired or unavailable")
				}
				credential = "Bearer " + accessToken
			} else if _, err := s.Queries.GetActivePluginRemoteMCPSecret(ctx, db.GetActivePluginRemoteMCPSecretParams{
				ID: secretID, WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contributionID,
			}); err != nil {
				return pluginruntime.RemoteMCPConnection{}, "", errors.New("credential is revoked or outside task scope")
			}
		} else {
			secret, err := s.Queries.GetActivePluginRemoteMCPSecret(ctx, db.GetActivePluginRemoteMCPSecretParams{
				ID: secretID, WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contributionID,
			})
			if err != nil {
				return pluginruntime.RemoteMCPConnection{}, "", errors.New("credential is revoked or outside task scope")
			}
			if resolveCredential {
				opened, err := s.RemoteMCPSecrets.Open(secret.Ciphertext)
				if err != nil {
					return pluginruntime.RemoteMCPConnection{}, "", errors.New("credential cannot be decrypted")
				}
				credential = string(opened)
			}
		}
		if resolveCredential && entry.AuthType == "bearer" {
			credentialHeader = "Authorization"
			credential = "Bearer " + credential
		}
	}
	return pluginruntime.RemoteMCPConnection{
		InstallationID: entry.InstallationID, ContributionID: entry.ContributionID,
		ContributionKey: entry.ContributionKey, ConfigID: entry.ConfigID, ConfigRevision: entry.ConfigRevision,
		Endpoint: entry.Endpoint, PublicConfig: append(json.RawMessage(nil), entry.PublicConfig...),
		Transport: entry.Transport, ProtocolVersions: append([]string(nil), entry.ProtocolVersions...),
		EndpointAllowedHosts: append([]string(nil), entry.EndpointAllowedHosts...),
		CredentialHeader:     credentialHeader,
		ApprovedTools:        append([]pluginruntime.RemoteMCPTool(nil), entry.ApprovedTools...),
		ToolSchemaDigest:     entry.ToolSchemaDigest, FailurePolicy: entry.FailurePolicy,
	}, credential, nil
}

func parsePluginRuntimeUUID(value, field string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil || !id.Valid {
		return pgtype.UUID{}, fmt.Errorf("invalid %s id", field)
	}
	return id, nil
}

func (s *PluginService) loadRemoteMCPDeclaration(ctx context.Context, q *db.Queries, workspaceID, installationID pgtype.UUID, contributionKey string) (db.GetInstallationRemoteMCPContributionRow, plugincontract.RemoteMCPContribution, error) {
	row, err := q.GetInstallationRemoteMCPContribution(ctx, db.GetInstallationRemoteMCPContributionParams{
		WorkspaceID: workspaceID, InstallationID: installationID, ContributionKey: contributionKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, plugincontract.RemoteMCPContribution{}, newPluginError(PluginErrorNotFound, "Remote MCP contribution not found", nil)
	}
	if err != nil {
		return row, plugincontract.RemoteMCPContribution{}, err
	}
	var manifest plugincontract.Manifest
	if err := json.Unmarshal(row.Manifest, &manifest); err != nil {
		return row, plugincontract.RemoteMCPContribution{}, err
	}
	for _, declaration := range manifest.Contributes.RemoteMCP {
		if declaration.Key == contributionKey {
			return row, declaration, nil
		}
	}
	return row, plugincontract.RemoteMCPContribution{}, newPluginError(PluginErrorNotFound, "Remote MCP contribution not found", nil)
}

func (s *PluginService) remoteMCPHeaders(ctx context.Context, workspaceID, installationID, contributionID pgtype.UUID, authType, authHeader, plaintext string, secretRef pgtype.UUID) (http.Header, error) {
	headers := make(http.Header)
	if authType == "none" {
		return headers, nil
	}
	if authType == "oauth" {
		if !secretRef.Valid {
			return nil, newPluginError(PluginErrorConflict, "Remote MCP OAuth connection is unavailable", nil)
		}
		accessToken, err := s.remoteMCPOAuthAccessToken(ctx, workspaceID, installationID, contributionID, secretRef)
		if err != nil {
			return nil, newPluginError(PluginErrorConflict, "Remote MCP OAuth connection is unavailable", err)
		}
		headers.Set("Authorization", "Bearer "+accessToken)
		return headers, nil
	}
	if plaintext == "" {
		if s.RemoteMCPSecrets == nil || !secretRef.Valid {
			return nil, newPluginError(PluginErrorConflict, "Remote MCP credential is unavailable", nil)
		}
		secret, err := s.Queries.GetActivePluginRemoteMCPSecret(ctx, db.GetActivePluginRemoteMCPSecretParams{
			ID: secretRef, WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contributionID,
		})
		if err != nil {
			return nil, newPluginError(PluginErrorConflict, "Remote MCP credential is unavailable", err)
		}
		opened, err := s.RemoteMCPSecrets.Open(secret.Ciphertext)
		if err != nil {
			return nil, newPluginError(PluginErrorConflict, "Remote MCP credential is unavailable", err)
		}
		plaintext = string(opened)
	}
	if authType == "bearer" {
		headers.Set("Authorization", "Bearer "+plaintext)
	} else {
		headers.Set(authHeader, plaintext)
	}
	return headers, nil
}

func (s *PluginService) bumpPluginConfigurationGeneration(ctx context.Context, q *db.Queries, workspaceID, installationID, actorID pgtype.UUID) (db.PluginInstallation, error) {
	installation, err := q.GetPluginInstallation(ctx, installationID)
	if err != nil || installation.WorkspaceID != workspaceID {
		return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Plugin installation not found", nil)
	}
	return q.SetPluginInstallationDesiredState(ctx, db.SetPluginInstallationDesiredStateParams{
		Enabled: installation.Enabled, UpdatedBy: actorID, WorkspaceID: workspaceID,
		DesiredReleaseID: installation.DesiredReleaseID, ID: installationID,
	})
}

func validateRemoteMCPPublicConfig(raw, rawSchema json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	if err := rejectRemoteMCPSecretKeys(config, ""); err != nil {
		return nil, err
	}
	if len(rawSchema) > 0 {
		var schema map[string]any
		if err := json.Unmarshal(rawSchema, &schema); err != nil {
			return nil, err
		}
		if err := validateRemoteMCPConfigValue(config, schema, "configuration"); err != nil {
			return nil, err
		}
	}
	return json.Marshal(config)
}

func rejectRemoteMCPSecretKeys(value any, path string) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			if strings.Contains(normalized, "secret") || strings.Contains(normalized, "token") || strings.Contains(normalized, "password") || strings.Contains(normalized, "authorization") || strings.Contains(normalized, "api_key") {
				return fmt.Errorf("public configuration key %q may contain a secret", childPath)
			}
			if err := rejectRemoteMCPSecretKeys(child, childPath); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range typed {
			if err := rejectRemoteMCPSecretKeys(child, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateRemoteMCPConfigValue intentionally implements the safe structural
// subset accepted by the V1 manifest contract. Publishers can constrain object
// properties, required fields, arrays, primitive types, and enums; unsupported
// JSON Schema keywords remain annotations and never broaden this validation.
func validateRemoteMCPConfigValue(value any, schema map[string]any, path string) error {
	if choices, ok := schema["enum"].([]any); ok {
		matched := false
		for _, choice := range choices {
			left, _ := json.Marshal(value)
			right, _ := json.Marshal(choice)
			if string(left) == string(right) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s is outside the configured enum", path)
		}
	}
	typeName, _ := schema["type"].(string)
	switch typeName {
	case "", "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		properties, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]any)
		for _, rawName := range required {
			name, _ := rawName.(string)
			if _, exists := object[name]; name != "" && !exists {
				return fmt.Errorf("%s.%s is required", path, name)
			}
		}
		additional, hasAdditional := schema["additionalProperties"].(bool)
		for name, child := range object {
			rawChildSchema, exists := properties[name]
			if !exists {
				if hasAdditional && !additional {
					return fmt.Errorf("%s.%s is not allowed", path, name)
				}
				continue
			}
			childSchema, ok := rawChildSchema.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.%s has an invalid schema", path, name)
			}
			if err := validateRemoteMCPConfigValue(child, childSchema, path+"."+name); err != nil {
				return err
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		itemSchema, _ := schema["items"].(map[string]any)
		for index, item := range items {
			if itemSchema != nil {
				if err := validateRemoteMCPConfigValue(item, itemSchema, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("%s must be a number", path)
		}
	case "integer":
		number, ok := value.(float64)
		if !ok || number != float64(int64(number)) {
			return fmt.Errorf("%s must be an integer", path)
		}
	case "null":
		if value != nil {
			return fmt.Errorf("%s must be null", path)
		}
	default:
		return fmt.Errorf("%s uses unsupported schema type %q", path, typeName)
	}
	return nil
}

func applyDeclaredRisk(tools []pluginruntime.RemoteMCPTool, declaration plugincontract.RemoteMCPContribution) {
	risks := make(map[string]string, len(declaration.ToolIntent))
	for _, intent := range declaration.ToolIntent {
		risks[intent.Name] = intent.Risk
	}
	for index := range tools {
		risk := risks[tools[index].Name]
		if risk == "" {
			risk = risks[plugincontract.RemoteMCPAnyToolIntent]
		}
		tools[index].Risk = risk
	}
}

func selectApprovedRemoteMCPTools(discovered []pluginruntime.RemoteMCPTool, declaration plugincontract.RemoteMCPContribution, names []string) ([]pluginruntime.RemoteMCPTool, error) {
	if len(names) == 0 {
		return nil, errors.New("at least one tool must be approved")
	}
	intended := make(map[string]bool, len(declaration.ToolIntent))
	for _, tool := range declaration.ToolIntent {
		intended[tool.Name] = true
	}
	allowDiscovered := intended[plugincontract.RemoteMCPAnyToolIntent]
	available := make(map[string]pluginruntime.RemoteMCPTool, len(discovered))
	for _, tool := range discovered {
		available[tool.Name] = tool
	}
	approved := make([]pluginruntime.RemoteMCPTool, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] || (!allowDiscovered && !intended[name]) {
			return nil, fmt.Errorf("tool %q is duplicate or outside declared intent", name)
		}
		tool, ok := available[name]
		if !ok {
			return nil, fmt.Errorf("tool %q was not discovered", name)
		}
		seen[name] = true
		approved = append(approved, tool)
	}
	return approved, nil
}

func secretHint(secret string) string {
	if len(secret) <= 4 {
		return "••••"
	}
	return "••••" + secret[len(secret)-4:]
}
