package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Two credentials, moving in opposite directions.
//
// Neither one ever enters an iframe: a surface still holds nothing and still
// reaches Multica only by asking the host page over postMessage. What changes
// with hooks is that a plugin now has a SERVER, and that server needs a way to
// be recognised. So the honest statement about the system is no longer "there
// are no plugin credentials" but "plugin credentials only move between
// servers".
//
//	install token  (mpi_…)  plugin -> host, long-lived, rotatable.
//	                        The host only ever verifies it, so it is stored
//	                        hashed and cannot be recovered from the database.
//	callback token          host -> plugin, minutes, one INVOCATION.
//	                        Handed to a hook handler so it can answer using the
//	                        Action API without being given standing access.

const (
	installTokenPrefix   = "mpi_"
	callbackTokenPrefix  = "mpc_"
	callbackTokenTTL     = 5 * time.Minute
	callbackTokenEntropy = 32
)

// IssueInstallToken mints a new install token and stores only its hash.
//
// Returned in plaintext exactly once. There is no endpoint that reads it back —
// an admin who loses it rotates rather than recovers, which is the same trade
// every other bearer credential in the product makes.
func (s *PluginService) IssueInstallToken(ctx context.Context, installationID pgtype.UUID) (string, error) {
	raw := make([]byte, callbackTokenEntropy)
	if _, err := rand.Read(raw); err != nil {
		return "", &PluginError{Kind: PluginErrorUnavailable, Message: "generate install token", Err: err}
	}
	token := installTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	if err := s.Queries.SetPluginInstallationToken(ctx, db.SetPluginInstallationTokenParams{
		ID:        installationID,
		TokenHash: pgtype.Text{String: hashToken(token), Valid: true},
	}); err != nil {
		return "", &PluginError{Kind: PluginErrorUnavailable, Message: "store install token", Err: err}
	}
	return token, nil
}

// RevokeInstallToken drops the stored hash, so nothing presented afterwards
// matches. Rotation is IssueInstallToken, which overwrites in place.
func (s *PluginService) RevokeInstallToken(ctx context.Context, installationID pgtype.UUID) error {
	if err := s.Queries.SetPluginInstallationToken(ctx, db.SetPluginInstallationTokenParams{
		ID:        installationID,
		TokenHash: pgtype.Text{},
	}); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "revoke install token", Err: err}
	}
	return nil
}

// AuthenticateInstallToken resolves a presented token to its installation.
func (s *PluginService) AuthenticateInstallToken(ctx context.Context, token string) (db.PluginInstallation, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, installTokenPrefix) {
		return db.PluginInstallation{}, pluginErrf(PluginErrorForbidden, "invalid plugin token")
	}
	installation, err := s.Queries.GetPluginInstallationByTokenHash(ctx, pgtype.Text{String: hashToken(token), Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.PluginInstallation{}, pluginErrf(PluginErrorForbidden, "invalid plugin token")
	}
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "load plugin installation", Err: err}
	}
	if !installation.Enabled {
		return db.PluginInstallation{}, pluginErrf(PluginErrorForbidden, "this Plugin is disabled")
	}
	return installation, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CallbackGrant is what a redeemed callback token proves.
//
// Its scopes are the installation's, never wider: the callback exists so a hook
// handler can finish the job it was called for, not so an out-of-band request
// can do more than the surface could.
type CallbackGrant struct {
	InstallationID pgtype.UUID
	WorkspaceID    pgtype.UUID
	HookKey        string
	Trigger        string
	// Actor is who the resulting writes belong to, decided when the hook was
	// dispatched. A handler cannot choose to write as somebody else.
	Actor HookActor
	// IssueID narrows an event callback to the issue that produced it. Zero
	// when the invocation had no issue.
	IssueID   pgtype.UUID
	ExpiresAt time.Time
}

// CallbackTokens issues and resolves the per-invocation callback tokens.
//
// Scoped to one INVOCATION, not one request — and that distinction was found by
// running a real handler rather than by reasoning about it. A single-use token
// looked stricter and was: the reference handler reads the issue, decides, then
// posts a comment, and the second call died on an already-spent token. Two calls
// is the floor for any handler that does something with what it read.
//
// The trade that actually matters is not one-call versus several. It is this
// token versus the installation's standing token: a handler that cannot finish
// its job with the callback will simply be written against the install token
// instead, which never expires and is not scoped to an invocation. A control
// that pushes authors toward the stronger credential is worse than the slightly
// looser one they will use.
//
// What still bounds it: minutes, this installation's granted scopes, the actor
// decided at dispatch, and the issue the invocation was about.
//
// Held in memory. Across several instances or after a restart a token stops
// resolving early rather than late, so the failure mode is a handler seeing 403
// — visible and retriable — not a grant outliving its window.
type CallbackTokens struct {
	mu     sync.Mutex
	issued map[string]CallbackGrant
}

func NewCallbackTokens() *CallbackTokens {
	return &CallbackTokens{issued: make(map[string]CallbackGrant)}
}

// Issue mints a token for one hook invocation.
func (c *CallbackTokens) Issue(ctx context.Context, invocation HookInvocation) (string, error) {
	raw := make([]byte, callbackTokenEntropy)
	if _, err := rand.Read(raw); err != nil {
		return "", &PluginError{Kind: PluginErrorUnavailable, Message: "generate callback token", Err: err}
	}
	token := callbackTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)

	grant := CallbackGrant{
		InstallationID: invocation.Installation.ID,
		WorkspaceID:    invocation.Installation.WorkspaceID,
		HookKey:        invocation.Hook.Key,
		Trigger:        invocation.Trigger,
		Actor:          invocation.Actor,
		IssueID:        invocation.IssueID,
		ExpiresAt:      time.Now().Add(callbackTokenTTL),
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked()
	c.issued[hashToken(token)] = grant
	_ = ctx
	return token, nil
}

// Resolve looks a token up. Valid for as many calls as the handler needs until
// it expires; see the type comment for why that is the right bound.
func (c *CallbackTokens) Resolve(token string) (CallbackGrant, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, callbackTokenPrefix) {
		return CallbackGrant{}, pluginErrf(PluginErrorForbidden, "invalid callback token")
	}
	key := hashToken(token)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked()
	grant, ok := c.issued[key]
	if !ok {
		return CallbackGrant{}, pluginErrf(PluginErrorForbidden, "callback token is expired or unknown")
	}
	if time.Now().After(grant.ExpiresAt) {
		delete(c.issued, key)
		return CallbackGrant{}, pluginErrf(PluginErrorForbidden, "callback token is expired or unknown")
	}
	return grant, nil
}

// Revoke drops a grant before its expiry. Called when a hook invocation has
// finished, so a token stops working the moment the work it was issued for is
// over rather than lingering for the rest of its window.
func (c *CallbackTokens) Revoke(token string) {
	if token == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.issued, hashToken(strings.TrimSpace(token)))
}

// sweepLocked drops expired grants so the map cannot grow without bound on a
// long-running server. Callers hold the mutex.
func (c *CallbackTokens) sweepLocked() {
	now := time.Now()
	for key, grant := range c.issued {
		if now.After(grant.ExpiresAt) {
			delete(c.issued, key)
		}
	}
}
