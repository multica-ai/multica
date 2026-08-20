package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/tagaccess"
	"github.com/multica-ai/multica/server/internal/util"
)

type TagHTTPMirrorResolver interface {
	MulticaUserID(context.Context, string) (string, bool, error)
	VIBESUserID(context.Context, string) (string, bool, error)
	MulticaWorkspaceID(context.Context, string) (string, bool, error)
}

type tagHTTPQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type postgresTagHTTPMirrors struct{ db tagHTTPQueryer }

func NewPostgresTagHTTPMirrorResolver(db tagHTTPQueryer) TagHTTPMirrorResolver {
	return &postgresTagHTTPMirrors{db: db}
}

func (r *postgresTagHTTPMirrors) MulticaUserID(ctx context.Context, vibesUserID string) (string, bool, error) {
	if r == nil || r.db == nil {
		return "", false, errors.New("Tag HTTP mirror store unavailable")
	}
	var multicaUserID pgtype.UUID
	err := r.db.QueryRow(ctx, `
		SELECT multica_user_id FROM vibes_user_mirror
		WHERE vibes_user_id = $1
	`, vibesUserID).Scan(&multicaUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return util.UUIDToString(multicaUserID), true, nil
}

func (r *postgresTagHTTPMirrors) VIBESUserID(ctx context.Context, multicaUserID string) (string, bool, error) {
	if r == nil || r.db == nil {
		return "", false, errors.New("Tag HTTP mirror store unavailable")
	}
	var vibesUserID string
	err := r.db.QueryRow(ctx, `
		SELECT vibes_user_id FROM vibes_user_mirror
		WHERE multica_user_id = $1::uuid
	`, multicaUserID).Scan(&vibesUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return vibesUserID, true, nil
}

func (r *postgresTagHTTPMirrors) MulticaWorkspaceID(ctx context.Context, vibesWorkspaceID string) (string, bool, error) {
	if r == nil || r.db == nil {
		return "", false, errors.New("Tag HTTP mirror store unavailable")
	}
	var multicaWorkspaceID pgtype.UUID
	err := r.db.QueryRow(ctx, `
		SELECT multica_workspace_id FROM vibes_workspace_mirror
		WHERE vibes_workspace_id = $1
	`, vibesWorkspaceID).Scan(&multicaWorkspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return util.UUIDToString(multicaWorkspaceID), true, nil
}

type tagHTTPContextKey int

const ctxKeyTagHTTPIdentity tagHTTPContextKey = iota

type TagHTTPIdentity struct {
	MulticaUserID              string
	MulticaWorkspaceID         string
	VIBESUserID                string
	VIBESWorkspaceID           string
	VIBESSessionID             string
	SessionWorkspaceGeneration uint64
	Role                       tagaccess.Role
	Mirrored                   bool
	Service                    bool
}

func TagHTTPIdentityFromContext(ctx context.Context) (TagHTTPIdentity, bool) {
	identity, ok := ctx.Value(ctxKeyTagHTTPIdentity).(TagHTTPIdentity)
	return identity, ok
}

type tagHTTPAuthenticationFailure int

const (
	tagHTTPUnauthorized tagHTTPAuthenticationFailure = iota + 1
	tagHTTPForbidden
	tagHTTPUnavailable
)

type tagHTTPAuthenticationError struct{ failure tagHTTPAuthenticationFailure }

func (e tagHTTPAuthenticationError) Error() string { return "Tag HTTP authentication failed" }

// TagHTTPBrowserAuthenticator is the narrow #299 adapter seam. It translates
// one verified Gateway request into the existing mirror and AccessGate truth;
// it owns no permission state and accepts no service audience.
type TagHTTPBrowserAuthenticator struct {
	gate     *tagaccess.Gate
	verifier *tagaccess.HTTPAssertionVerifier
	replay   tagaccess.HTTPAssertionReplayStore
	mirrors  TagHTTPMirrorResolver
}

func NewTagHTTPBrowserAuthenticator(gate *tagaccess.Gate, verifier *tagaccess.HTTPAssertionVerifier, replay tagaccess.HTTPAssertionReplayStore, mirrors TagHTTPMirrorResolver) (*TagHTTPBrowserAuthenticator, error) {
	if gate == nil || verifier == nil || replay == nil || mirrors == nil {
		return nil, errors.New("Tag HTTP browser authenticator is not configured")
	}
	return &TagHTTPBrowserAuthenticator{gate: gate, verifier: verifier, replay: replay, mirrors: mirrors}, nil
}

func (a *TagHTTPBrowserAuthenticator) Authenticate(request *http.Request) (TagHTTPIdentity, error) {
	if a == nil || a.gate == nil || a.verifier == nil || a.replay == nil || a.mirrors == nil {
		return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPUnavailable}
	}
	assertion, err := a.verifier.VerifyRequest(request)
	if err != nil {
		return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPUnauthorized}
	}
	multicaUserID, found, err := a.mirrors.MulticaUserID(request.Context(), assertion.UserID)
	if err != nil {
		return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPUnavailable}
	}
	if !found {
		return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPForbidden}
	}
	multicaWorkspaceID, found, err := a.mirrors.MulticaWorkspaceID(request.Context(), assertion.WorkspaceID)
	if err != nil {
		return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPUnavailable}
	}
	if !found {
		return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPForbidden}
	}
	consumed, err := a.replay.Consume(request.Context(), tagaccess.HTTPAssertionReplay{
		Issuer: assertion.Issuer, Audience: assertion.Audience,
		RequestID: assertion.RequestID, Nonce: assertion.Nonce, ExpiresAt: time.UnixMilli(assertion.ExpiresAt),
	})
	if err != nil {
		return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPUnavailable}
	}
	if !consumed {
		return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPUnauthorized}
	}
	tagSessionID := tagaccess.BrowserTagSessionID(assertion.UserID, assertion.SessionID)
	expiresAt := time.UnixMilli(assertion.ExpiresAt)
	if err := a.gate.GrantSession(request.Context(), tagaccess.SessionGrant{
		TagSessionID: tagSessionID, VIBESSessionID: assertion.SessionID,
		VIBESUserID: assertion.UserID, WorkspaceID: assertion.WorkspaceID,
		AccountEpoch: assertion.AccountEpoch, SessionWorkspaceGeneration: assertion.SessionWorkspaceGeneration,
		MembershipGeneration: assertion.MembershipGeneration, AuthorityVersion: assertion.AuthorityVersion,
		SessionExpiresAt: expiresAt, GrantExpiresAt: expiresAt, Continuous: true,
	}); err != nil {
		if errors.Is(err, tagaccess.ErrGrantDenied) || errors.Is(err, tagaccess.ErrInvalidGrant) {
			return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPForbidden}
		}
		return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPUnavailable}
	}
	decision := a.gate.Authorize(request.Context(), tagaccess.AccessRequest{
		TagSessionID: tagSessionID, VIBESSessionID: assertion.SessionID,
		VIBESUserID: assertion.UserID, WorkspaceID: assertion.WorkspaceID,
		AccountEpoch: assertion.AccountEpoch, SessionWorkspaceGeneration: assertion.SessionWorkspaceGeneration,
		MembershipGeneration: assertion.MembershipGeneration, AuthorityVersion: assertion.AuthorityVersion,
	})
	if !decision.Allowed {
		if decision.Reason == tagaccess.DenyStoreUnavailable {
			return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPUnavailable}
		}
		return TagHTTPIdentity{}, tagHTTPAuthenticationError{tagHTTPForbidden}
	}
	return TagHTTPIdentity{
		MulticaUserID: multicaUserID, MulticaWorkspaceID: multicaWorkspaceID,
		VIBESUserID: assertion.UserID, VIBESWorkspaceID: assertion.WorkspaceID,
		VIBESSessionID: assertion.SessionID, SessionWorkspaceGeneration: assertion.SessionWorkspaceGeneration,
		Role: decision.Role, Mirrored: true,
	}, nil
}

// AuthenticateTagHTTPBrowser must run before native Auth because the #299
// Gateway deliberately removes browser cookies and Authorization headers.
func AuthenticateTagHTTPBrowser(authenticator *TagHTTPBrowserAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if !tagaccess.HasGatewayAssertionHeaders(request) {
				stripUntrustedTagIdentityHeaders(request)
				next.ServeHTTP(w, request)
				return
			}
			if authenticator == nil {
				stripUntrustedTagIdentityHeaders(request)
				writeError(w, http.StatusServiceUnavailable, "Tag authority unavailable")
				return
			}
			identity, err := authenticator.Authenticate(request)
			stripUntrustedTagIdentityHeaders(request)
			request.Header.Del("Authorization")
			request.Header.Del("Proxy-Authorization")
			request.Header.Del("Cookie")
			if err != nil {
				var authErr tagHTTPAuthenticationError
				switch {
				case errors.As(err, &authErr) && authErr.failure == tagHTTPUnavailable:
					writeError(w, http.StatusServiceUnavailable, "Tag authority unavailable")
				case errors.As(err, &authErr) && authErr.failure == tagHTTPForbidden:
					writeError(w, http.StatusForbidden, "Tag access denied")
				default:
					writeError(w, http.StatusUnauthorized, "invalid Tag gateway assertion")
				}
				return
			}
			request.Header.Set("X-User-ID", identity.MulticaUserID)
			request.Header.Set("X-Workspace-ID", identity.MulticaWorkspaceID)
			request.Header.Del("X-Workspace-Slug")
			stripWorkspaceScopeQuery(request.URL)
			ctx := context.WithValue(request.Context(), ctxKeyTagHTTPIdentity, identity)
			next.ServeHTTP(w, request.WithContext(ctx))
		})
	}
}

// RequireTagHTTP runs after native Auth. It rejects mirrored native
// cookie/JWT/PAT fallback, while explicit task/cloud service identities and
// non-mirrored native users keep their existing separate auth paths.
func RequireTagHTTP(mirrors TagHTTPMirrorResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			stripUntrustedTagIdentityHeaders(request)
			multicaUserID := request.Header.Get("X-User-ID")
			if multicaUserID == "" {
				writeError(w, http.StatusUnauthorized, "user not authenticated")
				return
			}
			if identity, ok := TagHTTPIdentityFromContext(request.Context()); ok && identity.Mirrored {
				if identity.MulticaUserID != multicaUserID {
					writeError(w, http.StatusUnauthorized, "user not authenticated")
					return
				}
				next.ServeHTTP(w, request)
				return
			}
			if explicitTagServiceIdentity(request) {
				identity := TagHTTPIdentity{MulticaUserID: multicaUserID, Service: true}
				next.ServeHTTP(w, request.WithContext(context.WithValue(request.Context(), ctxKeyTagHTTPIdentity, identity)))
				return
			}
			if mirrors == nil {
				writeError(w, http.StatusServiceUnavailable, "Tag authority unavailable")
				return
			}
			_, mirrored, err := mirrors.VIBESUserID(request.Context(), multicaUserID)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "Tag authority unavailable")
				return
			}
			if mirrored {
				writeError(w, http.StatusUnauthorized, "VIBES gateway assertion required")
				return
			}
			next.ServeHTTP(w, request)
		})
	}
}

func DenyMirroredAuthorityWriter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if identity, ok := TagHTTPIdentityFromContext(request.Context()); ok && (identity.Mirrored || identity.Service) {
			writeError(w, http.StatusForbidden, "VIBES is the authority for this operation")
			return
		}
		next.ServeHTTP(w, request)
	})
}

func explicitTagServiceIdentity(request *http.Request) bool {
	switch request.Header.Get("X-Actor-Source") {
	case "task_token", "cloud_pat":
		return true
	default:
		return false
	}
}

func stripUntrustedTagIdentityHeaders(request *http.Request) {
	for name := range request.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-vibes-") || strings.HasPrefix(lower, "x-tag-") || strings.HasPrefix(lower, "x-internal-") {
			request.Header.Del(name)
		}
	}
}

func stripWorkspaceScopeQuery(target *url.URL) {
	if target == nil || target.RawQuery == "" {
		return
	}
	pairs := strings.Split(target.RawQuery, "&")
	kept := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		rawName, _, _ := strings.Cut(pair, "=")
		name, err := url.QueryUnescape(rawName)
		if err != nil {
			return
		}
		if name != "workspace_id" && name != "workspace_slug" {
			kept = append(kept, pair)
		}
	}
	target.RawQuery = strings.Join(kept, "&")
}
