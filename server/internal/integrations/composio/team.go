package composio

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrTeamConnectRefused is returned by a TeamConnectGate, and then by
// CompleteCallback, when the workspace may not add the team account.
var ErrTeamConnectRefused = errors.New("composio: team account not included in the workspace's plan")

// TeamConnectGate decides, when a completed handshake is about to write a team
// account, whether the workspace may add it. It returns ErrTeamConnectRefused
// to refuse, or admitted, which runs once the account is written. The handler
// supplies it; nil admits every account, as on a self-hosted deployment.
type TeamConnectGate func(ctx context.Context, workspaceID pgtype.UUID, toolkitSlug string) (admitted func(context.Context), err error)

// SetTeamConnectGate installs the gate. Call it before serving requests.
func (s *Service) SetTeamConnectGate(gate TeamConnectGate) { s.teamConnectGate = gate }

// ErrTeamAccountsUnavailable is returned when the configured SDK or store
// cannot serve team accounts or tool execution (test fakes that predate them).
var ErrTeamAccountsUnavailable = errors.New("composio: team accounts unavailable")

// ToolSDK is the part of *sdk.Client the gateway and the unified catalog use.
// It is separate from SDK so fakes written for the connect flow keep working.
type ToolSDK interface {
	GetToolkit(ctx context.Context, slug string) (*sdk.Toolkit, error)
	ListTools(ctx context.Context, req sdk.ListToolsRequest) (*sdk.ListToolsResponse, error)
	GetTool(ctx context.Context, toolSlug string) (*sdk.Tool, error)
	ExecuteTool(ctx context.Context, toolSlug string, req sdk.ExecuteToolRequest) (*sdk.ExecuteToolResponse, error)
	CreateManagedAuthConfig(ctx context.Context, toolkitSlug string) (string, error)
}

// TeamStore persists workspace-level (team) accounts. *db.Queries satisfies it.
type TeamStore interface {
	UpsertWorkspaceComposioConnection(ctx context.Context, arg db.UpsertWorkspaceComposioConnectionParams) (db.WorkspaceComposioConnection, error)
	RevokeOtherWorkspaceComposioConnections(ctx context.Context, arg db.RevokeOtherWorkspaceComposioConnectionsParams) ([]db.WorkspaceComposioConnection, error)
	GetWorkspaceComposioConnection(ctx context.Context, arg db.GetWorkspaceComposioConnectionParams) (db.WorkspaceComposioConnection, error)
	SetWorkspaceComposioConnectionStatus(ctx context.Context, arg db.SetWorkspaceComposioConnectionStatusParams) error
}

// TeamComposioUserID is the Composio user a workspace's team accounts belong
// to. It cannot collide with a member's id, which is a bare UUID.
func TeamComposioUserID(workspaceID pgtype.UUID) string {
	return "ws_" + util.UUIDToString(workspaceID)
}

// CallbackResult is what a completed connect handshake produced.
type CallbackResult struct {
	ToolkitSlug string
	// WorkspaceID is set for a team account.
	WorkspaceID string
	// UserID is the member who connected a personal account, and
	// PersonalWorkspaceID the workspace it belongs to (empty for a legacy
	// account).
	UserID              string
	PersonalWorkspaceID string
	// ConnectedBy is the admin who connected a team account.
	ConnectedBy string
	// ReturnTo is the app path the flow started from, if any.
	ReturnTo string
}

// Team reports whether the handshake connected a team account.
func (r CallbackResult) Team() bool { return r.WorkspaceID != "" }

func (s *Service) toolSDK() (ToolSDK, error) {
	t, ok := s.sdk.(ToolSDK)
	if !ok {
		return nil, ErrTeamAccountsUnavailable
	}
	return t, nil
}

func (s *Service) teamStore() (TeamStore, error) {
	t, ok := s.store.(TeamStore)
	if !ok {
		return nil, ErrTeamAccountsUnavailable
	}
	return t, nil
}

// BeginTeamConnect starts the connect handshake for a workspace's team
// account. The caller has already checked that actorID administers the
// workspace.
func (s *Service) BeginTeamConnect(ctx context.Context, workspaceID, actorID pgtype.UUID, toolkitSlug, returnTo string) (string, error) {
	if !SafeReturnPath(returnTo) {
		returnTo = ""
	}
	if _, err := s.teamStore(); err != nil {
		return "", err
	}
	if !workspaceID.Valid || !actorID.Valid {
		return "", errors.New("composio: invalid workspace or actor id")
	}
	slug := strings.ToLower(strings.TrimSpace(toolkitSlug))
	authConfigID, err := s.ensureAuthConfig(ctx, slug)
	if err != nil {
		return "", err
	}
	composioUserID := TeamComposioUserID(workspaceID)
	state, err := signState(s.secret, stateClaims{
		UserID:       composioUserID,
		ToolkitSlug:  slug,
		AuthConfigID: authConfigID,
		WorkspaceID:  util.UUIDToString(workspaceID),
		ConnectedBy:  util.UUIDToString(actorID),
		ReturnTo:     returnTo,
		Exp:          s.now().Add(s.stateTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("composio: sign state: %w", err)
	}
	resp, err := s.sdk.CreateLink(ctx, sdk.CreateLinkRequest{
		AuthConfigID: authConfigID,
		UserID:       composioUserID,
		CallbackURL:  s.callbackURL + "?state=" + url.QueryEscape(state),
	})
	if err != nil {
		return "", fmt.Errorf("composio: create link: %w", err)
	}
	return resp.RedirectURL, nil
}

// completeTeamCallback writes the team account named by a verified state. A
// workspace keeps one team account per app, so the previous one is revoked.
func (s *Service) completeTeamCallback(ctx context.Context, claims stateClaims, connectedAccountID string) error {
	store, err := s.teamStore()
	if err != nil {
		return err
	}
	workspaceID, err := util.ParseUUID(claims.WorkspaceID)
	if err != nil {
		return fmt.Errorf("composio: state has invalid workspace id: %w", err)
	}
	actorID, err := util.ParseUUID(claims.ConnectedBy)
	if err != nil {
		return fmt.Errorf("composio: state has invalid actor id: %w", err)
	}
	if claims.UserID != TeamComposioUserID(workspaceID) {
		return ErrAccountVerification
	}
	if err := s.verifyAccountOwnership(ctx, connectedAccountID, claims.UserID, claims.AuthConfigID); err != nil {
		return err
	}
	admitted := func(context.Context) {}
	if s.teamConnectGate != nil {
		gateAdmitted, err := s.teamConnectGate(ctx, workspaceID, claims.ToolkitSlug)
		if err != nil {
			// Keep the existing connection and drop the account Composio just
			// created, so it holds no grant Multica does not track.
			if errors.Is(err, ErrTeamConnectRefused) {
				s.revokeUpstream(ctx, connectedAccountID)
			}
			return err
		}
		admitted = gateAdmitted
	}
	row, err := store.UpsertWorkspaceComposioConnection(ctx, db.UpsertWorkspaceComposioConnectionParams{
		WorkspaceID:        workspaceID,
		ToolkitSlug:        claims.ToolkitSlug,
		AuthConfigID:       claims.AuthConfigID,
		ConnectedAccountID: connectedAccountID,
		ComposioUserID:     claims.UserID,
		ConnectedBy:        actorID,
	})
	if err != nil {
		return fmt.Errorf("composio: upsert team connection: %w", err)
	}
	replaced, err := store.RevokeOtherWorkspaceComposioConnections(ctx, db.RevokeOtherWorkspaceComposioConnectionsParams{
		WorkspaceID: workspaceID,
		ToolkitSlug: claims.ToolkitSlug,
		ID:          row.ID,
	})
	if err != nil {
		return fmt.Errorf("composio: revoke replaced team connections: %w", err)
	}
	for _, old := range replaced {
		s.revokeUpstream(ctx, old.ConnectedAccountID)
	}
	admitted(ctx)
	return nil
}

// DisconnectTeam revokes a team account upstream and marks it revoked.
// Idempotent like Disconnect.
func (s *Service) DisconnectTeam(ctx context.Context, workspaceID, connectionID pgtype.UUID) (db.WorkspaceComposioConnection, error) {
	store, err := s.teamStore()
	if err != nil {
		return db.WorkspaceComposioConnection{}, err
	}
	row, err := store.GetWorkspaceComposioConnection(ctx, db.GetWorkspaceComposioConnectionParams{ID: connectionID, WorkspaceID: workspaceID})
	if err != nil {
		return db.WorkspaceComposioConnection{}, ErrConnectionNotFound
	}
	if row.Status == "revoked" {
		return row, nil
	}
	if err := s.sdk.RevokeConnection(ctx, row.ConnectedAccountID); err != nil && !isNotFound(err) {
		return row, fmt.Errorf("composio: revoke connection: %w", err)
	}
	if err := s.sdk.DeleteConnectedAccount(ctx, row.ConnectedAccountID); err != nil && !isNotFound(err) {
		return row, fmt.Errorf("composio: delete connected account: %w", err)
	}
	if err := store.SetWorkspaceComposioConnectionStatus(ctx, db.SetWorkspaceComposioConnectionStatusParams{
		ID: row.ID, WorkspaceID: workspaceID, Status: "revoked",
	}); err != nil {
		return row, fmt.Errorf("composio: mark revoked: %w", err)
	}
	row.Status = "revoked"
	return row, nil
}

func (s *Service) revokeUpstream(ctx context.Context, connectedAccountID string) {
	if err := s.sdk.RevokeConnection(ctx, connectedAccountID); err != nil && !isNotFound(err) {
		return
	}
	_ = s.sdk.DeleteConnectedAccount(ctx, connectedAccountID)
}

// customAuthCreator is the part of *sdk.Client that registers an auth config
// whose credentials each member enters on the Connect Link page. It is
// separate from ToolSDK so fakes written before it keep working.
type customAuthCreator interface {
	CreateCustomAuthConfig(ctx context.Context, toolkitSlug, authScheme, name string) (string, error)
}

// ensureAuthConfig returns the toolkit's auth config, creating one when the
// project has none: with Composio's managed app when the toolkit offers one,
// else with no credentials for a toolkit members connect with their own API
// key, token, or Basic login. This is what lets every catalog app connect
// without a dashboard step.
func (s *Service) ensureAuthConfig(ctx context.Context, slug string) (string, error) {
	id, err := s.authConfigForToolkit(ctx, slug)
	if err != nil || id != "" {
		return id, err
	}
	tools, err := s.toolSDK()
	if err != nil {
		return "", ErrToolkitNotSupported
	}
	tk, err := tools.GetToolkit(ctx, slug)
	if err != nil {
		if isNotFound(err) {
			return "", ErrToolkitNotSupported
		}
		return "", fmt.Errorf("composio: get toolkit: %w", err)
	}
	managed := len(tk.ComposioManagedAuthSchemes) > 0
	keyScheme := userKeyScheme(*tk)
	creator, canCreateKeyConfigs := s.sdk.(customAuthCreator)
	if !managed && (keyScheme == "" || !canCreateKeyConfigs) {
		return "", ErrToolkitNotSupported
	}
	s.authCreateMu.Lock()
	defer s.authCreateMu.Unlock()
	// Another request may have created it while this one waited.
	s.invalidateAuthConfigs()
	if id, err := s.authConfigForToolkit(ctx, slug); err != nil || id != "" {
		return id, err
	}
	if managed {
		id, err = tools.CreateManagedAuthConfig(ctx, slug)
		if err != nil {
			return "", fmt.Errorf("composio: create managed auth config: %w", err)
		}
	} else {
		name := tk.Name
		if name == "" {
			name = slug
		}
		id, err = creator.CreateCustomAuthConfig(ctx, slug, keyScheme, name)
		if err != nil {
			return "", fmt.Errorf("composio: create %s auth config: %w", keyScheme, err)
		}
	}
	s.invalidateAuthConfigs()
	return id, nil
}

func (s *Service) invalidateAuthConfigs() {
	s.authCacheMu.Lock()
	s.authCacheExp = time.Time{}
	s.authCacheMu.Unlock()
}

// ---- tools ----------------------------------------------------------------

const toolCacheTTL = 10 * time.Minute

type toolCacheEntry struct {
	tools []sdk.Tool
	exp   time.Time
}

type toolCache struct {
	mu      sync.Mutex
	entries map[string]toolCacheEntry
}

// ListToolkitTools returns a toolkit's tools, cached briefly. The full list is
// fetched once per toolkit; search filters it locally.
func (s *Service) ListToolkitTools(ctx context.Context, toolkitSlug string) ([]sdk.Tool, error) {
	tools, err := s.toolSDK()
	if err != nil {
		return nil, err
	}
	slug := strings.ToLower(strings.TrimSpace(toolkitSlug))
	s.tools.mu.Lock()
	if e, ok := s.tools.entries[slug]; ok && s.now().Before(e.exp) {
		s.tools.mu.Unlock()
		return e.tools, nil
	}
	s.tools.mu.Unlock()

	var out []sdk.Tool
	cursor := ""
	for page := 0; page < maxToolkitPages; page++ {
		resp, err := tools.ListTools(ctx, sdk.ListToolsRequest{ToolkitSlug: slug, Limit: listPageLimit, Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("composio: list tools: %w", err)
		}
		for _, t := range resp.Items {
			if !t.IsDeprecated {
				out = append(out, t)
			}
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	s.tools.mu.Lock()
	if s.tools.entries == nil {
		s.tools.entries = map[string]toolCacheEntry{}
	}
	s.tools.entries[slug] = toolCacheEntry{tools: out, exp: s.now().Add(toolCacheTTL)}
	s.tools.mu.Unlock()
	return out, nil
}

// GetTool returns one tool definition, from the toolkit cache when warm.
func (s *Service) GetTool(ctx context.Context, toolkitSlug, toolSlug string) (*sdk.Tool, error) {
	tools, err := s.toolSDK()
	if err != nil {
		return nil, err
	}
	upper := strings.ToUpper(strings.TrimSpace(toolSlug))
	if list, err := s.ListToolkitTools(ctx, toolkitSlug); err == nil {
		for i := range list {
			if strings.EqualFold(list[i].Slug, upper) {
				return &list[i], nil
			}
		}
	}
	t, err := tools.GetTool(ctx, upper)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(t.Toolkit.Slug, toolkitSlug) {
		return nil, fmt.Errorf("composio: tool %s does not belong to %s", upper, toolkitSlug)
	}
	return t, nil
}

// ExecuteTool runs a tool against one connected account. Both identifiers must
// come from the same authorized connection record, never from tool arguments.
func (s *Service) ExecuteTool(ctx context.Context, connectedAccountID, composioUserID string, tool sdk.Tool, args map[string]any) (*sdk.ExecuteToolResponse, error) {
	if strings.TrimSpace(composioUserID) == "" {
		return nil, errors.New("composio: connected account user ID is missing; reconnect the account")
	}
	if strings.TrimSpace(connectedAccountID) == "" {
		return nil, errors.New("composio: connected account ID is missing; reconnect the account")
	}
	tools, err := s.toolSDK()
	if err != nil {
		return nil, err
	}
	version := tool.Version
	if version == "" {
		version = "latest"
	}
	return tools.ExecuteTool(ctx, tool.Slug, sdk.ExecuteToolRequest{
		Arguments:          args,
		ConnectedAccountID: connectedAccountID,
		UserID:             composioUserID,
		Version:            version,
	})
}
