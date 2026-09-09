package auth

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	ldap "github.com/go-ldap/ldap/v3"
)

// ---------------------------------------------------------------------------
// Fake directory
// ---------------------------------------------------------------------------

// fakeLDAPConn implements the same ldapConn slice the production flow uses, so
// every branch of Authenticate is exercised against a fake directory rather
// than a live OpenLDAP.
type fakeLDAPConn struct {
	mu sync.Mutex

	serviceBindErr error
	// searchResult is what a lookup returns; searchErr takes precedence.
	searchResult *ldap.SearchResult
	searchErr    error
	// userBindErr applies to the second bind, i.e. the requester's password.
	userBindErr error

	binds    []fakeBind
	searches []*ldap.SearchRequest
	closed   int
	hang     time.Duration
}

type fakeBind struct {
	DN       string
	Password string
}

func (f *fakeLDAPConn) Bind(username, password string) error {
	f.mu.Lock()
	f.binds = append(f.binds, fakeBind{DN: username, Password: password})
	service := len(f.binds) == 1
	hang := f.hang
	f.mu.Unlock()

	if hang > 0 {
		time.Sleep(hang)
	}
	if service {
		return f.serviceBindErr
	}
	return f.userBindErr
}

func (f *fakeLDAPConn) Search(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
	f.mu.Lock()
	f.searches = append(f.searches, req)
	f.mu.Unlock()
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.searchResult, nil
}

func (f *fakeLDAPConn) SetTimeout(time.Duration) {}

func (f *fakeLDAPConn) Close() error {
	f.mu.Lock()
	f.closed++
	f.mu.Unlock()
	return nil
}

func (f *fakeLDAPConn) lastSearch() *ldap.SearchRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.searches) == 0 {
		return nil
	}
	return f.searches[len(f.searches)-1]
}

func (f *fakeLDAPConn) bindCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.binds)
}

func (f *fakeLDAPConn) bindAt(i int) fakeBind {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.binds[i]
}

func (f *fakeLDAPConn) closedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// entry builds one directory row with the attributes the flow reads.
func entry(dn, emailAttr, email, nameAttr, name string) *ldap.Entry {
	attrs := []*ldap.EntryAttribute{{Name: emailAttr, Values: []string{email}}}
	if nameAttr != "" {
		attrs = append(attrs, &ldap.EntryAttribute{Name: nameAttr, Values: []string{name}})
	}
	return &ldap.Entry{DN: dn, Attributes: attrs}
}

// aliceEntry builds the row a default-configured directory returns for
// "alice". The config is normalised first so the attribute names are the ones
// the flow will actually ask for.
func aliceEntry(cfg LDAPConfig) *ldap.Entry {
	normalized := normalizeLDAPConfig(cfg)
	return entry(
		aliceDN,
		normalized.EmailAttr, "Alice@Corp.Example.com",
		normalized.NameAttr, "Alice Zhang",
	)
}

func aliceResult(cfg LDAPConfig) *ldap.SearchResult {
	return searchResult(aliceEntry(cfg))
}

func searchResult(rows ...*ldap.Entry) *ldap.SearchResult {
	return &ldap.SearchResult{Entries: rows}
}

// newFakeAuthenticator returns an authenticator wired to conn (created when
// the caller passes nil) plus the config it normalised, so a test can state
// the attribute names it expects back.
func newFakeAuthenticator(cfg LDAPConfig, conn *fakeLDAPConn, dialErr error) (*ldapAuthenticator, *fakeLDAPConn, LDAPConfig) {
	if conn == nil {
		conn = &fakeLDAPConn{}
	}
	normalized := normalizeLDAPConfig(cfg)
	a := &ldapAuthenticator{cfg: normalized, dial: func(context.Context, LDAPConfig) (ldapConn, error) {
		if dialErr != nil {
			return nil, dialErr
		}
		return conn, nil
	}}
	return a, conn, normalized
}

// baseTestConfig is a complete, plausible OpenLDAP configuration. Each test
// overrides the one field whose failure it means to produce, so nothing else
// is in play.
func baseTestConfig() LDAPConfig {
	return LDAPConfig{
		Enabled:      true,
		URL:          "ldap://openldap.corp.example.com:389",
		BaseDN:       "ou=people,dc=corp,dc=example,dc=com",
		BindDN:       "uid=multica-svc,ou=service,dc=corp,dc=example,dc=com",
		BindPassword: "svc-secret",
	}
}

const aliceDN = "uid=alice,ou=people,dc=corp,dc=example,dc=com"

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAuthenticate_Success(t *testing.T) {
	cfg := baseTestConfig()
	conn := &fakeLDAPConn{searchResult: aliceResult(cfg)}
	a, conn, _ := newFakeAuthenticator(cfg, conn, nil)

	user, err := a.Authenticate(context.Background(), "alice", "correct-password")
	if err != nil {
		t.Fatalf("Authenticate: unexpected error %v", err)
	}
	// Lowercased: users.email is the match key and the lookup is exact.
	if user.Email != "alice@corp.example.com" {
		t.Errorf("Email: got %q, want %q", user.Email, "alice@corp.example.com")
	}
	if user.DisplayName != "Alice Zhang" {
		t.Errorf("DisplayName: got %q, want %q", user.DisplayName, "Alice Zhang")
	}

	if got := conn.bindCount(); got != 2 {
		t.Fatalf("binds: got %d, want 2 (service account, then the user)", got)
	}
	if first := conn.bindAt(0); first.DN != cfg.BindDN || first.Password != cfg.BindPassword {
		t.Errorf("first bind: got %q, want the service account", first.DN)
	}
	second := conn.bindAt(1)
	if second.DN != aliceDN || second.Password != "correct-password" {
		t.Errorf("second bind: got DN %q, want the DN the directory returned, bound with the supplied password", second.DN)
	}
	if got := conn.closedCount(); got != 1 {
		t.Errorf("connections closed: got %d, want 1", got)
	}
}

// TestAuthenticate_WrongPassword covers the second bind failing with the
// directory's own invalid-credentials result code.
func TestAuthenticate_WrongPassword(t *testing.T) {
	cfg := baseTestConfig()
	conn := &fakeLDAPConn{
		searchResult: aliceResult(cfg),
		userBindErr:  ldap.NewError(ldap.LDAPResultInvalidCredentials, errors.New("invalid credentials")),
	}
	a, conn, _ := newFakeAuthenticator(cfg, conn, nil)

	if _, err := a.Authenticate(context.Background(), "alice", "wrong-password"); !errors.Is(err, ErrLDAPInvalidCredentials) {
		t.Fatalf("err: got %v, want ErrLDAPInvalidCredentials", err)
	}
	if got := conn.bindCount(); got != 2 {
		t.Errorf("binds: got %d, want 2 (the search did find the user)", got)
	}
}

// TestAuthenticate_UserNotFound asserts the same sentinel as a wrong password:
// the endpoint must not reveal whether a username exists.
func TestAuthenticate_UserNotFound(t *testing.T) {
	cfg := baseTestConfig()
	conn := &fakeLDAPConn{searchResult: searchResult()}
	a, conn, _ := newFakeAuthenticator(cfg, conn, nil)

	if _, err := a.Authenticate(context.Background(), "ghost", "any-password"); !errors.Is(err, ErrLDAPInvalidCredentials) {
		t.Fatalf("err: got %v, want ErrLDAPInvalidCredentials", err)
	}
	if got := conn.bindCount(); got != 1 {
		t.Errorf("binds: got %d, want 1 (never bind as a DN we did not find)", got)
	}
}

// TestAuthenticate_ServiceAccountBindFailure separates a broken deployment
// from a broken password, since the handler maps them to 5xx and 401.
func TestAuthenticate_ServiceAccountBindFailure(t *testing.T) {
	cfg := baseTestConfig()
	conn := &fakeLDAPConn{
		serviceBindErr: ldap.NewError(ldap.LDAPResultInvalidCredentials, errors.New("invalid credentials")),
	}
	a, conn, _ := newFakeAuthenticator(cfg, conn, nil)

	if _, err := a.Authenticate(context.Background(), "alice", "correct-password"); !errors.Is(err, ErrLDAPUnavailable) {
		t.Fatalf("err: got %v, want ErrLDAPUnavailable", err)
	}
	if conn.lastSearch() != nil {
		t.Error("a failed service-account bind must not go on to search")
	}
}

func TestAuthenticate_DialFailure(t *testing.T) {
	cfg := baseTestConfig()
	a, _, _ := newFakeAuthenticator(cfg, nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")})

	if _, err := a.Authenticate(context.Background(), "alice", "pw"); !errors.Is(err, ErrLDAPUnavailable) {
		t.Fatalf("err: got %v, want ErrLDAPUnavailable", err)
	}
}

func TestAuthenticate_SearchError(t *testing.T) {
	cfg := baseTestConfig()
	conn := &fakeLDAPConn{searchErr: ldap.NewError(ldap.LDAPResultOperationsError, errors.New("server problem"))}
	a, _, _ := newFakeAuthenticator(cfg, conn, nil)

	if _, err := a.Authenticate(context.Background(), "alice", "pw"); !errors.Is(err, ErrLDAPUnavailable) {
		t.Fatalf("err: got %v, want ErrLDAPUnavailable", err)
	}
}

// TestAuthenticate_FilterInjectionEscaped has to read the outgoing filter: the
// defence is that metacharacters survive as literals, so a username cannot
// rewrite `(uid=<input>)` into a query that matches a broader set.
func TestAuthenticate_FilterInjectionEscaped(t *testing.T) {
	cfg := baseTestConfig()
	conn := &fakeLDAPConn{searchResult: searchResult()}
	a, conn, _ := newFakeAuthenticator(cfg, conn, nil)

	const hostile = `alice)(mail=@corp.com`
	if _, err := a.Authenticate(context.Background(), hostile, "pw"); !errors.Is(err, ErrLDAPInvalidCredentials) {
		t.Fatalf("err: got %v, want ErrLDAPInvalidCredentials", err)
	}

	req := conn.lastSearch()
	if req == nil {
		t.Fatal("no search was issued")
	}
	// RFC 4515 escapes * ( ) \ and non-ASCII, leaving @ and = alone. So the
	// two injected parens come back as \29 and \28 and the filter keeps the
	// template's single pair.
	want := `(uid=alice\29\28mail=@corp.com)`
	if req.Filter != want {
		t.Errorf("filter: got %q, want the escaped literal %q", req.Filter, want)
	}
	// Belt and braces on the property that matters: a surviving wildcard would
	// match every entry in the subtree, and an extra paren pair would close
	// the template early and let the rest become a second assertion.
	if strings.Contains(req.Filter, "*") {
		t.Errorf("filter %q leaked a wildcard", req.Filter)
	}
	if n := strings.Count(req.Filter, "("); n != 1 {
		t.Errorf("filter %q has %d opening parens, want only the template's own 1", req.Filter, n)
	}
}

// TestAuthenticate_UserFilterIsConfigurable guards the requirement that the
// code is not OpenLDAP-specific: an AD-style filter works by config alone.
func TestAuthenticate_UserFilterIsConfigurable(t *testing.T) {
	cfg := baseTestConfig()
	cfg.UserFilter = "(sAMAccountName=%s)"
	conn := &fakeLDAPConn{searchResult: searchResult()}
	a, conn, _ := newFakeAuthenticator(cfg, conn, nil)

	_, _ = a.Authenticate(context.Background(), "alice", "pw")
	req := conn.lastSearch()
	if req == nil || !strings.HasPrefix(req.Filter, "(sAMAccountName=") {
		t.Fatalf("filter: got %+v, want the configured template used verbatim", req)
	}
}

// TestAuthenticate_AttrNamesAreConfigurable is the same requirement for the
// attributes: a schema without mail/displayName needs config, not code.
func TestAuthenticate_AttrNamesAreConfigurable(t *testing.T) {
	cfg := baseTestConfig()
	cfg.EmailAttr = "userEmail"
	cfg.NameAttr = "cn"
	conn := &fakeLDAPConn{searchResult: searchResult(entry(aliceDN, "userEmail", "alice@corp.example.com", "cn", "Alice Z"))}
	a, _, normalized := newFakeAuthenticator(cfg, conn, nil)

	user, err := a.Authenticate(context.Background(), "alice", "pw")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if user.Email != "alice@corp.example.com" || user.DisplayName != "Alice Z" {
		t.Errorf("got %+v, want the configured attributes read", user)
	}
	req := conn.lastSearch()
	if req == nil || len(req.Attributes) != 2 || req.Attributes[0] != normalized.EmailAttr {
		t.Errorf("requested attributes: got %v, want the configured pair", req)
	}
}

func TestAuthenticate_MissingPlaceholderInFilter(t *testing.T) {
	cfg := baseTestConfig()
	cfg.UserFilter = "(uid=alice)" // operator typo: nowhere to put the username
	conn := &fakeLDAPConn{searchResult: searchResult()}
	a, conn, _ := newFakeAuthenticator(cfg, conn, nil)

	if _, err := a.Authenticate(context.Background(), "alice", "pw"); !errors.Is(err, ErrLDAPUnavailable) {
		t.Fatalf("err: got %v, want ErrLDAPUnavailable", err)
	}
	if conn.lastSearch() != nil {
		t.Error("a filter with nowhere to put the username must not be searched with")
	}
}

// TestAuthenticate_MultipleMatches pins the refusal to pick a winner: with two
// rows for one username, a correct password would unlock whichever entry the
// directory happened to return first.
func TestAuthenticate_MultipleMatches(t *testing.T) {
	cfg := baseTestConfig()
	conn := &fakeLDAPConn{searchResult: searchResult(
		aliceEntry(cfg),
		entry("uid=alice2,ou=people,dc=corp,dc=example,dc=com", "mail", "a2@corp.example.com", "displayName", "Alice Two"),
	)}
	a, conn, _ := newFakeAuthenticator(cfg, conn, nil)

	if _, err := a.Authenticate(context.Background(), "alice", "pw"); !errors.Is(err, ErrLDAPInvalidCredentials) {
		t.Fatalf("err: got %v, want ErrLDAPInvalidCredentials", err)
	}
	if got := conn.bindCount(); got != 1 {
		t.Errorf("binds: got %d, want 1 (an ambiguous match must not be bound as)", got)
	}
	// SizeLimit 2 is what makes the ambiguity visible at all: a limit of 1
	// would return one row and the flow would log in the wrong account.
	if req := conn.lastSearch(); req == nil || req.SizeLimit != 2 {
		t.Errorf("search size limit: got %+v, want 2 so a duplicate username is detectable", req)
	}
}

// TestAuthenticate_MissingEmailAttr treats "the directory row has no mail" as
// the operator configuration fault it almost always is.
func TestAuthenticate_MissingEmailAttr(t *testing.T) {
	cfg := baseTestConfig()
	conn := &fakeLDAPConn{searchResult: searchResult(entry(aliceDN, "telephoneNumber", "+8613800000000", "displayName", "Alice"))}
	a, _, _ := newFakeAuthenticator(cfg, conn, nil)

	if _, err := a.Authenticate(context.Background(), "alice", "pw"); !errors.Is(err, ErrLDAPUnavailable) {
		t.Fatalf("err: got %v, want ErrLDAPUnavailable", err)
	}
}

// TestAuthenticate_AttributeNameCaseInsensitive covers directories that echo
// back `Mail` for a requested `mail` (attribute names are case-insensitive).
func TestAuthenticate_AttributeNameCaseInsensitive(t *testing.T) {
	cfg := baseTestConfig()
	conn := &fakeLDAPConn{searchResult: searchResult(entry(aliceDN, "Mail", "alice@corp.example.com", "DisplayName", "Alice"))}
	a, _, _ := newFakeAuthenticator(cfg, conn, nil)

	user, err := a.Authenticate(context.Background(), "alice", "pw")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if user.Email != "alice@corp.example.com" || user.DisplayName != "Alice" {
		t.Errorf("got %+v, want the attributes read regardless of casing", user)
	}
}

// TestAuthenticate_Timeout asserts the caller stops waiting: a directory that
// never answers must not pin a request goroutine, or the handler above it.
func TestAuthenticate_Timeout(t *testing.T) {
	cfg := baseTestConfig()
	cfg.Timeout = 50 * time.Millisecond
	conn := &fakeLDAPConn{hang: 300 * time.Millisecond, searchResult: aliceResult(cfg)}
	a, conn, _ := newFakeAuthenticator(cfg, conn, nil)

	start := time.Now()
	_, err := a.Authenticate(context.Background(), "alice", "pw")
	elapsed := time.Since(start)

	if !errors.Is(err, ErrLDAPUnavailable) {
		t.Fatalf("err: got %v, want ErrLDAPUnavailable", err)
	}
	if elapsed > time.Second {
		t.Errorf("elapsed %v: Authenticate did not honour the timeout", elapsed)
	}
	// The abandoned goroutine still has to release the connection, or every
	// slow login would leak one directory connection.
	deadline := time.Now().Add(2 * time.Second)
	for conn.closedCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := conn.closedCount(); got != 1 {
		t.Errorf("connections closed: got %d, want 1", got)
	}
}

// TestAuthenticate_RejectsEmptyCredentials closes the anonymous-bind hole: an
// empty password against a service-account-capable directory authenticates
// nobody, and must not cost a round trip to find that out.
func TestAuthenticate_RejectsEmptyCredentials(t *testing.T) {
	cfg := baseTestConfig()
	for _, tc := range []struct{ name, username, password string }{
		{"empty username", "", "pw"},
		{"empty password", "alice", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dialed := false
			a, _, _ := newFakeAuthenticator(cfg, nil, nil)
			a.dial = func(context.Context, LDAPConfig) (ldapConn, error) {
				dialed = true
				return &fakeLDAPConn{}, nil
			}
			if _, err := a.Authenticate(context.Background(), tc.username, tc.password); !errors.Is(err, ErrLDAPInvalidCredentials) {
				t.Fatalf("err: got %v, want ErrLDAPInvalidCredentials", err)
			}
			if dialed {
				t.Error("empty credentials must be rejected before contacting the directory")
			}
		})
	}
}

// TestAuthenticate_AbandonsCallerContext is about the flow rather than the
// configured deadline: a cancelled request returns promptly even mid-exchange.
func TestAuthenticate_AbandonsCallerContext(t *testing.T) {
	cfg := baseTestConfig()
	cfg.Timeout = 10 * time.Second
	conn := &fakeLDAPConn{hang: 300 * time.Millisecond, searchResult: aliceResult(cfg)}
	a, _, _ := newFakeAuthenticator(cfg, conn, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	if _, err := a.Authenticate(ctx, "alice", "pw"); !errors.Is(err, ErrLDAPUnavailable) {
		t.Fatalf("err: got %v, want ErrLDAPUnavailable", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("elapsed %v: a cancelled request was not abandoned promptly", elapsed)
	}
}

// TestAuthenticate_MisconfigurationIsNotACredentialFailure keeps the two 5xx
// causes distinct from 401, so an operator's typo never tells a user their
// password is wrong.
func TestAuthenticate_MisconfigurationIsNotACredentialFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spoil func(*LDAPConfig)
	}{
		{"no URL", func(c *LDAPConfig) { c.URL = "" }},
		{"no base DN", func(c *LDAPConfig) { c.BaseDN = "" }},
		{"no bind DN", func(c *LDAPConfig) { c.BindDN = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseTestConfig()
			tc.spoil(&cfg)
			conn := &fakeLDAPConn{searchResult: aliceResult(cfg)}
			a, conn, _ := newFakeAuthenticator(cfg, conn, nil)

			if _, err := a.Authenticate(context.Background(), "alice", "pw"); !errors.Is(err, ErrLDAPUnavailable) {
				t.Fatalf("err: got %v, want ErrLDAPUnavailable", err)
			}
			if got := conn.bindCount(); got != 0 {
				t.Errorf("binds: got %d, want 0 (must not contact a half-configured directory)", got)
			}
		})
	}
}

func TestNewLDAPAuthenticatorAppliesDefaults(t *testing.T) {
	a, ok := NewLDAPAuthenticator(LDAPConfig{Enabled: true}).(*ldapAuthenticator)
	if !ok {
		t.Fatalf("NewLDAPAuthenticator returned %T, want *ldapAuthenticator", a)
	}
	if a.cfg.UserFilter != "(uid=%s)" {
		t.Errorf("UserFilter: got %q, want the OpenLDAP default", a.cfg.UserFilter)
	}
	if a.cfg.EmailAttr != "mail" {
		t.Errorf("EmailAttr: got %q, want mail", a.cfg.EmailAttr)
	}
	if a.cfg.NameAttr != "displayName" {
		t.Errorf("NameAttr: got %q, want displayName", a.cfg.NameAttr)
	}
	if a.cfg.Timeout != DefaultLDAPTimeout {
		t.Errorf("Timeout: got %v, want %v", a.cfg.Timeout, DefaultLDAPTimeout)
	}
	if a.dial == nil {
		t.Error("dial must default to the real dialer")
	}
}

// TestNewLDAPAuthenticatorReturnsInterface is the compile-time proof that the
// constructor satisfies the exported contract the handler depends on.
func TestNewLDAPAuthenticatorReturnsInterface(t *testing.T) {
	var a LDAPAuthenticator = NewLDAPAuthenticator(baseTestConfig())
	if a == nil {
		t.Fatal("NewLDAPAuthenticator returned nil")
	}
}

// TestTlsConfigForKeepsVerificationOnByDefault is the point of making
// LDAP_TLS_INSECURE an opt-in flag rather than a default.
func TestTlsConfigForKeepsVerificationOnByDefault(t *testing.T) {
	if cfg := tlsConfigFor(LDAPConfig{}); cfg != nil {
		t.Errorf("default: got %+v, want nil so go-ldap verifies the chain and hostname", cfg)
	}
	cfg := tlsConfigFor(LDAPConfig{TLSInsecure: true})
	if cfg == nil || !cfg.InsecureSkipVerify {
		t.Errorf("TLSInsecure=true: got %+v, want InsecureSkipVerify", cfg)
	}
}

// TestDialLDAPConnRejectsMissingURL covers the cheapest misconfiguration: an
// enabled integration with no directory to talk to.
func TestDialLDAPConnRejectsMissingURL(t *testing.T) {
	if _, err := dialLDAPConn(context.Background(), LDAPConfig{}); err == nil {
		t.Fatal("dialing with an empty URL must fail")
	}
}

// TestDialLDAPConnFailsFastOnClosedPort proves a dead directory produces an
// error the flow maps to unavailable rather than a hang, and that the real
// dialer (not the injected fake) is what the constructor wires up.
func TestDialLDAPConnFailsFastOnClosedPort(t *testing.T) {
	cfg := normalizeLDAPConfig(LDAPConfig{URL: "ldap://" + deadLoopbackAddr(t), Timeout: 2 * time.Second})
	start := time.Now()
	_, err := dialLDAPConn(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected a dial error on a port with no listener")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("dial took %v, want a fast failure", elapsed)
	}

	a := NewLDAPAuthenticator(cfg)
	if _, err := a.Authenticate(context.Background(), "alice", "pw"); !errors.Is(err, ErrLDAPUnavailable) {
		t.Fatalf("err: got %v, want ErrLDAPUnavailable", err)
	}
}

// deadLoopbackAddr returns a host:port that only just had a listener on it, so
// a connect attempt gets an immediate refusal on every platform instead of
// depending on what happens to occupy a well-known port.
func deadLoopbackAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot open a loopback listener: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}
