package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeLDAPAuthenticator is the directory this suite pretends to talk to. It
// records what the handler passed so a test can prove the submitted
// credentials were forwarded verbatim and appear nowhere in the response.
type fakeLDAPAuthenticator struct {
	mu    sync.Mutex
	calls []fakeLDAPCall
	user  auth.LDAPUser
	err   error
}

type fakeLDAPCall struct {
	Username string
	Password string
}

func (f *fakeLDAPAuthenticator) Authenticate(_ context.Context, username, password string) (auth.LDAPUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeLDAPCall{Username: username, Password: password})
	if f.err != nil {
		return auth.LDAPUser{}, f.err
	}
	return f.user, nil
}

func (f *fakeLDAPAuthenticator) lastCall() (fakeLDAPCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return fakeLDAPCall{}, false
	}
	return f.calls[len(f.calls)-1], true
}

// ldapMockDB stands in for the users table: GetUserByEmail answers with
// cannedUser when set and pgx.ErrNoRows otherwise (meaning "new user"), and
// CreateUser returns a fresh row. Any other statement panics, so the login
// path cannot quietly grow a query this suite has not modelled.
type ldapMockDB struct {
	db.DBTX

	cannedUser db.User
	created    db.User

	mu           sync.Mutex
	createCalls  int
	updateCalls  int
	createParams []db.CreateUserParams
	// last is the row currently "in the table": seeded from cannedUser, then
	// replaced by whatever CreateUser or UpdateUser returned.
	last db.User
}

func (m *ldapMockDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, `FROM "user"`) && strings.Contains(sql, "WHERE email"):
		if m.cannedUser.Email == "" {
			return &mockRow{err: pgx.ErrNoRows}
		}
		m.mu.Lock()
		m.last = m.cannedUser
		m.mu.Unlock()
		return newUserRow(m.cannedUser)

	case strings.Contains(sql, `INSERT INTO "user"`):
		var params db.CreateUserParams
		if len(args) >= 2 {
			params.Name, _ = args[0].(string)
			params.Email, _ = args[1].(string)
		}
		m.mu.Lock()
		m.createCalls++
		m.createParams = append(m.createParams, params)
		m.mu.Unlock()

		created := m.created
		if created.Email == "" {
			created.Email = params.Email
		}
		if created.Name == "" {
			created.Name = params.Name
		}
		if !created.ID.Valid {
			created.ID = pgtype.UUID{Bytes: [16]byte{0x11, 0x22, 0x33, 0x44}, Valid: true}
		}
		m.mu.Lock()
		m.last = created
		m.mu.Unlock()
		return newUserRow(created)

	case strings.Contains(sql, `UPDATE "user"`):
		// Generated statement is UPDATE ... SET name = COALESCE($2, name) ...,
		// so an empty argument leaves the stored value alone.
		m.mu.Lock()
		m.updateCalls++
		current := m.last
		if current.Email == "" {
			current = m.cannedUser
		}
		if len(args) >= 2 {
			if name, ok := args[1].(string); ok && name != "" {
				current.Name = name
			}
		}
		m.last = current
		m.mu.Unlock()
		return newUserRow(current)
	}

	panic("ldapMockDB: unexpected query: " + sql)
}

func (m *ldapMockDB) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (m *ldapMockDB) counts() (creates, updates int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createCalls, m.updateCalls
}

// userRow satisfies sqlc's positional Scan over the 14-column user projection.
type userRow struct {
	user db.User
	err  error
}

func newUserRow(u db.User) *userRow { return &userRow{user: u} }

func (r *userRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 14 {
		return errors.New("ldapMockDB: unexpected user column count")
	}
	// Column order mirrors the generated projection:
	// id, name, email, avatar_url, created_at, updated_at, onboarded_at,
	// onboarding_questionnaire, cloud_waitlist_email, cloud_waitlist_reason,
	// starter_content_state, language, profile_description, timezone.
	setUUID(dest[0], r.user.ID)
	setString(dest[1], r.user.Name)
	setString(dest[2], r.user.Email)
	setText(dest[3], r.user.AvatarUrl)
	for _, i := range []int{4, 5, 6} {
		setTimestamp(dest[i])
	}
	setBytes(dest[7])
	for _, i := range []int{8, 9, 10, 11} {
		setText(dest[i], pgtype.Text{})
	}
	setString(dest[12], "")
	setText(dest[13], pgtype.Text{})
	return nil
}

func setUUID(dest any, v pgtype.UUID) {
	if p, ok := dest.(*pgtype.UUID); ok {
		*p = v
	}
}

func setString(dest any, v string) {
	if p, ok := dest.(*string); ok {
		*p = v
	}
}

func setText(dest any, v pgtype.Text) {
	if p, ok := dest.(*pgtype.Text); ok {
		*p = v
	}
}

func setTimestamp(dest any) {
	if p, ok := dest.(*pgtype.Timestamptz); ok {
		*p = pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	}
}

func setBytes(dest any) {
	if p, ok := dest.(*[]byte); ok {
		*p = []byte("{}")
	}
}

// newLDAPHandler builds a Handler carrying only what a directory login
// touches. Deliberately not testHandler: this path is exercised with no
// database at all, so these tests run where Postgres is unavailable.
func newLDAPHandler(ldapAuth auth.LDAPAuthenticator, mock *ldapMockDB) *Handler {
	h := &Handler{cfg: Config{AllowSignup: false}}
	if mock != nil {
		h.Queries = db.New(mock)
	}
	h.LDAPAuth = ldapAuth
	return h
}

const ldapTestEmail = "alice@corp.example.com"

func ldapLoginRequest(body any) *http.Request {
	return testutil.JSONRequest(http.MethodPost, "/auth/ldap/login", body)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestLDAPLogin_Success is the mainline: a directory bind yields an email, and
// that email goes through the same findOrCreateUser + issueJWT pipeline the
// other two login front doors use.
func TestLDAPLogin_Success(t *testing.T) {
	dir := &fakeLDAPAuthenticator{user: auth.LDAPUser{Email: ldapTestEmail, DisplayName: "Alice Zhang"}}
	mock := &ldapMockDB{}
	h := newLDAPHandler(dir, mock)

	var out LoginResponse
	testutil.Call(t, h.LDAPLogin, ldapLoginRequest(LDAPLoginRequest{
		Username: "alice",
		Password: "correct-horse-battery-staple",
	})).Want(http.StatusOK).JSON(&out)

	if out.Token == "" {
		t.Error("token: want a signed JWT, got empty")
	}
	if out.User.Email != ldapTestEmail {
		t.Errorf("user.email: got %q, want %q", out.User.Email, ldapTestEmail)
	}
	call, ok := dir.lastCall()
	if !ok {
		t.Fatal("the directory was never consulted")
	}
	if call.Username != "alice" || call.Password != "correct-horse-battery-staple" {
		t.Errorf("directory call: got %+v, want the submitted credentials unchanged", call)
	}
	if creates, _ := mock.counts(); creates != 1 {
		t.Errorf("CreateUser calls: got %d, want 1 (a first login provisions the account)", creates)
	}
	if len(mock.createParams) == 1 && mock.createParams[0].Email != ldapTestEmail {
		t.Errorf("created account email: got %q, want %q", mock.createParams[0].Email, ldapTestEmail)
	}
}

// TestLDAPLogin_SetsAuthCookies asserts the HttpOnly session cookie the web
// client relies on, read off the real recorder.
func TestLDAPLogin_SetsAuthCookies(t *testing.T) {
	dir := &fakeLDAPAuthenticator{user: auth.LDAPUser{Email: ldapTestEmail}}
	h := newLDAPHandler(dir, &ldapMockDB{})

	req := ldapLoginRequest(LDAPLoginRequest{Username: "alice", Password: "pw"})
	res := testutil.Call(t, h.LDAPLogin, req).Want(http.StatusOK)

	// SetAuthCookies writes the HttpOnly auth cookie and its CSRF companion.
	// A response carrying neither means the caller authenticated but got no
	// session, which the bearer token in the body does not fix for a
	// cookie-auth (web) client.
	setCookies := res.Header().Values("Set-Cookie")
	joined := strings.Join(setCookies, "\n")
	if !strings.Contains(joined, auth.AuthCookieName+"=") {
		t.Fatalf("Set-Cookie: got %q, want the auth cookie the other login paths set", joined)
	}
	if !strings.Contains(joined, auth.CSRFCookieName+"=") {
		t.Errorf("Set-Cookie: got %q, want the paired CSRF cookie", joined)
	}
	var authCookie string
	for _, c := range setCookies {
		if strings.HasPrefix(c, auth.AuthCookieName+"=") {
			authCookie = c
		}
	}
	if !strings.Contains(authCookie, "HttpOnly") {
		t.Errorf("auth cookie is not HttpOnly: %q", authCookie)
	}
}

// TestLDAPLogin_SeedsDisplayNameFromDirectory covers the one piece of profile
// data the directory can contribute: a first-time account gets the directory's
// name rather than the email prefix, the way GoogleLogin uses the Google name.
func TestLDAPLogin_SeedsDisplayNameFromDirectory(t *testing.T) {
	dir := &fakeLDAPAuthenticator{user: auth.LDAPUser{Email: ldapTestEmail, DisplayName: "Alice Zhang"}}
	mock := &ldapMockDB{}
	h := newLDAPHandler(dir, mock)

	var out LoginResponse
	testutil.Call(t, h.LDAPLogin, ldapLoginRequest(LDAPLoginRequest{Username: "alice", Password: "pw"})).
		Want(http.StatusOK).JSON(&out)

	if out.User.Name != "Alice Zhang" {
		t.Errorf("user.name: got %q, want the directory's display name", out.User.Name)
	}
	if _, updates := mock.counts(); updates != 1 {
		t.Errorf("UpdateUser calls: got %d, want 1 (the rename on a freshly created account)", updates)
	}
}

// TestLDAPLogin_ExistingUserIsNotRecreated covers a returning directory user.
func TestLDAPLogin_ExistingUserIsNotRecreated(t *testing.T) {
	existing := db.User{
		ID:    pgtype.UUID{Bytes: [16]byte{0xaa, 0xbb}, Valid: true},
		Email: ldapTestEmail,
		Name:  "Already Here",
	}
	dir := &fakeLDAPAuthenticator{user: auth.LDAPUser{Email: ldapTestEmail, DisplayName: "Alice Zhang"}}
	mock := &ldapMockDB{cannedUser: existing}
	h := newLDAPHandler(dir, mock)

	var out LoginResponse
	testutil.Call(t, h.LDAPLogin, ldapLoginRequest(LDAPLoginRequest{Username: "alice", Password: "pw"})).
		Want(http.StatusOK).JSON(&out)

	if creates, _ := mock.counts(); creates != 0 {
		t.Errorf("CreateUser calls: got %d, want 0 (the account already exists)", creates)
	}
	if out.User.Email != ldapTestEmail {
		t.Errorf("user.email: got %q, want the existing account", out.User.Email)
	}
}

// TestLDAPLogin_IgnoresSignupGate is decision D3 as an executable claim: this
// path must not consult ALLOW_SIGNUP / ALLOWED_EMAILS / ALLOWED_EMAIL_DOMAINS.
// The handler is configured with signup closed and an allowlist that does not
// match, so 200 proves the gate was skipped. A 403 here would mean a
// directory-authenticated employee was told to ask an admin for a whitelist
// entry, which is the failure the plan called out.
func TestLDAPLogin_IgnoresSignupGate(t *testing.T) {
	dir := &fakeLDAPAuthenticator{user: auth.LDAPUser{Email: "someone-outside@corp.example.com"}}
	mock := &ldapMockDB{}
	h := newLDAPHandler(dir, mock)
	h.cfg = Config{
		AllowSignup:         false,
		AllowedEmails:       []string{"listed@other.example"},
		AllowedEmailDomains: []string{"other.example"},
	}

	testutil.Call(t, h.LDAPLogin, ldapLoginRequest(LDAPLoginRequest{Username: "someone", Password: "pw"})).
		Want(http.StatusOK)

	if creates, _ := mock.counts(); creates != 1 {
		t.Errorf("CreateUser calls: got %d, want 1 (the signup gate must not have blocked provisioning)", creates)
	}
}

// TestFindOrCreateUserGateKeepsSignupGate protects the two existing login
// paths against the refactor that lets LDAP bypass the gate.
func TestFindOrCreateUserGateKeepsSignupGate(t *testing.T) {
	h := newLDAPHandler(nil, &ldapMockDB{})
	h.cfg = Config{AllowSignup: false}

	if _, _, err := h.findOrCreateUser(context.Background(), "new@blocked.example"); err == nil {
		t.Fatal("findOrCreateUser must keep applying the signup gate for the existing entry points")
	}
	// The same handler with an explicit nil gate skips it, which is what
	// LDAPLogin calls.
	if _, _, err := h.findOrCreateUserGate(context.Background(), "new@blocked.example", nil); err != nil {
		t.Fatalf("findOrCreateUserGate with a nil gate must skip the check, got %v", err)
	}
}

// TestLDAPLogin_InvalidCredentials: the two outcomes a user could otherwise be
// told about collapse into one response, so the endpoint cannot enumerate
// directory accounts.
func TestLDAPLogin_InvalidCredentials(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dirErr error
	}{
		{"wrong password", auth.ErrLDAPInvalidCredentials},
		{"unknown user", auth.ErrLDAPInvalidCredentials},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newLDAPHandler(&fakeLDAPAuthenticator{err: tc.dirErr}, &ldapMockDB{})
			res := testutil.Call(t, h.LDAPLogin, ldapLoginRequest(LDAPLoginRequest{
				Username: "alice", Password: "wrong",
			})).Want(http.StatusUnauthorized)

			var body map[string]any
			res.JSON(&body)
			msg, _ := body["error"].(string)
			if !strings.Contains(strings.ToLower(msg), "invalid username or password") {
				t.Errorf("error: got %q, want the wording that does not say which half failed", msg)
			}
			if strings.Contains(strings.ToLower(msg), "ldap") {
				t.Errorf("error %q leaks directory detail", msg)
			}
		})
	}
}

// TestLDAPLogin_DirectoryUnavailable keeps an outage distinct from a bad
// password: telling someone their credentials failed during an LDAP outage
// sends them to the wrong help desk.
func TestLDAPLogin_DirectoryUnavailable(t *testing.T) {
	h := newLDAPHandler(&fakeLDAPAuthenticator{err: auth.ErrLDAPUnavailable}, &ldapMockDB{})
	res := testutil.Call(t, h.LDAPLogin, ldapLoginRequest(LDAPLoginRequest{
		Username: "alice", Password: "pw",
	})).Want(http.StatusBadGateway)

	if strings.Contains(strings.ToLower(res.Text()), "password") {
		t.Errorf("body %q blames the caller's password for a directory outage", res.Text())
	}
}

// TestLDAPLogin_UnexpectedAuthenticatorError: an unclassifiable failure must
// not become a 401, which would read to the user as "wrong password".
func TestLDAPLogin_UnexpectedAuthenticatorError(t *testing.T) {
	h := newLDAPHandler(&fakeLDAPAuthenticator{err: errors.New("some new directory failure")}, &ldapMockDB{})
	testutil.Call(t, h.LDAPLogin, ldapLoginRequest(LDAPLoginRequest{
		Username: "alice", Password: "pw",
	})).Want(http.StatusBadGateway)
}

// TestLDAPLogin_Disabled: LDAP_ENABLED=false leaves Handler.LDAPAuth nil, and
// every request to the route has to say so without touching a database.
func TestLDAPLogin_Disabled(t *testing.T) {
	h := newLDAPHandler(nil, &ldapMockDB{})
	res := testutil.Call(t, h.LDAPLogin, ldapLoginRequest(LDAPLoginRequest{
		Username: "alice", Password: "pw",
	})).Want(http.StatusServiceUnavailable)

	if !strings.Contains(strings.ToLower(res.Text()), "not configured") {
		t.Errorf("body %q, want it to name the missing configuration", res.Text())
	}
}

func TestLDAPLogin_MissingCredentials(t *testing.T) {
	for _, tc := range []struct {
		name string
		body any
	}{
		{"whitespace username", LDAPLoginRequest{Username: "   ", Password: "pw"}},
		{"empty password", LDAPLoginRequest{Username: "alice", Password: ""}},
		{"neither field", LDAPLoginRequest{}},
		{"no fields at all", map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := &fakeLDAPAuthenticator{user: auth.LDAPUser{Email: ldapTestEmail}}
			h := newLDAPHandler(dir, &ldapMockDB{})
			testutil.Call(t, h.LDAPLogin, ldapLoginRequest(tc.body)).Want(http.StatusBadRequest)
			if _, called := dir.lastCall(); called {
				t.Error("a request with no credentials must not reach the directory at all")
			}
		})
	}
}

// TestLDAPLogin_MalformedBody: a body that is not valid JSON is a client
// error, decided before the directory is contacted.
func TestLDAPLogin_MalformedBody(t *testing.T) {
	dir := &fakeLDAPAuthenticator{user: auth.LDAPUser{Email: ldapTestEmail}}
	h := newLDAPHandler(dir, &ldapMockDB{})
	req := testutil.JSONRequest(http.MethodPost, "/auth/ldap/login", "{not json")
	testutil.Call(t, h.LDAPLogin, req).Want(http.StatusBadRequest)
	if _, called := dir.lastCall(); called {
		t.Error("a malformed body must not reach the directory")
	}
}

// TestLDAPLogin_TemporarilyDisabledUser reuses the same guard VerifyCode and
// GoogleLogin apply, reached through the shared helpers rather than a copy.
func TestLDAPLogin_TemporarilyDisabledUser(t *testing.T) {
	// The denylist lives in auth.temporarilyDisabledUserEmails; a listed
	// account must be refused even with a valid directory bind, and refused
	// with the same message every other login path uses.
	const disabledEmail = "pdzzer68@embassybase.com"
	if !auth.IsTemporarilyDisabledUserEmail(disabledEmail) {
		t.Skip("denylist entry rotated out of auth.temporarilyDisabledUserEmails; update this test's address")
	}
	dir := &fakeLDAPAuthenticator{user: auth.LDAPUser{Email: disabledEmail}}
	h := newLDAPHandler(dir, &ldapMockDB{})

	res := testutil.Call(t, h.LDAPLogin, ldapLoginRequest(LDAPLoginRequest{
		Username: "blocked", Password: "pw",
	})).Want(http.StatusForbidden)

	if !strings.Contains(res.Text(), auth.TemporarilyDisabledUserError) {
		t.Errorf("body %q, want the shared account-disabled message", res.Text())
	}
}

// TestLDAPLogin_ResponseShape pins the contract the frontend schema validates,
// and checks the password never comes back.
func TestLDAPLogin_ResponseShape(t *testing.T) {
	dir := &fakeLDAPAuthenticator{user: auth.LDAPUser{Email: ldapTestEmail, DisplayName: "Alice Zhang"}}
	h := newLDAPHandler(dir, &ldapMockDB{})

	res := testutil.Call(t, h.LDAPLogin, ldapLoginRequest(LDAPLoginRequest{
		Username: "alice", Password: "secretpw",
	})).Want(http.StatusOK)

	var body map[string]any
	res.JSON(&body)
	for key := range body {
		if key != "token" && key != "user" {
			t.Errorf("unexpected top-level key %q, want the existing LoginResponse shape", key)
		}
	}
	if strings.Contains(res.Text(), "secretpw") {
		t.Error("response echoed the password")
	}
}

// ---------------------------------------------------------------------------
// /api/config plumbing (Task 4)
// ---------------------------------------------------------------------------

// TestGetConfigLdapEnabled follows the same omitted-when-false convention as
// vcs_integration_available, so the managed cloud and older clients keep the
// response shape they already parse.
func TestGetConfigLdapEnabled(t *testing.T) {
	fetch := func(h *Handler) map[string]json.RawMessage {
		t.Helper()
		req := testutil.JSONRequest(http.MethodGet, "/api/config", nil)
		res := testutil.Call(t, h.GetConfig, req).Want(http.StatusOK)
		var out map[string]json.RawMessage
		res.JSON(&out)
		return out
	}

	if body := fetch(newLDAPHandler(nil, nil)); body["ldap_enabled"] != nil {
		t.Errorf("ldap_enabled: got %s, want the key omitted when no directory is configured", body["ldap_enabled"])
	}

	body := fetch(newLDAPHandler(&fakeLDAPAuthenticator{}, nil))
	if got := string(body["ldap_enabled"]); got != "true" {
		t.Errorf("ldap_enabled: got %q, want true when a directory is configured", got)
	}
}
