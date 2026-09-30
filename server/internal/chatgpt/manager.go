// Package chatgpt owns local Sign in with ChatGPT registrations and credentials.
// It never reads Codex credentials or sends tokens to the Multica server.
package chatgpt

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	AppName         = "Multica"
	Issuer          = "https://auth.openai.com"
	Resource        = "https://api.openai.com/v1"
	DirectScope     = "chatgpt.tokens.use.direct"
	requestedScopes = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
)

// Error retains machine-readable recovery information without upstream token data.
type Error struct {
	Code       string
	StatusCode int
	Retryable  bool
	Message    string
	RequestID  string
	Param      string
	BodyShape  string
}

func (e *Error) Error() string {
	details := []string{}
	if e.Code != "" {
		details = append(details, "code="+e.Code)
	}
	if e.StatusCode != 0 {
		details = append(details, fmt.Sprintf("status=%d", e.StatusCode))
	}
	if e.RequestID != "" {
		details = append(details, "request_id="+e.RequestID)
	}
	if e.Param != "" {
		details = append(details, "param="+e.Param)
	}
	if e.BodyShape != "" {
		details = append(details, "body="+e.BodyShape)
	}
	if len(details) == 0 {
		return e.Message
	}
	return e.Message + " (" + strings.Join(details, ", ") + ")"
}

// Account is safe for account pickers and CLI status output. It has no tokens.
type Account struct {
	ClientID    string    `json:"client_id"`
	Subject     string    `json:"subject"`
	Email       string    `json:"email,omitempty"`
	Label       string    `json:"label"`
	Active      bool      `json:"active"`
	SignedIn    bool      `json:"signed_in"`
	PlanEnabled bool      `json:"plan_enabled"`
	CanRefresh  bool      `json:"can_refresh"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Token is a dispatch credential, never a status/config serialization format.
type Token struct {
	AccessToken string    `json:"-"`
	ClientID    string    `json:"client_id"`
	Subject     string    `json:"subject"`
	Scopes      []string  `json:"scopes"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (t Token) String() string   { return "ChatGPTToken(<redacted>)" }
func (t Token) GoString() string { return t.String() }

type Manager struct {
	path   string
	client *http.Client
}

func NewManager(storePath string) *Manager {
	return &Manager{path: storePath, client: &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

type credentialStore struct {
	Version           int                      `json:"version"`
	HostID            string                   `json:"host_id"`
	ActiveClientID    string                   `json:"active_client_id,omitempty"`
	SelectionRevision string                   `json:"selection_revision,omitempty"`
	Registrations     map[string]*registration `json:"registrations"`
	PendingLogins     map[string]*registration `json:"pending_logins,omitempty"`
}

type registration struct {
	ClientID     string         `json:"client_id"`
	Subject      string         `json:"subject,omitempty"`
	Email        string         `json:"email,omitempty"`
	Label        string         `json:"label"`
	AccessToken  string         `json:"access_token,omitempty"`
	RefreshToken string         `json:"refresh_token,omitempty"`
	IDToken      string         `json:"id_token,omitempty"`
	Scopes       []string       `json:"scopes,omitempty"`
	ExpiresAt    time.Time      `json:"expires_at"`
	Revision     string         `json:"revision"`
	Pending      *pendingTokens `json:"pending,omitempty"`
}

type pendingTokens struct {
	Response          tokenResponse `json:"response"`
	ReceivedAt        time.Time     `json:"received_at"`
	Nonce             string        `json:"nonce,omitempty"`
	Login             bool          `json:"login"`
	SelectionRevision string        `json:"selection_revision,omitempty"`
}

func (r *registration) account(active string) Account {
	signedIn := r.Subject != "" && r.AccessToken != "" && r.Pending == nil
	return Account{ClientID: r.ClientID, Subject: r.Subject, Email: r.Email, Label: r.Label,
		Active: r.ClientID == active, SignedIn: signedIn, PlanEnabled: signedIn && hasScope(r.Scopes, DirectScope),
		CanRefresh: r.RefreshToken != "", ExpiresAt: r.ExpiresAt}
}

func (r *registration) token() Token {
	return Token{AccessToken: r.AccessToken, ClientID: r.ClientID, Subject: r.Subject,
		Scopes: append([]string(nil), r.Scopes...), ExpiresAt: r.ExpiresAt}
}

func (r *registration) clear() {
	r.AccessToken = ""
	r.RefreshToken = ""
	r.IDToken = ""
	r.Scopes = nil
	r.Pending = nil
	r.ExpiresAt = time.Time{}
	r.Revision = uuid.NewString()
}

func hasScope(scopes []string, wanted string) bool {
	for _, scope := range scopes {
		if scope == wanted {
			return true
		}
	}
	return false
}

func (m *Manager) load() (*credentialStore, error) {
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return &credentialStore{Version: 1, Registrations: map[string]*registration{}, PendingLogins: map[string]*registration{}}, nil
	}
	if err != nil {
		return nil, &Error{Code: "storage_read", Message: "Could not read local ChatGPT credentials."}
	}
	var store credentialStore
	if json.Unmarshal(data, &store) != nil || store.Version != 1 || store.Registrations == nil {
		return nil, &Error{Code: "storage_invalid", Message: "Local ChatGPT credential storage is invalid."}
	}
	if store.HostID != "" {
		if !strings.HasPrefix(store.HostID, "urn:uuid:") {
			return nil, &Error{Code: "storage_invalid", Message: "Invalid local ChatGPT host ID."}
		}
		if _, err := uuid.Parse(strings.TrimPrefix(store.HostID, "urn:uuid:")); err != nil {
			return nil, &Error{Code: "storage_invalid", Message: "Invalid local ChatGPT host ID."}
		}
	}
	for id, r := range store.Registrations {
		if r == nil || r.ClientID != id || id == "" || id == "dynamic_agent_client" {
			return nil, &Error{Code: "storage_invalid", Message: "Invalid local ChatGPT registration."}
		}
	}
	if store.PendingLogins == nil {
		store.PendingLogins = map[string]*registration{}
	}
	return &store, nil
}

func (m *Manager) save(store *credentialStore) error {
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return &Error{Code: "storage_write", Message: "Could not create local ChatGPT credential storage."}
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".chatgpt-*")
	if err != nil {
		return &Error{Code: "storage_write", Message: "Could not write local ChatGPT credentials."}
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, m.path)
	}
	if err != nil {
		return &Error{Code: "storage_write", Message: "Could not replace local ChatGPT credentials."}
	}
	return nil
}

func (m *Manager) withLock(ctx context.Context, fn func(*credentialStore) error) error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return &Error{Code: "storage_write", Message: "Could not create local ChatGPT credential storage."}
	}
	f, err := os.OpenFile(m.path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return &Error{Code: "storage_lock", Message: "Could not lock local ChatGPT credentials."}
	}
	defer f.Close()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for {
		ok, err := tryLock(f)
		if err != nil {
			return &Error{Code: "storage_lock", Message: "Could not lock local ChatGPT credentials."}
		}
		if ok {
			break
		}
		select {
		case <-ctx.Done():
			return &Error{Code: "storage_busy", Retryable: true, Message: "Another process is updating ChatGPT credentials. Try again."}
		case <-time.After(20 * time.Millisecond):
		}
	}
	defer unlock(f)
	store, err := m.load()
	if err != nil {
		return err
	}
	return fn(store)
}

func (m *Manager) Accounts() ([]Account, error) {
	store, err := m.load()
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, 0, len(store.Registrations))
	for _, r := range store.Registrations {
		accounts = append(accounts, r.account(store.ActiveClientID))
	}
	for id, r := range store.PendingLogins {
		if s := store.Registrations[id]; s == nil {
			accounts = append(accounts, r.account(store.ActiveClientID))
		}
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ClientID < accounts[j].ClientID })
	return accounts, nil
}

func (m *Manager) Status() (*Account, error) {
	store, err := m.load()
	if err != nil {
		return nil, err
	}
	r := store.Registrations[store.ActiveClientID]
	if r == nil {
		return nil, nil
	}
	a := r.account(store.ActiveClientID)
	return &a, nil
}

func (m *Manager) Select(ctx context.Context, clientID string) error {
	return m.withLock(ctx, func(s *credentialStore) error {
		r := s.Registrations[clientID]
		if r == nil {
			return authRequired("Choose an existing ChatGPT registration or sign in.")
		}
		if !r.account(s.ActiveClientID).PlanEnabled {
			return authRequired("Enable ChatGPT plan usage by signing in to this registration again.")
		}
		s.ActiveClientID = clientID
		s.SelectionRevision = uuid.NewString()
		return m.save(s)
	})
}

func authRequired(message string) *Error {
	return &Error{Code: "chatgpt_auth_required", Message: message}
}
func sessionChanged() *Error {
	return &Error{Code: "chatgpt_session_changed", Message: "The selected ChatGPT session changed. Start a new request."}
}

func selected(s *credentialStore, expected string) (*registration, error) {
	if expected != "" && expected != s.ActiveClientID {
		return nil, sessionChanged()
	}
	r := s.Registrations[s.ActiveClientID]
	if r == nil {
		return nil, authRequired("Sign in with ChatGPT before using this runtime.")
	}
	return r, nil
}

func (m *Manager) AccessToken(ctx context.Context, expectedClientID string) (Token, error) {
	var token Token
	err := m.withLock(ctx, func(s *credentialStore) error {
		r, err := selected(s, expectedClientID)
		if err != nil {
			return err
		}
		if r.Pending != nil {
			if err = m.finishPending(ctx, s, r); err != nil {
				return err
			}
		}
		if r.Subject == "" || r.AccessToken == "" {
			return authRequired("Sign in with ChatGPT again to renew this registration.")
		}
		if !hasScope(r.Scopes, DirectScope) {
			return authRequired("ChatGPT plan usage is disabled. Sign in again with plan permission.")
		}
		if !r.ExpiresAt.After(time.Now().Add(time.Minute)) {
			if r.RefreshToken == "" {
				return authRequired("ChatGPT authorization expired. Sign in again.")
			}
			response, err := m.postToken(ctx, urlValues("grant_type", "refresh_token", "client_id", r.ClientID, "refresh_token", r.RefreshToken, "resource", Resource))
			if err != nil {
				if terminalRefresh(err) {
					r.clear()
					if saveErr := m.save(s); saveErr != nil {
						return saveErr
					}
				}
				return err
			}
			r.Pending = &pendingTokens{Response: response, ReceivedAt: time.Now()}
			if err = m.save(s); err != nil {
				return err
			}
			if err = m.finishPending(ctx, s, r); err != nil {
				return err
			}
		}
		if !hasScope(r.Scopes, DirectScope) {
			return authRequired("ChatGPT plan usage permission was not granted. Sign in again to enable it.")
		}
		token = r.token()
		return nil
	})
	return token, err
}

func (m *Manager) AssertActive(ctx context.Context, token Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := m.load()
	if err != nil {
		return err
	}
	r, err := selected(s, token.ClientID)
	if err != nil {
		return err
	}
	if r.Pending != nil || r.Subject != token.Subject || token.AccessToken == "" ||
		subtle.ConstantTimeCompare([]byte(r.AccessToken), []byte(token.AccessToken)) != 1 || !hasScope(r.Scopes, DirectScope) || !r.ExpiresAt.After(time.Now()) {
		return sessionChanged()
	}
	return nil
}

func terminalRefresh(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code {
	case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused":
		return true
	}
	return false
}

func (m *Manager) finishPending(ctx context.Context, s *credentialStore, r *registration) error {
	pending := r.Pending
	if pending == nil {
		return nil
	}
	response := pending.Response
	if response.AccessToken == "" || !strings.EqualFold(response.TokenType, "Bearer") || response.ExpiresIn <= 0 || response.ExpiresIn > 365*24*60*60 {
		return &Error{Code: "token_response_invalid", Message: "ChatGPT returned incomplete credentials. Sign in again."}
	}
	if pending.Login && response.Scope == nil {
		return &Error{Code: "scope_missing", Message: "ChatGPT did not report the granted permissions."}
	}
	subject, email := r.Subject, r.Email
	if response.IDToken != "" {
		claims, err := m.validateIDToken(ctx, response.IDToken, r.ClientID, pending.Nonce)
		if err != nil {
			return err
		}
		if subject != "" && claims.Subject != subject {
			return &Error{Code: "identity_mismatch", Message: "ChatGPT returned a different account for this registration."}
		}
		subject, email = claims.Subject, claims.Email
	} else if pending.Login {
		return &Error{Code: "id_token_missing", Message: "ChatGPT sign-in did not return an ID token."}
	}
	if subject == "" {
		return &Error{Code: "identity_missing", Message: "ChatGPT identity could not be verified."}
	}
	r.Subject = subject
	r.Email = email
	r.AccessToken = response.AccessToken
	// Refresh may omit an unchanged renewable grant. A new sign-in may grant
	// only identity or short-lived access, without offline_access at all.
	if response.RefreshToken != "" || pending.Login {
		r.RefreshToken = response.RefreshToken
	}
	if response.IDToken != "" {
		r.IDToken = response.IDToken
	}
	if response.Scope != nil {
		r.Scopes = strings.Fields(*response.Scope)
	}
	r.ExpiresAt = pending.ReceivedAt.Add(time.Duration(response.ExpiresIn * float64(time.Second)))
	r.Pending = nil
	r.Revision = uuid.NewString()
	if r.Label == "" {
		r.Label = fmt.Sprintf("%s (%s)", r.Email, r.ClientID)
	}
	if pending.Login {
		s.Registrations[r.ClientID] = r
		delete(s.PendingLogins, r.ClientID)
		// Keep the verified registration, but a delayed browser callback must
		// never undo an account selection made while that browser was open.
		if pending.SelectionRevision != s.SelectionRevision {
			if err := m.save(s); err != nil {
				return err
			}
			return sessionChanged()
		}
		current := s.Registrations[s.ActiveClientID]
		if hasScope(r.Scopes, DirectScope) || current == nil || !current.account(s.ActiveClientID).PlanEnabled {
			s.ActiveClientID = r.ClientID
			s.SelectionRevision = uuid.NewString()
		}
	}
	return m.save(s)
}
