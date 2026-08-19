package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// PluginActionCaller is one authorized Action API call: which installation is
// speaking, in which workspace, on whose behalf.
//
// The plugin never holds a credential. A surface talks to the host page over
// postMessage and the host re-issues the call with the signed-in user's own
// session, so the identity here is always a real member — a plugin cannot act
// as anyone but the person using it.
type PluginActionCaller struct {
	Installation db.PluginInstallation
	WorkspaceID  pgtype.UUID
	UserID       pgtype.UUID
	// Scopes is the consented set, read from the installation row rather than
	// from the manifest the source URL serves today.
	Scopes []string
	// IssueScope, when set, is the ONLY issue this caller may touch.
	//
	// Set for a callback token issued against a specific issue. A hook called
	// about one issue has no business reaching another: without this the grant
	// is worth every issue in the workspace that its actor can see, for the
	// whole five minutes it lives. Zero means unrestricted — a session caller,
	// or an invocation that had no issue — and the ordinary workspace and
	// membership checks still apply on top.
	IssueScope pgtype.UUID
}

// AuthorizePluginAction performs the two checks that belong to the plugin: the
// installation is real and enabled, and it holds the scope this call needs.
//
// It deliberately does NOT decide whether the caller may touch the resource.
// That is the third check, and it stays with the normal resource loaders so a
// plugin inherits exactly the permissions of the user behind it — no more, and
// no separate copy of the permission rules to drift.
func (s *PluginService) AuthorizePluginAction(ctx context.Context, installationID string, userID pgtype.UUID, scope string) (PluginActionCaller, error) {
	if installationID == "" {
		return PluginActionCaller{}, pluginErrf(PluginErrorInvalid, "plugin installation is required")
	}
	parsed, err := parseInstallationID(installationID)
	if err != nil {
		return PluginActionCaller{}, err
	}
	installation, err := s.Queries.GetPluginInstallation(ctx, parsed)
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginActionCaller{}, pluginErrf(PluginErrorNotFound, "plugin installation not found")
	}
	if err != nil {
		return PluginActionCaller{}, &PluginError{Kind: PluginErrorUnavailable, Message: "load plugin installation", Err: err}
	}
	// A disabled plugin is off, not merely hidden: an iframe left open in a
	// stale tab must not keep working after an admin disables it.
	if !installation.Enabled {
		return PluginActionCaller{}, pluginErrf(PluginErrorForbidden, "this Plugin is disabled")
	}

	scopes := decodeScopes(installation.GrantedScopes)
	if scope != "" && !hasScope(scopes, scope) {
		return PluginActionCaller{}, &PluginError{
			Kind:    PluginErrorForbidden,
			Message: "this Plugin was not granted the " + scope + " scope",
		}
	}

	return PluginActionCaller{
		Installation: installation,
		WorkspaceID:  installation.WorkspaceID,
		UserID:       userID,
		Scopes:       scopes,
	}, nil
}

func hasScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}

func parseInstallationID(value string) (pgtype.UUID, error) {
	parsed, err := parseUUIDValue(value)
	if err != nil {
		return pgtype.UUID{}, pluginErrf(PluginErrorNotFound, "plugin installation not found")
	}
	return parsed, nil
}

// PluginContext is what a surface gets before it has asked for anything: who is
// looking, where, and which issue the panel is mounted on. It requires no scope
// because it contains nothing the user in front of the iframe cannot already
// see on the page around it.
type PluginContext struct {
	Workspace PluginContextWorkspace `json:"workspace"`
	// User is absent when the caller is the plugin itself — an event hook or a
	// standing install token has no person behind it, and inventing one here
	// would let a handler believe it is acting for somebody.
	User        *PluginContextUser  `json:"user,omitempty"`
	Issue       *PluginContextIssue `json:"issue,omitempty"`
	Config      map[string]any      `json:"config"`
	GrantedURLs []string            `json:"granted_net_domains"`
	// Actor names which of the two it is, so a handler can branch without
	// inferring it from a missing field.
	Actor string `json:"actor"`
}

type PluginContextWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type PluginContextUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Email is deliberately absent: a surface that needs to identify a member
	// to its own backend should use the opaque id, not a mailbox.
}

type PluginContextIssue struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

// BuildPluginContext assembles the context payload. Config carries only the
// non-secret installation values — secrets live in their own table and have no
// read path, so there is no way for one to reach the iframe.
func (s *PluginService) BuildPluginContext(caller PluginActionCaller, workspace db.Workspace, user *db.User, issue *db.Issue) PluginContext {
	config := map[string]any{}
	if len(caller.Installation.Config) > 0 {
		_ = json.Unmarshal(caller.Installation.Config, &config)
	}

	payload := PluginContext{
		Workspace: PluginContextWorkspace{
			ID:   uuidString(workspace.ID),
			Name: workspace.Name,
			Slug: workspace.Slug,
		},
		Config:      config,
		GrantedURLs: plugincontract.NetDomains(caller.Scopes),
		Actor:       "plugin",
	}
	if user != nil {
		payload.Actor = "member"
		payload.User = &PluginContextUser{
			ID:   uuidString(user.ID),
			Name: user.Name,
		}
	}
	if issue != nil {
		payload.Issue = &PluginContextIssue{
			ID:         uuidString(issue.ID),
			Identifier: IssueIdentifier(workspace.IssuePrefix, issue.Number),
			Title:      issue.Title,
		}
	}
	return payload
}
