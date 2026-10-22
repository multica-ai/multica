package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type tokenResponse struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	IDToken      string  `json:"id_token"`
	TokenType    string  `json:"token_type"`
	ExpiresIn    float64 `json:"expires_in"`
	Scope        *string `json:"scope"`
}

type discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	RevocationEndpoint    string `json:"revocation_endpoint"`
}

// Authorization.URL is only for opening the browser; use DisplayURL in output.
type Authorization struct {
	URL        string
	DisplayURL string
}

func (a Authorization) String() string   { return a.DisplayURL }
func (a Authorization) GoString() string { return a.DisplayURL }

type LoginOptions struct {
	ClientID  string
	Label     string
	Reconsent bool
	Authorize func(Authorization) error
}

// LoginAttempt owns one callback listener and one-use state/nonce/PKCE material.
type LoginAttempt struct {
	mu                                                                      sync.Mutex
	state, nonce, verifier, redirectURI, clientID, subject, revision, label string
	selectionRevision                                                       string
	authorization                                                           Authorization
	server                                                                  *http.Server
	callback                                                                chan url.Values
	used                                                                    bool
	closed                                                                  chan struct{}
	closeOnce                                                               sync.Once
}

func (a *LoginAttempt) Authorization() Authorization { return a.authorization }

func (a *LoginAttempt) Wait(ctx context.Context) (url.Values, error) {
	select {
	case values := <-a.callback:
		return values, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-a.closed:
		return nil, &Error{Code: "login_cancelled", Message: "ChatGPT sign-in was cancelled."}
	}
}

func (a *LoginAttempt) Close() {
	a.closeOnce.Do(func() {
		close(a.closed)
		if a.server != nil {
			_ = a.server.Close()
		}
	})
}

func randomValue() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (m *Manager) BeginLogin(ctx context.Context, existingClientID string) (*LoginAttempt, error) {
	return m.beginLogin(ctx, LoginOptions{ClientID: existingClientID}, nil)
}

func (m *Manager) beginLogin(ctx context.Context, options LoginOptions, pending *LoginAttempt) (*LoginAttempt, error) {
	var hostID string
	var selectionRevision string
	var saved *registration
	err := m.withLock(ctx, func(s *credentialStore) error {
		if s.HostID == "" {
			s.HostID = "urn:uuid:" + uuid.NewString()
			if err := m.save(s); err != nil {
				return err
			}
		}
		hostID = s.HostID
		selectionRevision = s.SelectionRevision
		if options.ClientID != "" {
			saved = s.Registrations[options.ClientID]
			if saved == nil {
				saved = s.PendingLogins[options.ClientID]
			}
			if saved == nil && pending == nil {
				return authRequired("The selected ChatGPT registration does not exist.")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	a := &LoginAttempt{clientID: options.ClientID, label: strings.TrimSpace(options.Label), callback: make(chan url.Values, 1), closed: make(chan struct{})}
	a.selectionRevision = selectionRevision
	if saved != nil {
		a.subject = saved.Subject
		a.revision = saved.Revision
		if a.label == "" {
			a.label = saved.Label
		}
	}
	if pending != nil {
		a.clientID = pending.clientID
		a.subject = pending.subject
		a.revision = pending.revision
		a.selectionRevision = pending.selectionRevision
	}
	if a.state, err = randomValue(); err != nil {
		return nil, err
	}
	if a.nonce, err = randomValue(); err != nil {
		return nil, err
	}
	if a.verifier, err = randomValue(); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, &Error{Code: "callback_listener", Message: "Could not start the ChatGPT loopback callback listener."}
	}
	a.redirectURI = "http://" + listener.Addr().String() + "/auth/callback"
	a.server = &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/auth/callback" {
			http.NotFound(w, r)
			return
		}
		values := r.URL.Query()
		states := values["state"]
		if len(states) != 1 || subtle.ConstantTimeCompare([]byte(states[0]), []byte(a.state)) != 1 {
			http.Error(w, "Invalid sign-in state.", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		select {
		case a.callback <- values:
			_, _ = io.WriteString(w, "Return to Multica to complete sign-in.")
		default:
			http.Error(w, "Sign-in callback already received.", http.StatusConflict)
		}
	})}
	go func() { _ = a.server.Serve(listener) }()
	go func() {
		select {
		case <-ctx.Done():
			a.Close()
		case <-a.closed:
		}
	}()
	challenge := sha256.Sum256([]byte(a.verifier))
	values := urlValues("response_type", "code", "redirect_uri", a.redirectURI, "scope", requestedScopes, "resource", Resource, "state", a.state, "nonce", a.nonce, "code_challenge_method", "S256", "code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]), "ext_agent_host_id", hostID)
	if a.clientID == "" {
		values.Set("client_id", "dynamic_agent_client")
		values.Set("agent_name_hint", AppName)
	} else {
		values.Set("client_id", a.clientID)
		if saved != nil {
			if saved.IDToken != "" {
				values.Set("id_token_hint", saved.IDToken)
			}
			if saved.Email != "" {
				values.Set("login_hint", saved.Email)
			}
		}
	}
	if options.Reconsent {
		values.Set("prompt", "consent")
	}
	a.authorization.URL = Issuer + "/api/accounts/authorize?" + values.Encode()
	values.Del("id_token_hint")
	a.authorization.DisplayURL = Issuer + "/api/accounts/authorize?" + values.Encode()
	return a, nil
}

func (m *Manager) Login(ctx context.Context, options LoginOptions) (Account, error) {
	if options.Authorize == nil {
		return Account{}, &Error{Code: "authorization_handler", Message: "A browser or authorization URL handler is required."}
	}
	// Resume a consumed code exchange whose ID-token verification was temporarily unavailable.
	if options.ClientID != "" {
		var resumed *Account
		err := m.withLock(ctx, func(s *credentialStore) error {
			pending := s.PendingLogins[options.ClientID]
			if pending == nil {
				return nil
			}
			if err := m.finishPending(ctx, s, pending); err != nil {
				var validationErr *Error
				if !errors.As(err, &validationErr) || validationErr.Retryable || !permanentIdentityFailure(validationErr.Code) {
					return err
				}
				// A permanently rejected ID token must not trap all future
				// login attempts behind the same failed verification. Retain
				// the issued client, then request a fresh authorization code.
				pending.clear()
				if s.Registrations[pending.ClientID] == nil {
					s.Registrations[pending.ClientID] = pending
				}
				delete(s.PendingLogins, pending.ClientID)
				return m.save(s)
			}
			a := pending.account(s.ActiveClientID)
			resumed = &a
			return nil
		})
		if err != nil {
			return Account{}, err
		}
		if resumed != nil {
			return *resumed, nil
		}
	}
	var previous *LoginAttempt
	for attempt := 0; attempt < 2; attempt++ {
		a, err := m.beginLogin(ctx, options, previous)
		if err != nil {
			return Account{}, err
		}
		if err = options.Authorize(a.Authorization()); err != nil {
			a.Close()
			return Account{}, err
		}
		values, err := a.Wait(ctx)
		if err != nil {
			a.Close()
			return Account{}, err
		}
		account, err := m.CompleteLogin(ctx, a, values)
		a.Close()
		if err == nil {
			return account, nil
		}
		var oauthErr *Error
		if !errors.As(err, &oauthErr) || oauthErr.Code != "invalid_grant" || a.clientID == "" || attempt > 0 {
			return Account{}, err
		}
		previous = a
		options.ClientID = a.clientID
	}
	return Account{}, authRequired("ChatGPT authorization expired. Start sign-in again.")
}

func (m *Manager) CompleteLogin(ctx context.Context, a *LoginAttempt, callback url.Values) (Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.used {
		return Account{}, &Error{Code: "callback_reused", Message: "This ChatGPT authorization attempt has already been used."}
	}
	for _, key := range []string{"state", "code", "client_id", "error"} {
		if len(callback[key]) > 1 {
			return Account{}, &Error{Code: "callback_invalid", Message: "The ChatGPT callback contains duplicate parameters."}
		}
	}
	if subtle.ConstantTimeCompare([]byte(callback.Get("state")), []byte(a.state)) != 1 {
		return Account{}, &Error{Code: "state_mismatch", Message: "The ChatGPT callback did not match this sign-in attempt."}
	}
	a.used = true
	if callback.Get("error") != "" {
		return Account{}, &Error{Code: "access_denied", Message: "ChatGPT sign-in was not authorized."}
	}
	issued := callback.Get("client_id")
	if a.clientID == "" {
		if issued == "" || issued == "dynamic_agent_client" || strings.TrimSpace(issued) != issued {
			return Account{}, &Error{Code: "client_id_missing", Message: "ChatGPT registration did not return an issued client ID."}
		}
		a.clientID = issued
	} else if issued != "" && issued != a.clientID {
		return Account{}, &Error{Code: "client_id_mismatch", Message: "ChatGPT returned a different client registration."}
	}
	if callback.Get("code") == "" {
		return Account{}, &Error{Code: "code_missing", Message: "ChatGPT sign-in did not return an authorization code."}
	}
	response, err := m.postToken(ctx, urlValues("grant_type", "authorization_code", "client_id", a.clientID, "code", callback.Get("code"), "code_verifier", a.verifier, "redirect_uri", a.redirectURI, "resource", Resource))
	if err != nil {
		return Account{}, err
	}
	var result Account
	err = m.withLock(ctx, func(s *credentialStore) error {
		old := s.Registrations[a.clientID]
		if old != nil && (old.Subject != a.subject || old.Revision != a.revision) {
			return sessionChanged()
		}
		r := &registration{ClientID: a.clientID, Subject: a.subject, Label: a.label, Revision: uuid.NewString(), Pending: &pendingTokens{Response: response, ReceivedAt: time.Now(), Nonce: a.nonce, Login: true, SelectionRevision: a.selectionRevision}}
		if old != nil {
			r.Email = old.Email
		}
		s.PendingLogins[a.clientID] = r
		if err := m.save(s); err != nil {
			return err
		}
		if err := m.finishPending(ctx, s, r); err != nil {
			return err
		}
		result = r.account(s.ActiveClientID)
		return nil
	})
	return result, err
}

func urlValues(values ...string) url.Values {
	result := url.Values{}
	for i := 0; i < len(values); i += 2 {
		result.Set(values[i], values[i+1])
	}
	return result
}

func permanentIdentityFailure(code string) bool {
	switch code {
	case "identity_invalid", "identity_mismatch", "identity_missing", "id_token_missing", "token_response_invalid", "scope_missing":
		return true
	default:
		return false
	}
}

func (m *Manager) requestJSON(ctx context.Context, method, endpoint string, form url.Values, target any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := m.client.Do(req)
	if err != nil {
		return &Error{Code: "network_error", Retryable: true, Message: "OpenAI authentication is temporarily unavailable. Try again."}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return &Error{Code: "network_error", Retryable: true, Message: "OpenAI authentication response was interrupted."}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response.StatusCode, data, response.Header.Get("x-request-id"))
	}
	if target == nil {
		return nil
	}
	if json.Unmarshal(data, target) != nil {
		return &Error{Code: "response_invalid", Message: "OpenAI returned invalid authentication data."}
	}
	return nil
}

var diagnosticPattern = regexp.MustCompile(`^[a-zA-Z0-9_.:/\[\]-]{1,160}$`)

func safeDiagnostic(value string) string {
	if diagnosticPattern.MatchString(value) {
		return value
	}
	return ""
}

func responseError(status int, data []byte, requestIDs ...string) *Error {
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Detail json.RawMessage `json:"detail"`
	}
	_ = json.Unmarshal(data, &envelope)
	var code, param string
	if json.Unmarshal(envelope.Error, &code) != nil {
		var detail struct {
			Code  string `json:"code"`
			Param string `json:"param"`
		}
		_ = json.Unmarshal(envelope.Error, &detail)
		code = detail.Code
		param = detail.Param
	}
	err := &Error{
		Code: safeDiagnostic(code), StatusCode: status, Param: safeDiagnostic(param),
		Retryable: status == 429 || status >= 500,
		Message:   "OpenAI rejected the request. Check the request configuration.",
		BodyShape: "other",
	}
	if len(requestIDs) > 0 {
		err.RequestID = safeDiagnostic(requestIDs[0])
	}
	if len(envelope.Error) > 0 {
		err.BodyShape = "error"
	} else if len(envelope.Detail) > 0 {
		err.BodyShape = "detail"
	}
	if err.Retryable {
		err.Message = "OpenAI is temporarily unavailable. Credentials were retained; retry later with bounded backoff."
	} else if status == 401 || status == 403 {
		err.Message = "OpenAI did not accept this registration's authorization. Check the selected account, granted permissions and serving-region policy."
	}
	switch err.Code {
	case "subscription_sharing_usage_limit_exceeded":
		err.Retryable = false
		err.Message = "ChatGPT plan usage for Multica has reached a limit. Check https://chatgpt.com/settings/usage; this may be an app-specific limit."
	case "subscription_sharing_user_not_eligible":
		err.Retryable = false
		err.Message = "ChatGPT plan usage is unavailable for the selected user, workspace or policy. Repeating sign-in will not resolve this restriction."
	case "subscription_sharing_unsupported_capability", "subscription_sharing_route_not_supported":
		err.Retryable = false
		err.Message = "ChatGPT does not support this request capability or route. Check the request configuration before retrying."
	case "chatpass_v2_scope_not_authorized", "chatpass_v2_invalid_authorization_context", "subscription_sharing_invalid_user":
		err.Retryable = false
		err.Message = "ChatGPT did not accept the selected registration's permission context. Check its identity and granted scopes."
	case "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable":
		err.Retryable = true
		err.Message = "ChatGPT usage or account information is temporarily unavailable. Credentials were retained; retry later with bounded backoff."
	case "invalid_client":
		err.Retryable = false
		err.Message = "OpenAI did not accept this client registration. Check its configuration before signing in again."
	case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused", "token_revoked":
		err.Retryable = false
		err.Message = "ChatGPT authorization must be renewed. Run multica chatgpt login --account with this registration's client ID."
	}
	return err
}

func (m *Manager) postToken(ctx context.Context, form url.Values) (tokenResponse, error) {
	var response tokenResponse
	err := m.requestJSON(ctx, http.MethodPost, Issuer+"/api/accounts/oauth/token", form, &response)
	return response, err
}

func (m *Manager) discover(ctx context.Context) (discovery, error) {
	var d discovery
	if err := m.requestJSON(ctx, http.MethodGet, Issuer+"/.well-known/openid-configuration", nil, &d); err != nil {
		return d, err
	}
	if d.Issuer != Issuer || !authEndpoint(d.JWKSURI) || !authEndpoint(d.RevocationEndpoint) {
		return d, &Error{Code: "discovery_invalid", Message: "OpenAI authentication discovery returned unexpected endpoints."}
	}
	return d, nil
}

func authEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && u.Scheme == "https" && u.Host == "auth.openai.com" && u.User == nil && u.Fragment == "" && u.Path != ""
}

type identityClaims struct {
	jwt.RegisteredClaims
	Nonce string `json:"nonce"`
	Email string `json:"email"`
}

func (m *Manager) validateIDToken(ctx context.Context, raw, clientID, nonce string) (*identityClaims, error) {
	d, err := m.discover(ctx)
	if err != nil {
		return nil, err
	}
	var keys struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err = m.requestJSON(ctx, http.MethodGet, d.JWKSURI, nil, &keys); err != nil {
		return nil, err
	}
	claims := new(identityClaims)
	_, err = jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		for _, key := range keys.Keys {
			if key.Kty != "RSA" || key.Kid != kid || kid == "" || (key.Alg != "" && key.Alg != "RS256") || (key.Use != "" && key.Use != "sig") {
				continue
			}
			n, err := base64.RawURLEncoding.DecodeString(key.N)
			if err != nil {
				return nil, err
			}
			e, err := base64.RawURLEncoding.DecodeString(key.E)
			if err != nil {
				return nil, err
			}
			exponent := new(big.Int).SetBytes(e)
			if len(n) < 256 || !exponent.IsInt64() || exponent.Int64() < 3 || exponent.Int64() > 1<<31 {
				return nil, errors.New("invalid JWKS key")
			}
			return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent.Int64())}, nil
		}
		return nil, errors.New("unknown signing key")
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(Issuer), jwt.WithAudience(clientID), jwt.WithExpirationRequired())
	if err != nil || claims.Subject == "" || (nonce != "" && subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) != 1) {
		return nil, &Error{Code: "identity_invalid", Message: "ChatGPT identity validation failed. Start sign-in again."}
	}
	return claims, nil
}

type LogoutResult struct {
	RevocationConfirmed bool
	Message             string
}

func (m *Manager) Logout(ctx context.Context, clientID string) (LogoutResult, error) {
	result := LogoutResult{RevocationConfirmed: true, Message: "Signed out of ChatGPT locally."}
	err := m.withLock(ctx, func(s *credentialStore) error {
		if clientID == "" {
			clientID = s.ActiveClientID
		}
		r := s.Registrations[clientID]
		pendingLogin := s.PendingLogins[clientID]
		if r == nil {
			if pendingLogin == nil {
				return nil
			}
			r = &registration{ClientID: clientID, Subject: pendingLogin.Subject, Label: pendingLogin.Label}
			s.Registrations[clientID] = r
		}
		refresh := r.RefreshToken
		if r.Pending != nil && r.Pending.Response.RefreshToken != "" {
			refresh = r.Pending.Response.RefreshToken
		}
		// The documented revocation flow accepts the renewable session's
		// refresh token. A short-lived grant can still contain usable access
		// or identity tokens without one; clearing those is only local logout.
		if refresh == "" && (r.AccessToken != "" || r.IDToken != "" || pendingHasTokens(r.Pending)) {
			result.RevocationConfirmed = false
		}
		refreshTokens := []string{}
		if pendingLogin != nil && pendingLogin.Pending != nil {
			if refresh := pendingLogin.Pending.Response.RefreshToken; refresh != "" {
				refreshTokens = append(refreshTokens, refresh)
			} else if pendingHasTokens(pendingLogin.Pending) {
				result.RevocationConfirmed = false
			}
		}
		if refresh != "" && (len(refreshTokens) == 0 || refreshTokens[0] != refresh) {
			refreshTokens = append(refreshTokens, refresh)
		}
		for _, refresh := range refreshTokens {
			confirmed := false
			for attempt := 0; attempt < 2; attempt++ {
				d, err := m.discover(ctx)
				if err == nil {
					err = m.requestJSON(ctx, http.MethodPost, d.RevocationEndpoint, urlValues("token", refresh, "token_type_hint", "refresh_token", "client_id", r.ClientID), nil)
				}
				if err == nil {
					confirmed = true
					break
				}
				var oauthErr *Error
				if !errors.As(err, &oauthErr) || !oauthErr.Retryable || attempt == 1 {
					break
				}
				select {
				case <-ctx.Done():
				case <-time.After(100 * time.Millisecond):
				}
			}
			result.RevocationConfirmed = result.RevocationConfirmed && confirmed
		}
		r.clear()
		delete(s.PendingLogins, clientID)
		if clientID == s.ActiveClientID {
			s.SelectionRevision = uuid.NewString()
		}
		if !result.RevocationConfirmed {
			result.Message = "Signed out locally. Remote revocation was not confirmed; disconnect Multica in https://chatgpt.com/settings if needed."
		}
		return m.save(s)
	})
	return result, err
}

func pendingHasTokens(pending *pendingTokens) bool {
	return pending != nil && (pending.Response.AccessToken != "" || pending.Response.RefreshToken != "" || pending.Response.IDToken != "")
}
