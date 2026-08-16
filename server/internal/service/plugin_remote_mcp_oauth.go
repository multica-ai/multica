package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

const remoteMCPOAuthStateTTL = 10 * time.Minute
const remoteMCPOAuthRefreshTimeout = 10 * time.Second

type RemoteMCPOAuthStartInput struct {
	Endpoint                string
	PublicConfig            json.RawMessage
	FailurePolicy           string
	Scope                   string
	ClientID                string
	ClientSecret            string
	TokenEndpointAuthMethod string
	AuthorizationEndpoint   string
	TokenEndpoint           string
	RedirectURI             string
	ReturnTo                string
}

type RemoteMCPOAuthStartResult struct {
	AuthorizationURL string
}

type RemoteMCPOAuthCallbackResult struct {
	ReturnTo string
}

type remoteMCPOAuthStateSecret struct {
	Verifier                string `json:"verifier"`
	ClientSecret            string `json:"client_secret,omitempty"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
}

// remoteMCPOAuthTokenSecret is the encrypted connection record. Keeping the
// refresh metadata beside the token means neither the Plugin artifact nor the
// execution manifest ever receives reusable OAuth material.
type remoteMCPOAuthTokenSecret struct {
	AccessToken             string    `json:"access_token"`
	RefreshToken            string    `json:"refresh_token,omitempty"`
	ExpiresAt               time.Time `json:"expires_at,omitempty"`
	Scope                   string    `json:"scope,omitempty"`
	Resource                string    `json:"resource"`
	TokenEndpoint           string    `json:"token_endpoint"`
	ClientID                string    `json:"client_id"`
	ClientSecret            string    `json:"client_secret,omitempty"`
	TokenEndpointAuthMethod string    `json:"token_endpoint_auth_method"`
}

func (s *PluginService) BeginRemoteMCPOAuth(ctx context.Context, workspaceID, installationID, actorID pgtype.UUID, contributionKey string, input RemoteMCPOAuthStartInput) (RemoteMCPOAuthStartResult, error) {
	if s.RemoteMCPSecrets == nil {
		return RemoteMCPOAuthStartResult{}, newPluginError(PluginErrorIncompatible, "Remote MCP credential encryption is not configured", nil)
	}
	if strings.TrimSpace(input.RedirectURI) == "" {
		return RemoteMCPOAuthStartResult{}, newPluginError(PluginErrorIncompatible, "Remote MCP OAuth callback URL is not configured", nil)
	}
	contribution, declaration, err := s.loadRemoteMCPDeclaration(ctx, s.Queries, workspaceID, installationID, contributionKey)
	if err != nil {
		return RemoteMCPOAuthStartResult{}, err
	}
	input.Endpoint = strings.TrimSpace(input.Endpoint)
	if input.Endpoint == "" {
		input.Endpoint = declaration.EndpointPolicy.DefaultEndpoint
	}
	if _, err := remotemcp.ValidatePublicHTTPSEndpoint(ctx, input.Endpoint, declaration.EndpointPolicy.AllowedHosts, nil); err != nil {
		return RemoteMCPOAuthStartResult{}, newPluginError(PluginErrorInvalid, "Remote MCP endpoint is not allowed", err)
	}
	publicConfig, err := validateRemoteMCPPublicConfig(input.PublicConfig, declaration.ConfigurationSchema)
	if err != nil {
		return RemoteMCPOAuthStartResult{}, newPluginError(PluginErrorInvalid, "Remote MCP public configuration is invalid", err)
	}
	input.FailurePolicy = strings.TrimSpace(input.FailurePolicy)
	if input.FailurePolicy == "" {
		input.FailurePolicy = "required"
	}
	if input.FailurePolicy != "required" && input.FailurePolicy != "optional" {
		return RemoteMCPOAuthStartResult{}, newPluginError(PluginErrorInvalid, "failure_policy must be required or optional", nil)
	}

	metadata, err := remotemcp.DiscoverOAuth(ctx, input.Endpoint, declaration.EndpointPolicy.AllowedHosts)
	if err != nil {
		return RemoteMCPOAuthStartResult{}, newPluginError(PluginErrorInvalid, "Remote MCP OAuth discovery failed", err)
	}
	if strings.TrimSpace(input.AuthorizationEndpoint) != "" || strings.TrimSpace(input.TokenEndpoint) != "" {
		if input.AuthorizationEndpoint == "" || input.TokenEndpoint == "" {
			return RemoteMCPOAuthStartResult{}, newPluginError(PluginErrorInvalid, "both OAuth endpoints are required", nil)
		}
		// Advanced overrides still pass through the remotemcp package's public
		// HTTPS checks when the authorization URL is built and the token is
		// exchanged. Discovery remains mandatory so a random MCP endpoint cannot
		// turn Multica into an arbitrary OAuth client.
		metadata.AuthorizationEndpoint = strings.TrimSpace(input.AuthorizationEndpoint)
		metadata.TokenEndpoint = strings.TrimSpace(input.TokenEndpoint)
	}
	if err := remotemcp.ValidateOAuthMetadata(ctx, metadata); err != nil {
		return RemoteMCPOAuthStartResult{}, newPluginError(PluginErrorInvalid, "Remote MCP OAuth endpoints are invalid", err)
	}

	registration := remotemcp.OAuthClientRegistration{
		ClientID: strings.TrimSpace(input.ClientID), ClientSecret: input.ClientSecret,
		TokenEndpointAuthMethod: strings.TrimSpace(input.TokenEndpointAuthMethod),
	}
	if registration.ClientID == "" {
		registration, err = remotemcp.RegisterOAuthClient(ctx, metadata, input.RedirectURI)
		if err != nil {
			return RemoteMCPOAuthStartResult{}, newPluginError(PluginErrorInvalid, "Remote MCP OAuth client registration failed", err)
		}
	}
	if registration.TokenEndpointAuthMethod == "" {
		if registration.ClientSecret == "" {
			registration.TokenEndpointAuthMethod = "none"
		} else {
			registration.TokenEndpointAuthMethod = "client_secret_basic"
		}
	}
	if registration.TokenEndpointAuthMethod != "none" && registration.TokenEndpointAuthMethod != "client_secret_basic" && registration.TokenEndpointAuthMethod != "client_secret_post" {
		return RemoteMCPOAuthStartResult{}, newPluginError(PluginErrorInvalid, "unsupported OAuth token endpoint authentication method", nil)
	}

	state, err := secureRemoteMCPRandom()
	if err != nil {
		return RemoteMCPOAuthStartResult{}, err
	}
	verifier, err := secureRemoteMCPRandom()
	if err != nil {
		return RemoteMCPOAuthStartResult{}, err
	}
	scope := strings.TrimSpace(input.Scope)
	if scope == "" {
		scope = strings.Join(metadata.Scopes, " ")
	}
	stateSecret, _ := json.Marshal(remoteMCPOAuthStateSecret{
		Verifier: verifier, ClientSecret: registration.ClientSecret,
		TokenEndpointAuthMethod: registration.TokenEndpointAuthMethod,
	})
	sealed, err := s.RemoteMCPSecrets.Seal(stateSecret)
	if err != nil {
		return RemoteMCPOAuthStartResult{}, fmt.Errorf("encrypt Remote MCP OAuth state: %w", err)
	}
	stateHash := sha256.Sum256([]byte(state))
	returnTo := sanitizeRemoteMCPReturnTo(input.ReturnTo)
	_, _ = s.Queries.DeleteExpiredPluginRemoteMCPOAuthStates(ctx)
	if _, err := s.Queries.CreatePluginRemoteMCPOAuthState(ctx, db.CreatePluginRemoteMCPOAuthStateParams{
		StateHash: stateHash[:], WorkspaceID: workspaceID, InstallationID: installationID,
		ContributionID: contribution.ID, ActorID: actorID, Endpoint: metadata.ResourceEndpoint,
		PublicConfig: publicConfig, FailurePolicy: input.FailurePolicy,
		AuthorizationEndpoint: metadata.AuthorizationEndpoint, TokenEndpoint: metadata.TokenEndpoint,
		ClientID: registration.ClientID, Scope: scope, RedirectUri: input.RedirectURI,
		ReturnTo: returnTo, SecretCiphertext: sealed,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(remoteMCPOAuthStateTTL), Valid: true},
	}); err != nil {
		return RemoteMCPOAuthStartResult{}, err
	}
	authorizationURL, err := remotemcp.BuildAuthorizationURL(metadata, registration, input.RedirectURI, state, verifier, scope)
	if err != nil {
		return RemoteMCPOAuthStartResult{}, newPluginError(PluginErrorInvalid, "build Remote MCP authorization URL", err)
	}
	return RemoteMCPOAuthStartResult{AuthorizationURL: authorizationURL}, nil
}

func (s *PluginService) CompleteRemoteMCPOAuth(ctx context.Context, state, code string) (RemoteMCPOAuthCallbackResult, error) {
	if s.RemoteMCPSecrets == nil {
		return RemoteMCPOAuthCallbackResult{}, newPluginError(PluginErrorIncompatible, "Remote MCP credential encryption is not configured", nil)
	}
	if state == "" {
		return RemoteMCPOAuthCallbackResult{}, newPluginError(PluginErrorInvalid, "OAuth callback is missing state", nil)
	}
	stateHash := sha256.Sum256([]byte(state))
	claimed, err := s.Queries.ClaimPluginRemoteMCPOAuthState(ctx, stateHash[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return RemoteMCPOAuthCallbackResult{}, newPluginError(PluginErrorInvalid, "OAuth state is invalid, expired, or already used", nil)
	}
	if err != nil {
		return RemoteMCPOAuthCallbackResult{}, err
	}
	if code == "" {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, newPluginError(PluginErrorInvalid, "OAuth callback did not include an authorization code", nil)
	}
	opened, err := s.RemoteMCPSecrets.Open(claimed.SecretCiphertext)
	if err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, newPluginError(PluginErrorInvalid, "OAuth state cannot be decrypted", err)
	}
	var stateSecret remoteMCPOAuthStateSecret
	if err := json.Unmarshal(opened, &stateSecret); err != nil || stateSecret.Verifier == "" {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, newPluginError(PluginErrorInvalid, "OAuth state is invalid", err)
	}
	registration := remotemcp.OAuthClientRegistration{
		ClientID: claimed.ClientID, ClientSecret: stateSecret.ClientSecret,
		TokenEndpointAuthMethod: stateSecret.TokenEndpointAuthMethod,
	}
	token, err := remotemcp.ExchangeOAuthCode(ctx, claimed.TokenEndpoint, claimed.Endpoint, code, claimed.RedirectUri, stateSecret.Verifier, registration)
	if err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, newPluginError(PluginErrorInvalid, "Remote MCP OAuth token exchange failed", err)
	}
	if token.RefreshToken == "" {
		// A non-expiring access token is usable without a refresh token. A
		// short-lived token without one would leave a connection that appears
		// healthy but predictably dies, so fail it at connect time.
		if token.ExpiresIn > 0 {
			return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, newPluginError(PluginErrorInvalid, "OAuth server returned an expiring token without refresh capability", nil)
		}
	}
	headers := make(map[string][]string)
	headers["Authorization"] = []string{"Bearer " + token.AccessToken}
	contribution, declaration, err := s.loadRemoteMCPDeclarationByID(ctx, claimed.WorkspaceID, claimed.InstallationID, claimed.ContributionID)
	if err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, err
	}
	discovered, discoveredDigest, err := remotemcp.Discover(ctx, claimed.Endpoint, declaration.EndpointPolicy.AllowedHosts, declaration.ProtocolVersions, headers)
	if err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, newPluginError(PluginErrorInvalid, "Remote MCP connection test failed after OAuth", err)
	}
	applyDeclaredRisk(discovered, declaration)
	discoveredJSON, _ := json.Marshal(discovered)

	tokenSecret := remoteMCPOAuthTokenSecret{
		AccessToken: token.AccessToken, RefreshToken: token.RefreshToken,
		ExpiresAt: remotemcp.OAuthExpiry(time.Now(), token.ExpiresIn), Scope: token.Scope,
		Resource: claimed.Endpoint, TokenEndpoint: claimed.TokenEndpoint,
		ClientID: claimed.ClientID, ClientSecret: stateSecret.ClientSecret,
		TokenEndpointAuthMethod: stateSecret.TokenEndpointAuthMethod,
	}
	tokenJSON, _ := json.Marshal(tokenSecret)
	sealed, err := s.RemoteMCPSecrets.Seal(tokenJSON)
	if err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, fmt.Errorf("encrypt Remote MCP OAuth token: %w", err)
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.Queries.WithTx(tx)
	if _, err := q.LockPluginRemoteMCPInstallation(ctx, db.LockPluginRemoteMCPInstallationParams{
		WorkspaceID: claimed.WorkspaceID, InstallationID: claimed.InstallationID, ContributionID: contribution.ID,
	}); err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, err
	}
	if _, err := q.RevokePluginRemoteMCPSecrets(ctx, db.RevokePluginRemoteMCPSecretsParams{
		WorkspaceID: claimed.WorkspaceID, InstallationID: claimed.InstallationID, ContributionID: contribution.ID,
	}); err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, err
	}
	secret, err := q.CreatePluginRemoteMCPSecret(ctx, db.CreatePluginRemoteMCPSecretParams{
		WorkspaceID: claimed.WorkspaceID, InstallationID: claimed.InstallationID, ContributionID: contribution.ID,
		Ciphertext: sealed, Hint: "OAuth", CreatedBy: claimed.ActorID,
	})
	if err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, err
	}
	if _, err := q.CreatePluginInstallationConfig(ctx, db.CreatePluginInstallationConfigParams{
		WorkspaceID: claimed.WorkspaceID, InstallationID: claimed.InstallationID, ContributionID: contribution.ID,
		Endpoint: claimed.Endpoint, PublicConfig: claimed.PublicConfig, AuthType: "oauth", AuthHeader: "Authorization",
		SecretRef: secret.ID, DiscoveredTools: discoveredJSON,
		DiscoveredSchemaDigest: pgtype.Text{String: discoveredDigest, Valid: true},
		ApprovedTools:          []byte(`[]`), FailurePolicy: claimed.FailurePolicy, CreatedBy: claimed.ActorID,
	}); err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, err
	}
	if _, err := s.bumpPluginConfigurationGeneration(ctx, q, claimed.WorkspaceID, claimed.InstallationID, claimed.ActorID); err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, err
	}
	if _, err := s.reconcileWorkspaceTx(ctx, q, claimed.WorkspaceID); err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, err
	}
	return RemoteMCPOAuthCallbackResult{ReturnTo: claimed.ReturnTo}, nil
}

func (s *PluginService) loadRemoteMCPDeclarationByID(ctx context.Context, workspaceID, installationID, contributionID pgtype.UUID) (db.GetInstallationRemoteMCPContributionRow, plugincontract.RemoteMCPContribution, error) {
	row, err := s.Queries.GetInstallationRemoteMCPContributionByID(ctx, db.GetInstallationRemoteMCPContributionByIDParams{
		WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contributionID,
	})
	if err != nil {
		return db.GetInstallationRemoteMCPContributionRow{}, plugincontract.RemoteMCPContribution{}, err
	}
	var manifest plugincontract.Manifest
	if err := json.Unmarshal(row.Manifest, &manifest); err != nil {
		return db.GetInstallationRemoteMCPContributionRow{}, plugincontract.RemoteMCPContribution{}, err
	}
	for _, declaration := range manifest.Contributes.RemoteMCP {
		if declaration.Key == row.ContributionKey {
			// sqlc generates structurally identical row types for the key and id
			// queries, but Go treats them as distinct. Resolve once by key to keep
			// callers on the established service type.
			return s.loadRemoteMCPDeclaration(ctx, s.Queries, workspaceID, installationID, row.ContributionKey)
		}
	}
	return db.GetInstallationRemoteMCPContributionRow{}, plugincontract.RemoteMCPContribution{}, newPluginError(PluginErrorNotFound, "Remote MCP contribution not found", nil)
}

func secureRemoteMCPRandom() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func sanitizeRemoteMCPReturnTo(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\r\n") {
		return "/"
	}
	return value
}

func (s *PluginService) remoteMCPOAuthAccessToken(ctx context.Context, workspaceID, installationID, contributionID, secretID pgtype.UUID) (string, error) {
	if s.RemoteMCPSecrets == nil {
		return "", errors.New("credential encryption is not configured")
	}
	_, token, err := s.loadRemoteMCPOAuthToken(ctx, s.Queries, workspaceID, installationID, contributionID, secretID)
	if err != nil {
		return "", err
	}
	if token.ExpiresAt.IsZero() || token.ExpiresAt.After(time.Now().Add(time.Minute)) {
		return token.AccessToken, nil
	}

	key := base64.RawURLEncoding.EncodeToString(secretID.Bytes[:])
	value, err, _ := s.remoteMCPOAuthRefresh.Do(key, func() (any, error) {
		secret, current, err := s.loadRemoteMCPOAuthToken(ctx, s.Queries, workspaceID, installationID, contributionID, secretID)
		if err != nil {
			return "", err
		}
		if current.ExpiresAt.IsZero() || current.ExpiresAt.After(time.Now().Add(time.Minute)) {
			return current.AccessToken, nil
		}
		if current.RefreshToken == "" {
			return "", errors.New("OAuth access token expired without a refresh token")
		}

		refreshCtx, cancel := context.WithTimeout(ctx, remoteMCPOAuthRefreshTimeout)
		defer cancel()
		refreshed, err := remotemcp.RefreshOAuthToken(refreshCtx, current.TokenEndpoint, current.Resource, current.RefreshToken, remotemcp.OAuthClientRegistration{
			ClientID: current.ClientID, ClientSecret: current.ClientSecret, TokenEndpointAuthMethod: current.TokenEndpointAuthMethod,
		})
		if err != nil {
			return "", err
		}
		current.AccessToken = refreshed.AccessToken
		if refreshed.RefreshToken != "" {
			current.RefreshToken = refreshed.RefreshToken
		}
		if refreshed.Scope != "" {
			current.Scope = refreshed.Scope
		}
		current.ExpiresAt = remotemcp.OAuthExpiry(time.Now(), refreshed.ExpiresIn)

		// The network call intentionally happens before the write lock. Once the
		// provider responds, lock briefly to re-check revocation/rotation and
		// publish the refreshed token atomically.
		tx, err := s.TxStarter.Begin(ctx)
		if err != nil {
			return "", err
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		q := s.Queries.WithTx(tx)
		locked, err := q.GetActivePluginRemoteMCPSecretForUpdate(ctx, db.GetActivePluginRemoteMCPSecretForUpdateParams{
			ID: secretID, WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contributionID,
		})
		if err != nil {
			return "", err
		}
		lockedToken, err := s.decodeRemoteMCPOAuthToken(locked.Ciphertext)
		if err != nil {
			return "", err
		}
		if lockedToken.ExpiresAt.IsZero() || lockedToken.ExpiresAt.After(time.Now().Add(time.Minute)) {
			if err := tx.Commit(ctx); err != nil {
				return "", err
			}
			return lockedToken.AccessToken, nil
		}
		if !bytes.Equal(locked.Ciphertext, secret.Ciphertext) {
			return "", errors.New("OAuth token changed while it was being refreshed")
		}

		encoded, _ := json.Marshal(current)
		sealed, err := s.RemoteMCPSecrets.Seal(encoded)
		if err != nil {
			return "", err
		}
		if _, err := q.UpdateActivePluginRemoteMCPSecret(ctx, db.UpdateActivePluginRemoteMCPSecretParams{
			Ciphertext: sealed, Hint: "OAuth", ID: secretID, WorkspaceID: workspaceID,
			InstallationID: installationID, ContributionID: contributionID,
		}); err != nil {
			return "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return current.AccessToken, nil
	})
	if err != nil {
		return "", err
	}
	accessToken, ok := value.(string)
	if !ok || accessToken == "" {
		return "", errors.New("OAuth token refresh returned no access token")
	}
	return accessToken, nil
}

func (s *PluginService) loadRemoteMCPOAuthToken(ctx context.Context, q *db.Queries, workspaceID, installationID, contributionID, secretID pgtype.UUID) (db.PluginRemoteMcpSecret, remoteMCPOAuthTokenSecret, error) {
	secret, err := q.GetActivePluginRemoteMCPSecret(ctx, db.GetActivePluginRemoteMCPSecretParams{
		ID: secretID, WorkspaceID: workspaceID, InstallationID: installationID, ContributionID: contributionID,
	})
	if err != nil {
		return db.PluginRemoteMcpSecret{}, remoteMCPOAuthTokenSecret{}, err
	}
	token, err := s.decodeRemoteMCPOAuthToken(secret.Ciphertext)
	return secret, token, err
}

func (s *PluginService) decodeRemoteMCPOAuthToken(ciphertext []byte) (remoteMCPOAuthTokenSecret, error) {
	opened, err := s.RemoteMCPSecrets.Open(ciphertext)
	if err != nil {
		return remoteMCPOAuthTokenSecret{}, err
	}
	var token remoteMCPOAuthTokenSecret
	if err := json.Unmarshal(opened, &token); err != nil || token.AccessToken == "" {
		return remoteMCPOAuthTokenSecret{}, errors.New("OAuth token is invalid")
	}
	return token, nil
}
