package connectedapps

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	composiointeg "github.com/multica-ai/multica/server/internal/integrations/composio"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// RemoteApp is an app whose own MCP server Multica connects to as an OAuth
// client, instead of through Composio. Adding one is configuration only: the
// server must publish OAuth discovery metadata (RFC 9728 / RFC 8414) and
// accept dynamic client registration.
type RemoteApp struct {
	Slug     string
	Name     string
	Endpoint string
	Category string
}

// BuiltinRemoteApps are the remote MCP apps every deployment offers when
// remote apps are configured.
var BuiltinRemoteApps = []RemoteApp{
	{Slug: "mobbin", Name: "Mobbin", Endpoint: "https://api.mobbin.com/mcp", Category: "design"},
}

// RemoteProvider tags a remote MCP app in a run's connected-app metadata. Its
// tools are served under their own MCP server, not the apps_* meta tools.
const RemoteProvider = "multica_remote"

// RemoteCallbackPath is the public OAuth redirect URI path.
const RemoteCallbackPath = "/api/apps/oauth/callback"

const (
	remoteStateTTL     = 10 * time.Minute
	remoteRefreshSkew  = 2 * time.Minute
	remoteToolCacheTTL = 10 * time.Minute
	// Covers the token request (its HTTP client gives up at 30s) and the save.
	remoteRefreshTimeout = 40 * time.Second
)

// ErrRemoteState means the callback's state is missing, tampered with, or
// expired. The handler shows a generic failure for it.
var ErrRemoteState = errors.New("connectedapps: invalid connect state")

// TxStarter begins the transaction a token refresh runs in.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Sealer encrypts tokens at rest; *secretbox.Box implements it.
type Sealer interface {
	Seal(plaintext []byte) ([]byte, error)
	Open(sealed []byte) ([]byte, error)
}

// RemoteUpstream is the network side of remote apps. remotemcpUpstream is the
// production implementation; tests substitute a fake.
type RemoteUpstream interface {
	DiscoverOAuth(ctx context.Context, endpoint string) (remotemcp.OAuthMetadata, error)
	Register(ctx context.Context, metadata remotemcp.OAuthMetadata, redirectURI string) (remotemcp.OAuthClientRegistration, error)
	Exchange(ctx context.Context, tokenEndpoint, resource, code, redirectURI, verifier string, client remotemcp.OAuthClientRegistration) (remotemcp.OAuthTokenResponse, error)
	Refresh(ctx context.Context, tokenEndpoint, resource, refreshToken string, client remotemcp.OAuthClientRegistration) (remotemcp.OAuthTokenResponse, error)
	ListTools(ctx context.Context, endpoint, accessToken string) ([]json.RawMessage, error)
	CallTool(ctx context.Context, endpoint, accessToken, name string, args map[string]any) (json.RawMessage, error)
}

type remotemcpUpstream struct{}

func (remotemcpUpstream) DiscoverOAuth(ctx context.Context, endpoint string) (remotemcp.OAuthMetadata, error) {
	return remotemcp.DiscoverOAuth(ctx, endpoint, nil)
}

func (remotemcpUpstream) Register(ctx context.Context, metadata remotemcp.OAuthMetadata, redirectURI string) (remotemcp.OAuthClientRegistration, error) {
	return remotemcp.RegisterOAuthClient(ctx, metadata, redirectURI)
}

func (remotemcpUpstream) Exchange(ctx context.Context, tokenEndpoint, resource, code, redirectURI, verifier string, client remotemcp.OAuthClientRegistration) (remotemcp.OAuthTokenResponse, error) {
	return remotemcp.ExchangeOAuthCode(ctx, tokenEndpoint, resource, code, redirectURI, verifier, client)
}

func (remotemcpUpstream) Refresh(ctx context.Context, tokenEndpoint, resource, refreshToken string, client remotemcp.OAuthClientRegistration) (remotemcp.OAuthTokenResponse, error) {
	return remotemcp.RefreshOAuthToken(ctx, tokenEndpoint, resource, refreshToken, client)
}

func bearer(token string) http.Header {
	return http.Header{"Authorization": {"Bearer " + token}}
}

func (remotemcpUpstream) ListTools(ctx context.Context, endpoint, accessToken string) ([]json.RawMessage, error) {
	session, err := remotemcp.OpenSession(ctx, endpoint, bearer(accessToken))
	if err != nil {
		return nil, err
	}
	return session.ListTools(ctx)
}

func (remotemcpUpstream) CallTool(ctx context.Context, endpoint, accessToken, name string, args map[string]any) (json.RawMessage, error) {
	session, err := remotemcp.OpenSession(ctx, endpoint, bearer(accessToken))
	if err != nil {
		return nil, err
	}
	return session.CallTool(ctx, name, args)
}

// remoteSupport is the Service state remote apps need. It is nil when the
// deployment has no MCP apps secret key.
type remoteSupport struct {
	apps        map[string]RemoteApp
	order       []string
	box         Sealer
	db          TxStarter
	upstream    RemoteUpstream
	redirectURI string
	teamGate    composiointeg.TeamConnectGate

	mu    sync.Mutex
	tools map[string]remoteToolCache
}

type remoteToolCache struct {
	at    time.Time
	tools []RemoteTool
}

// RemoteTool is one tool an app's MCP server lists.
type RemoteTool struct {
	Name        string
	Description string
	// Tags carries the server's readOnlyHint / destructiveHint annotations
	// for Classify.
	Tags []string
	Raw  json.RawMessage
}

func newRemoteSupport(cfg Config, publicBase string) *remoteSupport {
	if cfg.SecretBox == nil || cfg.DB == nil {
		return nil
	}
	apps := cfg.RemoteApps
	if apps == nil {
		apps = BuiltinRemoteApps
	}
	upstream := cfg.RemoteUpstream
	if upstream == nil {
		upstream = remotemcpUpstream{}
	}
	r := &remoteSupport{
		apps:        map[string]RemoteApp{},
		box:         cfg.SecretBox,
		db:          cfg.DB,
		upstream:    upstream,
		redirectURI: publicBase + RemoteCallbackPath,
		tools:       map[string]remoteToolCache{},
	}
	for _, app := range apps {
		slug := NormalizeSlug(app.Slug)
		app.Slug = slug
		r.apps[slug] = app
		r.order = append(r.order, slug)
	}
	return r
}

// RemoteApp returns the remote app for slug when remote apps are enabled.
func (s *Service) RemoteApp(slug string) (RemoteApp, bool) {
	if s.remote == nil {
		return RemoteApp{}, false
	}
	app, ok := s.remote.apps[NormalizeSlug(slug)]
	return app, ok
}

// RemoteApps lists the enabled remote apps in catalog order.
func (s *Service) RemoteApps() []RemoteApp {
	if s.remote == nil {
		return nil
	}
	out := make([]RemoteApp, 0, len(s.remote.order))
	for _, slug := range s.remote.order {
		out = append(out, s.remote.apps[slug])
	}
	return out
}

// SetTeamConnectGate installs the plan check a new team account must pass,
// the same one Composio team accounts use. Call it before serving requests.
func (s *Service) SetTeamConnectGate(gate composiointeg.TeamConnectGate) {
	if s.remote != nil {
		s.remote.teamGate = gate
	}
}

// remoteState is the connect handshake carried through the browser. It is
// sealed, not just signed: it holds the PKCE verifier.
type remoteState struct {
	App      string `json:"a"`
	Scope    string `json:"s"`
	Owner    string `json:"o"`
	Actor    string `json:"u"`
	Client   string `json:"c"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r,omitempty"`
	// Workspace is the workspace the account belongs to.
	Workspace string `json:"w,omitempty"`
	Exp       int64  `json:"e"`
}

// RemoteConnectResult is what a completed (or failed) handshake was for.
type RemoteConnectResult struct {
	Slug      string
	Scope     string
	Owner     pgtype.UUID
	Actor     pgtype.UUID
	Workspace pgtype.UUID
	ReturnTo  string
}

// BeginRemoteConnect returns the authorization URL that connects an account
// for a remote app in workspaceID. owner is the workspace for a team account
// and the actor for a personal one; the caller has already checked the actor
// may do this.
func (s *Service) BeginRemoteConnect(ctx context.Context, slug, scope string, workspaceID, owner, actor pgtype.UUID, returnTo string) (string, error) {
	app, ok := s.RemoteApp(slug)
	if !ok {
		return "", composiointeg.ErrToolkitNotSupported
	}
	if !ValidScope(scope) || !owner.Valid || !actor.Valid || !workspaceID.Valid {
		return "", errors.New("connectedapps: invalid remote connect owner")
	}
	if !composiointeg.SafeReturnPath(returnTo) {
		returnTo = ""
	}
	client, err := s.remoteOAuthClient(ctx, app)
	if err != nil {
		return "", err
	}
	verifier, err := randomToken()
	if err != nil {
		return "", err
	}
	state, err := s.sealState(remoteState{
		App: app.Slug, Scope: scope, Owner: util.UUIDToString(owner), Actor: util.UUIDToString(actor),
		Client: util.UUIDToString(client.ID), Verifier: verifier, ReturnTo: returnTo,
		Workspace: util.UUIDToString(workspaceID), Exp: s.now().Add(remoteStateTTL).Unix(),
	})
	if err != nil {
		return "", err
	}
	return remotemcp.BuildAuthorizationURL(
		remotemcp.OAuthMetadata{ResourceEndpoint: app.Endpoint, AuthorizationEndpoint: client.AuthorizationEndpoint},
		remotemcp.OAuthClientRegistration{ClientID: client.ClientID},
		s.remote.redirectURI, state, verifier, client.Scope,
	)
}

// CompleteRemoteConnect finishes the handshake the authorization server
// redirected back with, storing the account's tokens. The result names the
// app and owner whenever the state was valid, so a failure can still be shown
// on the right page.
func (s *Service) CompleteRemoteConnect(ctx context.Context, rawState, code, oauthError string) (RemoteConnectResult, error) {
	if s.remote == nil {
		return RemoteConnectResult{}, ErrRemoteState
	}
	st, err := s.openState(rawState)
	if err != nil {
		return RemoteConnectResult{}, err
	}
	owner, err1 := util.ParseUUID(st.Owner)
	actor, err2 := util.ParseUUID(st.Actor)
	clientID, err3 := util.ParseUUID(st.Client)
	app, ok := s.RemoteApp(st.App)
	if err1 != nil || err2 != nil || err3 != nil || !ok || !ValidScope(st.Scope) {
		return RemoteConnectResult{}, ErrRemoteState
	}
	workspace := owner
	if st.Scope != ScopeTeam {
		var err4 error
		if workspace, err4 = util.ParseUUID(st.Workspace); err4 != nil {
			return RemoteConnectResult{}, ErrRemoteState
		}
	}
	result := RemoteConnectResult{Slug: app.Slug, Scope: st.Scope, Owner: owner, Actor: actor, Workspace: workspace, ReturnTo: st.ReturnTo}
	if oauthError != "" || code == "" {
		return result, fmt.Errorf("connectedapps: authorization not granted")
	}
	client, err := s.q.GetRemoteAppOAuthClientByID(ctx, clientID)
	if err != nil {
		return result, fmt.Errorf("connectedapps: load oauth client: %w", err)
	}
	registration, err := s.registration(client)
	if err != nil {
		return result, err
	}
	tokens, err := s.remote.upstream.Exchange(ctx, client.TokenEndpoint, app.Endpoint, code, s.remote.redirectURI, st.Verifier, registration)
	if err != nil {
		var oauthErr *remotemcp.OAuthError
		if errors.As(err, &oauthErr) && oauthErr.Code == "invalid_client" {
			// The server forgot this client; register again on the next try.
			_ = s.q.DeleteRemoteAppOAuthClient(ctx, client.ID)
		}
		return result, fmt.Errorf("connectedapps: exchange code: %w", err)
	}
	admitted := func(context.Context) {}
	if st.Scope == ScopeTeam && s.remote.teamGate != nil {
		gateAdmitted, err := s.remote.teamGate(ctx, owner, app.Slug)
		if err != nil {
			return result, err
		}
		admitted = gateAdmitted
	}
	access, err := s.seal(tokens.AccessToken)
	if err != nil {
		return result, err
	}
	refresh, err := s.seal(tokens.RefreshToken)
	if err != nil {
		return result, err
	}
	row, err := s.q.UpsertRemoteAppConnection(ctx, db.UpsertRemoteAppConnectionParams{
		AppSlug: app.Slug, Scope: st.Scope, OwnerID: owner, ConnectedBy: actor, OauthClientID: client.ID,
		AccessToken: access, RefreshToken: refresh, ExpiresAt: expiry(s.now(), tokens.ExpiresIn),
		WorkspaceID: workspace,
	})
	if err != nil {
		return result, fmt.Errorf("connectedapps: save remote account: %w", err)
	}
	s.dropRemoteTools(row.ID)
	admitted(ctx)
	return result, nil
}

// DisconnectRemote forgets a remote account's tokens and marks it revoked.
// It is owner-scoped and idempotent; an unknown id is ErrConnectionNotFound.
func (s *Service) DisconnectRemote(ctx context.Context, id pgtype.UUID, scope string, owner pgtype.UUID) (db.RemoteAppConnection, error) {
	if s.remote == nil {
		return db.RemoteAppConnection{}, composiointeg.ErrConnectionNotFound
	}
	row, err := s.q.GetRemoteAppConnectionForOwner(ctx, db.GetRemoteAppConnectionForOwnerParams{ID: id, Scope: scope, OwnerID: owner})
	if err != nil {
		return db.RemoteAppConnection{}, composiointeg.ErrConnectionNotFound
	}
	if row.Status == "revoked" {
		return row, nil
	}
	if err := s.q.SetRemoteAppConnectionStatus(ctx, db.SetRemoteAppConnectionStatusParams{ID: row.ID, Status: "revoked"}); err != nil {
		return row, fmt.Errorf("connectedapps: revoke remote account: %w", err)
	}
	s.dropRemoteTools(row.ID)
	row.Status = "revoked"
	return row, nil
}

// remoteOAuthClient returns this deployment's client for app, registering one
// the first time.
func (s *Service) remoteOAuthClient(ctx context.Context, app RemoteApp) (db.RemoteAppOauthClient, error) {
	key := db.GetRemoteAppOAuthClientParams{AppSlug: app.Slug, RedirectUri: s.remote.redirectURI}
	row, err := s.q.GetRemoteAppOAuthClient(ctx, key)
	if err == nil {
		return row, nil
	}
	if !isNoRows(err) {
		return row, err
	}
	metadata, err := s.remote.upstream.DiscoverOAuth(ctx, app.Endpoint)
	if err != nil {
		return row, fmt.Errorf("connectedapps: discover oauth: %w", err)
	}
	registration, err := s.remote.upstream.Register(ctx, metadata, s.remote.redirectURI)
	if err != nil {
		return row, fmt.Errorf("connectedapps: register oauth client: %w", err)
	}
	secret, err := s.seal(registration.ClientSecret)
	if err != nil {
		return row, err
	}
	row, err = s.q.InsertRemoteAppOAuthClient(ctx, db.InsertRemoteAppOAuthClientParams{
		AppSlug: app.Slug, RedirectUri: s.remote.redirectURI,
		AuthorizationEndpoint: metadata.AuthorizationEndpoint, TokenEndpoint: metadata.TokenEndpoint,
		Scope: strings.Join(metadata.Scopes, " "), ClientID: registration.ClientID, ClientSecret: secret,
		TokenEndpointAuthMethod: registration.TokenEndpointAuthMethod,
	})
	if isNoRows(err) {
		return s.q.GetRemoteAppOAuthClient(ctx, key)
	}
	return row, err
}

func (s *Service) registration(client db.RemoteAppOauthClient) (remotemcp.OAuthClientRegistration, error) {
	secret, err := s.open(client.ClientSecret)
	if err != nil {
		return remotemcp.OAuthClientRegistration{}, fmt.Errorf("connectedapps: open client secret: %w", err)
	}
	return remotemcp.OAuthClientRegistration{
		ClientID: client.ClientID, ClientSecret: secret, TokenEndpointAuthMethod: client.TokenEndpointAuthMethod,
	}, nil
}

// remoteAccessToken returns a usable access token for an account, refreshing
// it when it is about to expire. stale is a token the app just rejected;
// passing it forces a refresh unless another call already replaced it. A
// refresh the server refuses marks the account expired and returns
// ErrAccountMissing, so the agent asks for a new connection.
func (s *Service) remoteAccessToken(ctx context.Context, id pgtype.UUID, stale string) (string, error) {
	row, err := s.q.GetRemoteAppConnection(ctx, id)
	if err != nil || row.Status != "active" {
		return "", ErrAccountMissing
	}
	if stale == "" && s.tokenFresh(row) {
		if token, err := s.open(row.AccessToken); err == nil && token != "" {
			return token, nil
		}
	}
	return s.refreshRemote(ctx, id, stale)
}

func (s *Service) tokenFresh(row db.RemoteAppConnection) bool {
	return len(row.AccessToken) > 0 && (!row.ExpiresAt.Valid || row.ExpiresAt.Time.After(s.now().Add(remoteRefreshSkew)))
}

func (s *Service) refreshRemote(ctx context.Context, id pgtype.UUID, stale string) (string, error) {
	tx, err := s.remote.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	row, err := qtx.LockRemoteAppConnection(ctx, id)
	if err != nil || row.Status != "active" {
		return "", ErrAccountMissing
	}
	if current, err := s.open(row.AccessToken); err == nil && current != "" && current != stale && s.tokenFresh(row) {
		return current, tx.Commit(ctx)
	}
	expire := func(reason string) (string, error) {
		slog.InfoContext(ctx, "connectedapps: remote account sign-in expired",
			"connection_id", util.UUIDToString(row.ID), "app", row.AppSlug, "scope", row.Scope, "reason", reason)
		if err := qtx.SetRemoteAppConnectionStatus(ctx, db.SetRemoteAppConnectionStatusParams{ID: row.ID, Status: "expired"}); err != nil {
			return "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		s.dropRemoteTools(row.ID)
		return "", ErrAccountMissing
	}
	refresh, err := s.open(row.RefreshToken)
	if err != nil || refresh == "" {
		return expire("no_refresh_token")
	}
	client, err := qtx.GetRemoteAppOAuthClientByID(ctx, row.OauthClientID)
	if err != nil {
		return expire("client_missing")
	}
	app, ok := s.RemoteApp(row.AppSlug)
	if !ok {
		return "", ErrAccountMissing
	}
	registration, err := s.registration(client)
	if err != nil {
		return "", err
	}
	// Refresh tokens are single-use: once the app has rotated it, the new pair
	// must be saved even if the caller has gone away.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), remoteRefreshTimeout)
	defer cancel()
	tokens, err := s.remote.upstream.Refresh(saveCtx, client.TokenEndpoint, app.Endpoint, refresh, registration)
	if err != nil {
		var oauthErr *remotemcp.OAuthError
		if errors.As(err, &oauthErr) && oauthErr.Rejected() {
			if oauthErr.Code == "invalid_client" {
				// Reconnecting would send the user to an authorize page that
				// rejects this client and never calls back; register anew.
				if err := qtx.DeleteRemoteAppOAuthClient(ctx, client.ID); err != nil {
					return "", err
				}
			}
			return expire("refresh_rejected")
		}
		return "", fmt.Errorf("connectedapps: refresh token: %w", err)
	}
	access, err := s.seal(tokens.AccessToken)
	if err != nil {
		return "", err
	}
	nextRefresh := row.RefreshToken
	if tokens.RefreshToken != "" {
		if nextRefresh, err = s.seal(tokens.RefreshToken); err != nil {
			return "", err
		}
	}
	if err := qtx.UpdateRemoteAppConnectionTokens(saveCtx, db.UpdateRemoteAppConnectionTokensParams{
		ID: row.ID, AccessToken: access, RefreshToken: nextRefresh, ExpiresAt: expiry(s.now(), tokens.ExpiresIn),
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(saveCtx); err != nil {
		return "", err
	}
	return tokens.AccessToken, nil
}

// withRemoteToken runs fn with the account's access token. When the app
// rejects the token, it refreshes once and retries; a second rejection marks
// the account expired.
func (s *Service) withRemoteToken(ctx context.Context, acct account, fn func(token string) error) error {
	token, err := s.remoteAccessToken(ctx, acct.connectionID, "")
	if err != nil {
		return err
	}
	err = fn(token)
	if !unauthorized(err) {
		return err
	}
	if token, err = s.remoteAccessToken(ctx, acct.connectionID, token); err != nil {
		return err
	}
	err = fn(token)
	if unauthorized(err) {
		if setErr := s.q.SetRemoteAppConnectionStatus(ctx, db.SetRemoteAppConnectionStatusParams{ID: acct.connectionID, Status: "expired"}); setErr != nil {
			return setErr
		}
		s.dropRemoteTools(acct.connectionID)
		return ErrAccountMissing
	}
	return err
}

func unauthorized(err error) bool {
	var status *remotemcp.StatusError
	return errors.As(err, &status) && status.Code == http.StatusUnauthorized
}

// remoteTools lists an app's tools as the account sees them, cached briefly
// per account.
func (s *Service) remoteTools(ctx context.Context, app RemoteApp, acct account) ([]RemoteTool, error) {
	key := util.UUIDToString(acct.connectionID)
	s.remote.mu.Lock()
	cached, ok := s.remote.tools[key]
	s.remote.mu.Unlock()
	if ok && s.now().Sub(cached.at) < remoteToolCacheTTL {
		return cached.tools, nil
	}
	var raws []json.RawMessage
	err := s.withRemoteToken(ctx, acct, func(token string) error {
		var err error
		raws, err = s.remote.upstream.ListTools(ctx, app.Endpoint, token)
		return err
	})
	if err != nil {
		return nil, err
	}
	tools := make([]RemoteTool, 0, len(raws))
	for _, raw := range raws {
		var t struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Annotations struct {
				ReadOnlyHint    *bool `json:"readOnlyHint"`
				DestructiveHint *bool `json:"destructiveHint"`
			} `json:"annotations"`
		}
		if json.Unmarshal(raw, &t) != nil || strings.TrimSpace(t.Name) == "" {
			continue
		}
		tool := RemoteTool{Name: t.Name, Description: t.Description, Raw: raw}
		if t.Annotations.ReadOnlyHint != nil && *t.Annotations.ReadOnlyHint {
			tool.Tags = append(tool.Tags, "readOnlyHint")
		} else if t.Annotations.DestructiveHint != nil && *t.Annotations.DestructiveHint {
			tool.Tags = append(tool.Tags, "destructiveHint")
		}
		tools = append(tools, tool)
	}
	s.remote.mu.Lock()
	s.remote.tools[key] = remoteToolCache{at: s.now(), tools: tools}
	s.remote.mu.Unlock()
	return tools, nil
}

func (s *Service) dropRemoteTools(id pgtype.UUID) {
	if s.remote == nil {
		return
	}
	s.remote.mu.Lock()
	delete(s.remote.tools, util.UUIDToString(id))
	s.remote.mu.Unlock()
}

// ListRemoteTools lists a remote app's tools for the settings page, through
// the workspace's team account or else the viewer's own. ErrAccountMissing
// means neither is connected, so there is nothing to list yet.
func (s *Service) ListRemoteTools(ctx context.Context, workspaceID, viewer pgtype.UUID, slug string) ([]RemoteTool, error) {
	app, ok := s.RemoteApp(slug)
	if !ok {
		return nil, composiointeg.ErrToolkitNotSupported
	}
	acct, err := s.resolveRemoteAccount(ctx, workspaceID, viewer, app.Slug, ScopeTeam)
	if err != nil {
		acct, err = s.resolveRemoteAccount(ctx, workspaceID, viewer, app.Slug, ScopePersonal)
	}
	if err != nil {
		return nil, err
	}
	return s.remoteTools(ctx, app, acct)
}

func (s *Service) resolveRemoteAccount(ctx context.Context, workspaceID, triggeredBy pgtype.UUID, slug, scope string) (account, error) {
	owner := workspaceID
	if scope != ScopeTeam {
		if !triggeredBy.Valid {
			return account{}, ErrAccountMissing
		}
		owner = triggeredBy
	}
	row, err := s.q.GetActiveRemoteAppConnection(ctx, db.GetActiveRemoteAppConnectionParams{AppSlug: slug, Scope: scope, OwnerID: owner, WorkspaceID: workspaceID})
	if isNoRows(err) {
		return account{}, ErrAccountMissing
	}
	if err != nil {
		return account{}, err
	}
	acct := account{scope: scope, connectionID: row.ID, remote: true}
	if scope != ScopeTeam {
		acct.owner = triggeredBy
	}
	return acct, nil
}

// classifyRemote is classify for a remote tool: the workspace's correction,
// then the curated pin, the tool name's verb, and the server's annotations.
func (s *Service) classifyRemote(ctx context.Context, workspaceID pgtype.UUID, slug string, tool RemoteTool) string {
	class, err := s.q.GetAppToolClassOverride(ctx, db.GetAppToolClassOverrideParams{
		WorkspaceID: workspaceID, ToolkitSlug: NormalizeSlug(slug), ToolSlug: strings.ToUpper(tool.Name),
	})
	if err == nil {
		return class
	}
	return Classify(slug, tool.Name, tool.Tags)
}

func (s *Service) seal(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	return s.remote.box.Seal([]byte(value))
}

func (s *Service) open(sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	plain, err := s.remote.box.Open(sealed)
	return string(plain), err
}

func (s *Service) sealState(st remoteState) (string, error) {
	raw, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	sealed, err := s.remote.box.Seal(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Service) openState(raw string) (remoteState, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return remoteState{}, ErrRemoteState
	}
	plain, err := s.remote.box.Open(sealed)
	if err != nil {
		return remoteState{}, ErrRemoteState
	}
	var st remoteState
	if json.Unmarshal(plain, &st) != nil || s.now().Unix() > st.Exp {
		return remoteState{}, ErrRemoteState
	}
	return st, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func expiry(now time.Time, expiresIn int64) pgtype.Timestamptz {
	at := remotemcp.OAuthExpiry(now, expiresIn)
	return pgtype.Timestamptz{Time: at, Valid: !at.IsZero()}
}
