// Package composio is the Stage 2 business-integration glue between Multica and
// the standalone Composio SDK (server/pkg/composio). It owns Multica semantics:
// the signed-state connect handshake, the local user_composio_connection
// mirror, idempotent disconnect, and the per-user MCP session helper.
//
// It deliberately does NOT wrap the SDK in another HTTP client — it composes
// *sdk.Client directly through the SDK interface so tests can drop in a fake.
//
// MVP scope (MUL-3720): toolkits are discovered dynamically. The
// toolkit→auth-config mapping is resolved at request time from Composio's
// /auth_configs endpoint (cached briefly), so a toolkit becomes connectable the
// moment an auth config is enabled for it in the Composio dashboard — no env
// var and no redeploy. A toolkit with no enabled auth config is rejected.
package composio

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	sdk "github.com/multica-ai/multica/server/pkg/composio"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Service-level errors surfaced to the handler layer.
var (
	// ErrToolkitNotSupported is returned by BeginConnect when the requested
	// toolkit has no enabled auth config in the Composio project, so there is
	// no auth_config_id to start a connect link with.
	ErrToolkitNotSupported = errors.New("composio: toolkit not supported")
	// ErrConnectNotSuccessful is returned by CompleteCallback when Composio
	// reported a non-success status — no active row is written.
	ErrConnectNotSuccessful = errors.New("composio: connection was not successful")
	// ErrConnectionNotFound is returned by Disconnect when the connection id
	// does not belong to the user (or does not exist).
	ErrConnectionNotFound = errors.New("composio: connection not found")
	// ErrAccountVerification is returned by CompleteCallback when the
	// connected_account_id carried on the callback cannot be confirmed (with
	// Composio) to belong to the user/auth-config named in the signed state —
	// i.e. a tampered or unknown account id. No local row is written.
	ErrAccountVerification = errors.New("composio: connected account verification failed")
)

// defaultStateTTL bounds how long a connect handshake may sit between
// BeginConnect and the Composio callback. Five minutes is generous for a hosted
// OAuth flow while keeping the replay window small.
const defaultStateTTL = 5 * time.Minute

// defaultAuthCacheTTL bounds how long the resolved toolkit→auth-config map is
// cached before a re-fetch from Composio. Short enough that enabling an auth
// config in the dashboard reflects within minutes; long enough that a burst of
// connect/list requests does not hammer /auth_configs.
const defaultAuthCacheTTL = 5 * time.Minute

// maxAuthConfigPages / maxToolkitPages cap the paginated fetch-all loops so a
// pathological or buggy upstream cursor cannot spin forever. At limit=1000 per
// page these cover far more than any real project / catalog.
const (
	maxAuthConfigPages  = 20
	maxToolkitPages     = 20
	listPageLimit       = 1000
	composioLogoBaseURL = "https://logos.composio.dev/api"
)

// SDK is the subset of *sdk.Client the service depends on. Declared as an
// interface so handler/service tests can inject a fake without hitting Composio.
// *sdk.Client satisfies it.
type SDK interface {
	CreateLink(ctx context.Context, req sdk.CreateLinkRequest) (*sdk.CreateLinkResponse, error)
	ListConnectedAccounts(ctx context.Context, req sdk.ListConnectedAccountsRequest) (*sdk.ListConnectedAccountsResponse, error)
	ListAuthConfigs(ctx context.Context, req sdk.ListAuthConfigsRequest) (*sdk.ListAuthConfigsResponse, error)
	ListToolkits(ctx context.Context, req sdk.ListToolkitsRequest) (*sdk.ListToolkitsResponse, error)
	RevokeConnection(ctx context.Context, connectedAccountID string) error
	DeleteConnectedAccount(ctx context.Context, connectedAccountID string) error
	CreateSession(ctx context.Context, req sdk.CreateSessionRequest) (*sdk.CreateSessionResponse, error)
	MCPAuthHeaders() map[string]string
}

// Store is the persistence seam for the local connection mirror. *db.Queries
// satisfies it; tests use an in-memory fake.
type Store interface {
	UpsertUserComposioConnection(ctx context.Context, arg db.UpsertUserComposioConnectionParams) (db.UserComposioConnection, error)
	ListActiveUserComposioConnections(ctx context.Context, userID pgtype.UUID) ([]db.UserComposioConnection, error)
	GetUserComposioConnection(ctx context.Context, arg db.GetUserComposioConnectionParams) (db.UserComposioConnection, error)
	MarkUserComposioConnectionRevoked(ctx context.Context, arg db.MarkUserComposioConnectionRevokedParams) error
}

// Config configures a Service.
type Config struct {
	// StateSecret signs the connect-state HMAC. Required (non-empty).
	StateSecret []byte
	// CallbackBaseURL is the absolute, public base URL of THIS API, with no
	// trailing slash (e.g. "https://app.multica.ai"). The Composio callback
	// URL is built as CallbackBaseURL + CallbackPath. Required.
	CallbackBaseURL string
	// FrontendBaseURL is the web app base used to build the post-callback
	// browser redirect (e.g. "https://app.multica.ai"). May be empty, in which
	// case CallbackRedirect returns a site-relative path.
	FrontendBaseURL string
	// StateTTL overrides the default connect-state lifetime. Zero uses
	// defaultStateTTL.
	StateTTL time.Duration
	// AuthConfigTTL overrides how long the resolved toolkit→auth-config map is
	// cached. Zero uses defaultAuthCacheTTL.
	AuthConfigTTL time.Duration
	// Now is overridable for deterministic tests. Nil uses time.Now.
	Now func() time.Time
}

// callbackPath is the API path Composio redirects the browser back to. It is a
// constant (not configurable) so the SDK callback URL and the router route
// cannot drift apart.
const callbackPath = "/api/integrations/composio/callback"

// Service is the Composio business-integration service.
type Service struct {
	sdk         SDK
	store       Store
	secret      []byte
	callbackURL string
	frontendURL string
	stateTTL    time.Duration
	now         func() time.Time

	// authCache holds the resolved toolkit_slug → auth_config_id map for the
	// project. It is rebuilt from Composio's /auth_configs endpoint on first
	// use and whenever authCacheExp has passed; authCacheMu guards both fields.
	authCacheMu  sync.Mutex
	authCache    map[string]string
	authCacheExp time.Time
	authCacheTTL time.Duration
}

// NewService validates its inputs and returns a ready Service. It errors when a
// required dependency is missing so a misconfigured boot fails loudly instead
// of returning 500s at request time.
func NewService(client SDK, store Store, cfg Config) (*Service, error) {
	if client == nil {
		return nil, errors.New("composio: SDK client is required")
	}
	if store == nil {
		return nil, errors.New("composio: store is required")
	}
	if len(cfg.StateSecret) == 0 {
		return nil, errors.New("composio: StateSecret is required")
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.CallbackBaseURL), "/")
	if base == "" {
		return nil, errors.New("composio: CallbackBaseURL is required")
	}

	ttl := cfg.StateTTL
	if ttl <= 0 {
		ttl = defaultStateTTL
	}
	authTTL := cfg.AuthConfigTTL
	if authTTL <= 0 {
		authTTL = defaultAuthCacheTTL
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Service{
		sdk:          client,
		store:        store,
		secret:       cfg.StateSecret,
		callbackURL:  base + callbackPath,
		frontendURL:  strings.TrimRight(strings.TrimSpace(cfg.FrontendBaseURL), "/"),
		stateTTL:     ttl,
		now:          now,
		authCacheTTL: authTTL,
	}, nil
}

// Connection is the API-facing view of a local connection row. The Composio
// connected_account_id and auth_config_id are intentionally omitted — they are
// server-internal handles, not API surface.
type Connection struct {
	ID          string  `json:"id"`
	ToolkitSlug string  `json:"toolkit_slug"`
	Status      string  `json:"status"`
	ConnectedAt string  `json:"connected_at"`
	LastUsedAt  *string `json:"last_used_at"`
}

// MCPSession is the result of CreateMCPSession: the streamable MCP URL plus the
// headers an MCP client must attach. Headers carry the Composio x-api-key, so
// callers must route them through the redact pipeline before logging.
type MCPSession struct {
	URL     string
	Headers map[string]string
}

// ToolkitView is the API-facing descriptor for one Composio toolkit, carrying
// exactly the fields the Settings UI renders plus a Connectable flag.
//
// Connectable means the project has an enabled auth config for the toolkit, so
// BeginConnect would succeed. Since MUL-4009 ListToolkits only returns
// connectable toolkits, so this is always true on the wire; the field is
// retained for backward compatibility with older desktop clients that branch on
// it (removing it would make them treat every entry as non-connectable).
type ToolkitView struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	LogoURL     string `json:"logo,omitempty"`
	Category    string `json:"category,omitempty"`
	Connectable bool   `json:"connectable"`
}

func toolkitLogoURL(slug, upstreamLogoURL string) string {
	if upstreamLogoURL != "" {
		return upstreamLogoURL
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return ""
	}
	return composioLogoBaseURL + "/" + url.PathEscape(slug)
}

// BeginConnect validates the toolkit, mints a signed state, and asks Composio
// for a hosted Connect Link. The returned redirect URL is where the caller
// sends the user's browser.
//
// The auth_config_id is resolved dynamically from the project's enabled auth
// configs (cached), so a toolkit is connectable iff the dashboard has an auth
// config for it — no static env map. A toolkit with none yields
// ErrToolkitNotSupported.
//
// The composio_user_id sent to Composio is the Multica user id verbatim — the
// invariant the rest of the integration relies on.
func (s *Service) BeginConnect(ctx context.Context, userID pgtype.UUID, toolkitSlug string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(toolkitSlug))
	authConfigID, err := s.authConfigForToolkit(ctx, slug)
	if err != nil {
		return "", err
	}
	if authConfigID == "" {
		return "", ErrToolkitNotSupported
	}
	if !userID.Valid {
		return "", errors.New("composio: invalid user id")
	}
	composioUserID := util.UUIDToString(userID)

	state, err := signState(s.secret, stateClaims{
		UserID:       composioUserID,
		ToolkitSlug:  slug,
		AuthConfigID: authConfigID,
		Exp:          s.now().Add(s.stateTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("composio: sign state: %w", err)
	}

	// Composio appends its own status / connected_account_id query params to
	// the callback URL and preserves ours, so the signed state rides back to us
	// on the redirect.
	callbackURL := s.callbackURL + "?state=" + url.QueryEscape(state)

	resp, err := s.sdk.CreateLink(ctx, sdk.CreateLinkRequest{
		AuthConfigID: authConfigID,
		UserID:       composioUserID,
		CallbackURL:  callbackURL,
	})
	if err != nil {
		return "", fmt.Errorf("composio: create link: %w", err)
	}
	return resp.RedirectURL, nil
}

// CompleteCallback verifies the signed state and, on a successful Composio
// status, upserts the local connection row. It returns the toolkit slug from
// the state so the handler can build the right redirect even on the
// not-successful path.
//
// Idempotency: the upsert is keyed on (user_id, connected_account_id), so a
// duplicate callback re-activates the same row instead of creating a second.
func (s *Service) CompleteCallback(ctx context.Context, state, status, connectedAccountID string) (string, error) {
	claims, err := verifyState(s.secret, state, s.now())
	if err != nil {
		return "", err
	}

	if !strings.EqualFold(strings.TrimSpace(status), "success") {
		// Honor the state for the redirect slug, but do not write an active row.
		return claims.ToolkitSlug, ErrConnectNotSuccessful
	}
	if strings.TrimSpace(connectedAccountID) == "" {
		return claims.ToolkitSlug, errors.New("composio: callback missing connected_account_id")
	}

	userID, err := util.ParseUUID(claims.UserID)
	if err != nil {
		return claims.ToolkitSlug, fmt.Errorf("composio: state has invalid user id: %w", err)
	}

	// The auth_config_id was resolved at BeginConnect and signed into the state,
	// so we compare against THAT exact value rather than re-resolving here (a
	// re-resolve that failed or drifted would otherwise fail-open: a missing
	// expected auth config used to skip the check, letting another toolkit's
	// account id be bound under this toolkit's slug). An empty value fails
	// closed in verifyAccountOwnership.
	authConfigID := claims.AuthConfigID

	// Defense-in-depth (PR 4608 review): the signed state proves *who* started
	// the handshake and *which* toolkit, but connected_account_id rides back as
	// a plain query param Composio appends to our callback URL. A crafted
	// redirect could pair a valid, un-expired state with someone else's account
	// id and we would mirror it verbatim. Before writing, confirm with Composio
	// that this account actually belongs to the state's user (the
	// composio_user_id == multica user id invariant) and was created under the
	// toolkit's auth config. Any mismatch fails closed with ErrAccountVerification.
	if err := s.verifyAccountOwnership(ctx, connectedAccountID, claims.UserID, authConfigID); err != nil {
		return claims.ToolkitSlug, err
	}

	if _, err := s.store.UpsertUserComposioConnection(ctx, db.UpsertUserComposioConnectionParams{
		UserID:             userID,
		ToolkitSlug:        claims.ToolkitSlug,
		AuthConfigID:       authConfigID,
		ConnectedAccountID: connectedAccountID,
		// Invariant: composio_user_id == Multica user id.
		ComposioUserID: claims.UserID,
	}); err != nil {
		return claims.ToolkitSlug, fmt.Errorf("composio: upsert connection: %w", err)
	}
	return claims.ToolkitSlug, nil
}

// ListConnections returns the user's active connections.
func (s *Service) ListConnections(ctx context.Context, userID pgtype.UUID) ([]Connection, error) {
	rows, err := s.store.ListActiveUserComposioConnections(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Connection, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowToConnection(row))
	}
	return out, nil
}

// Disconnect revokes and deletes the connection at Composio, then marks the
// local row revoked. It is idempotent: a Composio 404 (already gone) is treated
// as success, and re-revoking an already-revoked local row is a no-op.
//
// A connection id that does not belong to the user (or does not exist at all)
// returns ErrConnectionNotFound so the handler can 404 without leaking
// existence across users.
func (s *Service) Disconnect(ctx context.Context, userID, connectionID pgtype.UUID) error {
	row, err := s.store.GetUserComposioConnection(ctx, db.GetUserComposioConnectionParams{
		ID:     connectionID,
		UserID: userID,
	})
	if err != nil {
		// pgx.ErrNoRows or fake "not found" — treat as not found for the owner.
		return ErrConnectionNotFound
	}

	// Already disconnected locally: a repeat DELETE is a pure no-op. Short-
	// circuiting here keeps disconnect idempotent even when the upstream now
	// answers revoke/delete with a NON-404 error (PR 4608 review): the account
	// is already gone, so re-hitting Composio could turn a second DELETE into a
	// 502 and break the 204-idempotent contract. The first disconnect already
	// revoked upstream and marked the row.
	if !strings.EqualFold(row.Status, "active") {
		return nil
	}

	// Revoke the upstream grant first, then delete the Composio record. Both are
	// made idempotent against a 404 so a repeated disconnect (or a connection
	// already removed at Composio) still resolves the local row.
	if err := s.sdk.RevokeConnection(ctx, row.ConnectedAccountID); err != nil && !isNotFound(err) {
		return fmt.Errorf("composio: revoke connection: %w", err)
	}
	// DeleteConnectedAccount already swallows 404 in the SDK, but guard anyway.
	if err := s.sdk.DeleteConnectedAccount(ctx, row.ConnectedAccountID); err != nil && !isNotFound(err) {
		return fmt.Errorf("composio: delete connected account: %w", err)
	}

	if err := s.store.MarkUserComposioConnectionRevoked(ctx, db.MarkUserComposioConnectionRevokedParams{
		ID:     connectionID,
		UserID: userID,
	}); err != nil {
		return fmt.Errorf("composio: mark revoked: %w", err)
	}
	return nil
}

// CreateMCPSession opens a Composio tool-router (MCP) session scoped to the
// user's active connections. It returns (nil, nil) when the user has no active
// connections — callers treat that as "no MCP overlay for this user".
//
// connected_accounts is pinned per toolkit to the user's own connected account
// id array so the session cannot surface accounts the user did not connect. This
// helper is NOT yet wired into task dispatch (Stage 3); it exists so that wiring
// is a pure consumer of an already-tested seam.
//
// Single-account constraint (v1, PR 4608 review follow-up): the MVP connect
// flow assumes AT MOST ONE active connection per (user, toolkit) — there is no
// UI or API to hold several. Should duplicates ever exist, we must choose
// deterministically: rows arrive newest-first (ListActive orders by
// connected_at DESC), so we keep the FIRST occurrence per toolkit (the most
// recently connected account) instead of letting a later map write silently
// select an older one.
//
// Stage 3 owns the real decision before this is wired into dispatch: either
// enforce the single-active constraint at connect time (revoke the previous
// active row for the same toolkit on a new connect) or extend CreateSession to
// a multi-account request shape. Until then, newest-wins keeps behavior
// deterministic rather than order-dependent.
func (s *Service) CreateMCPSession(ctx context.Context, userID pgtype.UUID) (*MCPSession, error) {
	rows, err := s.store.ListActiveUserComposioConnections(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	connectedAccounts := make(map[string]any, len(rows))
	for _, row := range rows {
		// Keep the first (newest) account per toolkit; ignore older duplicates.
		if _, exists := connectedAccounts[row.ToolkitSlug]; exists {
			continue
		}
		connectedAccounts[row.ToolkitSlug] = []string{row.ConnectedAccountID}
	}

	resp, err := s.sdk.CreateSession(ctx, sdk.CreateSessionRequest{
		UserID:            util.UUIDToString(userID),
		ConnectedAccounts: connectedAccounts,
	})
	if err != nil {
		return nil, fmt.Errorf("composio: create session: %w", err)
	}
	return &MCPSession{
		URL:     resp.MCP.URL,
		Headers: s.sdk.MCPAuthHeaders(),
	}, nil
}

// CallbackRedirect builds the browser redirect target for the callback handler.
// On success it points at the settings page (Integrations tab) with the
// connected toolkit slug; on failure it carries a stable error code. The path
// is the slug-less `/settings?tab=integrations&...` form on purpose: the web
// proxy's legacy-route redirect prepends the user's last workspace slug, so it
// resolves to the real `/{slug}/settings?tab=integrations` route that mounts
// the Composio tab. The older `/settings/integrations` path was NOT a real
// route and 404'd after the legacy rewrite. When FrontendBaseURL is unset it
// returns a site-relative path.
func (s *Service) CallbackRedirect(slug string, success bool) string {
	var path string
	if success {
		path = "/settings?tab=integrations&connected=" + url.QueryEscape(slug)
	} else {
		path = "/settings?tab=integrations&error=composio_connect_failed"
	}
	return s.frontendURL + path
}

// rowToConnection maps a DB row to the API-facing Connection view.
func rowToConnection(row db.UserComposioConnection) Connection {
	c := Connection{
		ID:          util.UUIDToString(row.ID),
		ToolkitSlug: row.ToolkitSlug,
		Status:      row.Status,
	}
	if row.ConnectedAt.Valid {
		c.ConnectedAt = row.ConnectedAt.Time.UTC().Format(time.RFC3339)
	}
	c.LastUsedAt = util.TimestampToPtr(row.LastUsedAt)
	return c
}

// ListToolkits returns only the Composio toolkits the project can actually
// connect (those with an enabled auth config). Toolkits with no enabled auth
// config are filtered out entirely (MUL-4009): a card the user can't act on is
// noise, and the old "Not configured" grey label existed only to avoid a dead
// button — dropping the entry removes the need for it. It fetches all pages
// (capped by maxToolkitPages) so the UI gets the complete connectable list in
// one call; the catalog is a few hundred entries, well within a single JSON
// response. Every returned entry has Connectable == true, preserving Composio's
// usage order.
//
// Unlike the previous behavior, a resolver error is NOT masked into an
// everything-not-connectable catalog: with filtering in place that would render
// as a silent "no apps configured" empty state, which is misleading. We return
// the error so the handler can surface a 502 and the UI can show its honest
// load-failed state instead.
func (s *Service) ListToolkits(ctx context.Context) ([]ToolkitView, error) {
	// connectable is the project's enabled toolkit_slug → auth_config_id map.
	// It is the source of truth for what to surface: only slugs present here
	// are returned.
	connectable, err := s.authConfigMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("composio: resolve connectable toolkits: %w", err)
	}

	out := []ToolkitView{}
	seen := make(map[string]struct{})
	cursor := ""
	for page := 0; page < maxToolkitPages; page++ {
		resp, err := s.sdk.ListToolkits(ctx, sdk.ListToolkitsRequest{
			Limit:  listPageLimit,
			Cursor: cursor,
			SortBy: "usage",
		})
		if err != nil {
			return nil, fmt.Errorf("composio: list toolkits: %w", err)
		}
		for _, tk := range resp.Items {
			slug := strings.ToLower(strings.TrimSpace(tk.Slug))
			if slug == "" {
				continue
			}
			if _, dup := seen[slug]; dup {
				continue
			}
			seen[slug] = struct{}{}
			// Filter out toolkits with no enabled auth config: the user has no
			// working action for them, so they are omitted from the catalog.
			if _, canConnect := connectable[slug]; !canConnect {
				continue
			}
			category := ""
			if len(tk.Categories) > 0 {
				category = tk.Categories[0]
			}
			out = append(out, ToolkitView{
				Slug:     tk.Slug,
				Name:     tk.Name,
				LogoURL:  toolkitLogoURL(slug, tk.LogoURL),
				Category: category,
				// Every surfaced toolkit is connectable by construction. The
				// wire field is kept (see ComposioToolkitResponse) for backward
				// compatibility with older desktop clients that branch on it.
				Connectable: true,
			})
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}

	return out, nil
}

// authConfigForToolkit returns the chosen auth_config_id for a toolkit slug, or
// "" when the project has no enabled auth config for it. It reads the cached
// project-wide map (refreshed on TTL).
func (s *Service) authConfigForToolkit(ctx context.Context, slug string) (string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return "", nil
	}
	m, err := s.authConfigMap(ctx)
	if err != nil {
		return "", err
	}
	return m[slug], nil
}

// authConfigMap returns the toolkit_slug → auth_config_id map for the project,
// rebuilding it from Composio when the cache is empty or expired. Concurrent
// callers serialize on authCacheMu; the fetch runs under the lock, which is
// acceptable for a short-TTL map that is cheap to build and read by a
// low-traffic settings surface. A new map is assigned on refresh (never mutated
// in place), so a reference handed to a caller stays safe to read.
func (s *Service) authConfigMap(ctx context.Context) (map[string]string, error) {
	s.authCacheMu.Lock()
	defer s.authCacheMu.Unlock()
	if s.authCache != nil && s.now().Before(s.authCacheExp) {
		return s.authCache, nil
	}
	m, err := s.fetchAuthConfigMap(ctx)
	if err != nil {
		// Serve a stale snapshot if we have one — a transient /auth_configs
		// blip should not make every toolkit suddenly un-connectable.
		if s.authCache != nil {
			return s.authCache, nil
		}
		return nil, err
	}
	s.authCache = m
	s.authCacheExp = s.now().Add(s.authCacheTTL)
	return m, nil
}

// authCandidate is one project auth config in contention to represent a toolkit
// during resolution.
type authCandidate struct {
	id      string
	managed bool
	updated string
}

// fetchAuthConfigMap pages through the project's ENABLED auth configs and
// reduces them to one chosen auth_config_id per toolkit slug. When a toolkit
// has several (e.g. a Composio-managed one plus a custom white-label one),
// betterAuthConfig picks the winner.
func (s *Service) fetchAuthConfigMap(ctx context.Context) (map[string]string, error) {
	best := make(map[string]authCandidate)
	cursor := ""
	for page := 0; page < maxAuthConfigPages; page++ {
		resp, err := s.sdk.ListAuthConfigs(ctx, sdk.ListAuthConfigsRequest{
			ShowDisabled: false,
			Limit:        listPageLimit,
			Cursor:       cursor,
		})
		if err != nil {
			return nil, fmt.Errorf("composio: list auth configs: %w", err)
		}
		for _, ac := range resp.Items {
			if ac.ID == "" || strings.EqualFold(ac.Status, "DISABLED") {
				continue
			}
			slug := strings.ToLower(strings.TrimSpace(ac.Toolkit.Slug))
			if slug == "" {
				continue
			}
			cand := authCandidate{id: ac.ID, managed: ac.IsComposioManaged, updated: ac.LastUpdatedAt}
			if cur, ok := best[slug]; !ok || betterAuthConfig(cand, cur) {
				best[slug] = cand
			}
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	out := make(map[string]string, len(best))
	for slug, c := range best {
		out[slug] = c.id
	}
	return out, nil
}

// betterAuthConfig reports whether candidate a should win over the currently
// selected b for the same toolkit. A custom (bring-your-own OAuth) config beats
// a Composio-managed one — it is the white-label path the product wants — and
// among configs of the same kind the most recently updated wins.
func betterAuthConfig(a, b authCandidate) bool {
	if a.managed != b.managed {
		return !a.managed
	}
	return a.updated > b.updated
}

// verifyAccountOwnership confirms with Composio that connectedAccountID really
// belongs to expectedUserID and was created under expectedAuthConfigID, so a
// tampered or cross-toolkit connected_account_id on the callback cannot smuggle
// another account into the local mirror. It fails closed: an upstream error, an
// unknown account, an owner mismatch, an EMPTY expected auth config, or an
// auth-config mismatch all return ErrAccountVerification. Requiring a non-empty,
// exactly-matching auth config is what closes the cross-toolkit binding gap —
// the expected value is the auth_config_id signed into the state at
// BeginConnect, which is toolkit-specific.
func (s *Service) verifyAccountOwnership(ctx context.Context, connectedAccountID, expectedUserID, expectedAuthConfigID string) error {
	resp, err := s.sdk.ListConnectedAccounts(ctx, sdk.ListConnectedAccountsRequest{
		ConnectedAccountIDs: []string{connectedAccountID},
	})
	if err != nil {
		return fmt.Errorf("composio: verify connected account: %w", err)
	}
	var acct *sdk.ConnectedAccount
	for i := range resp.Items {
		if resp.Items[i].ID == connectedAccountID {
			acct = &resp.Items[i]
			break
		}
	}
	if acct == nil {
		return ErrAccountVerification
	}
	if acct.UserID != expectedUserID {
		return ErrAccountVerification
	}
	// Fail closed: the account MUST have been created under the exact auth
	// config the connect link used. An empty expected value (state missing it,
	// or a resolver gap) is rejected rather than skipped — skipping is the
	// fail-open hole that let a cross-toolkit account id be bound here.
	accountAuthConfigID := acct.AuthConfigID
	if accountAuthConfigID == "" {
		accountAuthConfigID = acct.AuthConfig.ID
	}
	if expectedAuthConfigID == "" || accountAuthConfigID != expectedAuthConfigID {
		return ErrAccountVerification
	}
	return nil
}

// isNotFound reports whether err is a Composio 404 APIError, used to make
// revoke/delete idempotent.
func isNotFound(err error) bool {
	var apiErr *sdk.APIError
	return errors.As(err, &apiErr) && apiErr.IsNotFound()
}
