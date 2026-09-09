package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"

	ldap "github.com/go-ldap/ldap/v3"
)

// ErrLDAPInvalidCredentials is returned when the directory rejects the
// username/password pair, or when no entry matches the username. Both cases
// collapse into one sentinel on purpose: distinguishing "no such user" from
// "wrong password" would turn the login endpoint into a username oracle.
var ErrLDAPInvalidCredentials = errors.New("ldap: invalid credentials")

// ErrLDAPUnavailable is returned when the directory itself could not be
// reached or is misconfigured (dial failure, service-account bind failure,
// timeout). It is a server-side condition, so the handler maps it to a 5xx
// instead of implying the user typed their password wrong.
var ErrLDAPUnavailable = errors.New("ldap: directory service unavailable")

// DefaultLDAPTimeout is the per-attempt budget applied when LDAPConfig.Timeout
// is unset, matching the LDAP_TIMEOUT default documented in .env.example.
const DefaultLDAPTimeout = 5 * time.Second

// LDAPConfig mirrors the LDAP_* environment variables. Every field is
// deployment-owned (never request-owned) and resolved once at boot.
type LDAPConfig struct {
	Enabled      bool
	URL          string // e.g. ldaps://openldap.corp.example.com:636
	BaseDN       string // subtree the user search starts from
	BindDN       string // service account; read/search permissions only
	BindPassword string
	// UserFilter is a search filter template with one %s placeholder,
	// substituted with the RFC 4515-escaped username. OpenLDAP conventionally
	// uses (uid=%s); AD would use (sAMAccountName=%s). Nothing here is
	// directory-specific: the operator picks, so supporting a second server
	// type is config, not a code change.
	UserFilter string
	// EmailAttr and NameAttr name the entry attributes the flow reads. Also
	// configurable rather than hardcoded, for the same reason.
	EmailAttr   string
	NameAttr    string
	TLSInsecure bool
	Timeout     time.Duration
}

// LDAPUser is what a successful bind resolves to: only what
// findOrCreateUser needs, nothing else. Deliberately no group or role
// attributes, because mapping LDAP groups to Multica workspace roles is out of
// scope for this integration.
type LDAPUser struct {
	Email       string
	DisplayName string
}

// LDAPAuthenticator verifies a username/password pair against a directory.
// Implementations are stateless: every Authenticate call opens its own
// connection, so a request can never inherit an earlier request's bound
// identity.
type LDAPAuthenticator interface {
	Authenticate(ctx context.Context, username, password string) (LDAPUser, error)
}

// ldapConn is the slice of *ldap.Conn the flow uses. Naming it as an
// interface is what lets the tests drive every branch (success, wrong
// password, missing entry, service-account failure, injection, timeout)
// against a fake directory instead of a live one.
type ldapConn interface {
	Bind(username, password string) error
	Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error)
	SetTimeout(timeout time.Duration)
	Close() error
}

// ldapAuthenticator is the production LDAPAuthenticator. dial is a field
// rather than a direct ldap.DialURL call for the same reason ldapConn exists.
type ldapAuthenticator struct {
	cfg  LDAPConfig
	dial func(ctx context.Context, cfg LDAPConfig) (ldapConn, error)
}

// NewLDAPAuthenticator builds an authenticator from cfg and fills in the
// documented defaults. It performs no network I/O, so an unreachable directory
// fails the first Authenticate call rather than the boot that configured it.
func NewLDAPAuthenticator(cfg LDAPConfig) LDAPAuthenticator {
	return &ldapAuthenticator{cfg: normalizeLDAPConfig(cfg), dial: dialLDAPConn}
}

// normalizeLDAPConfig applies the defaults .env.example documents, so a caller
// that builds an LDAPConfig by hand gets the same semantics as the env path.
func normalizeLDAPConfig(cfg LDAPConfig) LDAPConfig {
	if strings.TrimSpace(cfg.UserFilter) == "" {
		cfg.UserFilter = "(uid=%s)"
	}
	if strings.TrimSpace(cfg.EmailAttr) == "" {
		cfg.EmailAttr = "mail"
	}
	if strings.TrimSpace(cfg.NameAttr) == "" {
		cfg.NameAttr = "displayName"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultLDAPTimeout
	}
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.BaseDN = strings.TrimSpace(cfg.BaseDN)
	cfg.BindDN = strings.TrimSpace(cfg.BindDN)
	return cfg
}

// errLDAPInsecurePlainURL rejects LDAP_TLS_INSECURE alongside a plain
// ldap:// URL. The flag means "accept this certificate without checking
// it"; on a clear-text connection there is no certificate to check, so the
// pair is always a misreading of the config rather than a working setup.
// Refusing it is also what .env.example promises.
var errLDAPInsecurePlainURL = errors.New(
	"ldap: LDAP_TLS_INSECURE requires an ldaps:// (or ldapi://) URL; plain ldap:// sends the password unencrypted",
)

// dialLDAPConn opens one LDAP connection. The dialer timeout bounds the TCP
// (or TLS) handshake; SetTimeout bounds the request round trips after it.
func dialLDAPConn(_ context.Context, cfg LDAPConfig) (ldapConn, error) {
	if cfg.URL == "" {
		return nil, errors.New("ldap: URL is not configured")
	}
	if err := validateLDAPTransport(cfg); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: cfg.Timeout}
	conn, err := ldap.DialURL(cfg.URL,
		ldap.DialWithDialer(dialer),
		ldap.DialWithTLSConfig(tlsConfigFor(cfg)),
	)
	if err != nil {
		return nil, err
	}
	conn.SetTimeout(cfg.Timeout)
	return conn, nil
}

// validateLDAPTransport checks the scheme against TLS settings before any
// bytes go on the wire. Anything it cannot parse is left to ldap.DialURL,
// which reports the same failure with its own context.
func validateLDAPTransport(cfg LDAPConfig) error {
	if !cfg.TLSInsecure {
		return nil
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil {
		return nil
	}
	if strings.EqualFold(parsed.Scheme, "ldap") {
		return errLDAPInsecurePlainURL
	}
	return nil
}

// tlsConfigFor returns nil for anything but an explicit insecure request: a
// plain ldap:// URL ignores tls.Config entirely, and ldaps:// with a nil
// config gets go-ldap's default, which verifies the chain and the hostname.
// InsecureSkipVerify stays opt-in (LDAP_TLS_INSECURE) for private-CA or
// self-signed directories during bring-up.
func tlsConfigFor(cfg LDAPConfig) *tls.Config {
	if !cfg.TLSInsecure {
		return nil
	}
	//nolint:gosec // operator opt-in for a directory whose CA we cannot verify
	return &tls.Config{InsecureSkipVerify: true}
}

// Authenticate runs Search + Bind: bind the service account, search for the
// username, then bind again as the found DN with the supplied password.
//
// The two-step form is deliberate. Composing a DN from the username and
// binding directly (the alternative) misses on real directories whose OU
// layout is irregular, and it lets request input decide the bound identity.
// Here the only DN ever bound is one the directory returned.
func (a *ldapAuthenticator) Authenticate(ctx context.Context, username, password string) (LDAPUser, error) {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	// go-ldap takes no context, so a wedged directory would otherwise hold the
	// request past its deadline. Run the exchange off the calling goroutine
	// and abandon it when the deadline wins; the buffered channel plus the
	// conn's own SetTimeout means the abandoned goroutine still exits.
	type attempt struct {
		user LDAPUser
		err  error
	}
	done := make(chan attempt, 1)
	go func() {
		user, err := a.attempt(ctx, username, password)
		select {
		case done <- attempt{user: user, err: err}:
		case <-ctx.Done():
		}
	}()

	select {
	case res := <-done:
		return res.user, res.err
	case <-ctx.Done():
		slog.Warn("ldap: authentication timed out", "url", a.cfg.URL)
		return LDAPUser{}, ErrLDAPUnavailable
	}
}

// attempt is the blocking directory exchange. It returns only sentinels, never
// an error carrying directory- or user-supplied text, so a handler can log it
// verbatim without leaking anything.
func (a *ldapAuthenticator) attempt(ctx context.Context, username, password string) (LDAPUser, error) {
	cfg := a.cfg

	// An empty password alongside a configured BindDN is an anonymous bind,
	// which succeeds on most directories and authenticates nobody. Reject it
	// before touching the network.
	if username == "" || password == "" {
		return LDAPUser{}, ErrLDAPInvalidCredentials
	}
	if cfg.URL == "" || cfg.BaseDN == "" || cfg.BindDN == "" {
		slog.Error("ldap: misconfigured; URL, BaseDN and BindDN are all required")
		return LDAPUser{}, ErrLDAPUnavailable
	}

	conn, err := a.dial(ctx, cfg)
	if err != nil {
		slog.Warn("ldap: connect to directory failed", "url", cfg.URL, "error", err)
		return LDAPUser{}, ErrLDAPUnavailable
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			slog.Debug("ldap: closing connection", "error", cerr)
		}
	}()

	// First bind: the service account, which exists only to look up the
	// requester. Its password never reaches the second bind.
	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		// Deployment-owned credentials, so a failure here is never something
		// the person logging in can fix. The directory's own message can echo
		// the BindDN, hence the summarised error.
		slog.Error("ldap: service account bind failed; check LDAP_BIND_DN and LDAP_BIND_PASSWORD",
			"bind_dn", cfg.BindDN, "error", ldapResultSummary(err))
		return LDAPUser{}, ErrLDAPUnavailable
	}

	request, err := searchRequestFor(cfg, username)
	if err != nil {
		slog.Error("ldap: invalid LDAP_USER_FILTER", "filter", cfg.UserFilter, "error", err)
		return LDAPUser{}, ErrLDAPUnavailable
	}

	result, err := conn.Search(request)
	if err != nil {
		slog.Warn("ldap: user search failed", "base_dn", cfg.BaseDN, "error", ldapResultSummary(err))
		return LDAPUser{}, ErrLDAPUnavailable
	}

	switch {
	case len(result.Entries) == 0:
		// Same sentinel as a wrong password; see ErrLDAPInvalidCredentials.
		return LDAPUser{}, ErrLDAPInvalidCredentials
	case len(result.Entries) > 1:
		// Multiple matches for one username is a directory the operator has to
		// resolve. Choosing one silently would decide, by server-side ordering,
		// which account a correct password unlocks.
		slog.Error("ldap: username matched multiple directory entries", "matches", len(result.Entries))
		return LDAPUser{}, ErrLDAPInvalidCredentials
	}

	entry := result.Entries[0]

	// Second bind: proves the password against the DN the directory itself
	// named.
	if err := conn.Bind(entry.DN, password); err != nil {
		if isLDAPInvalidCredentials(err) {
			slog.Info("ldap: directory rejected credentials", "username", username)
			return LDAPUser{}, ErrLDAPInvalidCredentials
		}
		slog.Warn("ldap: user bind failed", "username", username, "error", ldapResultSummary(err))
		return LDAPUser{}, ErrLDAPUnavailable
	}

	email := strings.ToLower(strings.TrimSpace(attributeValue(entry, cfg.EmailAttr)))
	if email == "" {
		// Multica accounts are matched on users.email, so an entry without one
		// cannot resolve to a user at all. A blank here is almost always a
		// wrong LDAP_EMAIL_ATTR, which is an operator fault rather than a
		// wrong password, so it must not read as a credential failure.
		slog.Error("ldap: directory entry has no email attribute; check LDAP_EMAIL_ATTR",
			"attribute", cfg.EmailAttr)
		return LDAPUser{}, ErrLDAPUnavailable
	}

	return LDAPUser{
		Email:       email,
		DisplayName: strings.TrimSpace(attributeValue(entry, cfg.NameAttr)),
	}, nil
}

// searchRequestFor builds the lookup for one username. The username goes
// through ldap.EscapeFilter before it reaches the template, so filter
// metacharacters (* ( ) \ NUL) are matched literally and cannot widen the
// query into something like (uid=*)(mail=@corp.com).
//
// SizeLimit is 2, not 1: reading a single row would hide a duplicate-uid
// directory, and the caller needs "more than one" to refuse the login.
func searchRequestFor(cfg LDAPConfig, username string) (*ldap.SearchRequest, error) {
	if strings.Count(cfg.UserFilter, "%s") != 1 {
		return nil, fmt.Errorf("ldap: user filter %q needs exactly one %%s placeholder", cfg.UserFilter)
	}

	return ldap.NewSearchRequest(
		cfg.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false,
		fmt.Sprintf(cfg.UserFilter, ldap.EscapeFilter(username)),
		[]string{cfg.EmailAttr, cfg.NameAttr},
		nil,
	), nil
}

// attributeValue reads an attribute case-insensitively. LDAP attribute names
// are case-insensitive per RFC 4511, and directories routinely echo back
// different casing than the search asked for (Mail for a requested mail).
func attributeValue(entry *ldap.Entry, name string) string {
	if entry == nil || name == "" {
		return ""
	}
	for _, attr := range entry.Attributes {
		if strings.EqualFold(attr.Name, name) && len(attr.Values) > 0 {
			return attr.Values[0]
		}
	}
	return ""
}

// isLDAPInvalidCredentials matches the one directory response that means
// "wrong password" as opposed to "the directory is broken".
func isLDAPInvalidCredentials(err error) bool {
	return ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials)
}

// ldapResultSummary reduces a go-ldap error to its result code. The wrapped
// Err can carry the server's diagnostic string, which on some directories
// repeats back the bound DN or the attempted password.
func ldapResultSummary(err error) string {
	var ldapErr *ldap.Error
	if errors.As(err, &ldapErr) {
		return fmt.Sprintf("ldap result code %d", ldapErr.ResultCode)
	}
	return err.Error()
}
