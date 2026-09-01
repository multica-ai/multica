package main

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

// clearLdapEnv neutralises the whole LDAP_* block so a developer's own .env, or
// a CI deployment matrix, cannot leak into a test that asserts defaults.
func clearLdapEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"LDAP_ENABLED",
		"LDAP_URL",
		"LDAP_BASE_DN",
		"LDAP_BIND_DN",
		"LDAP_BIND_PASSWORD",
		"LDAP_USER_FILTER",
		"LDAP_EMAIL_ATTR",
		"LDAP_NAME_ATTR",
		"LDAP_TLS_INSECURE",
		"LDAP_TIMEOUT",
	} {
		t.Setenv(name, "")
	}
}

// TestLDAPConfigFromEnvDefaults pins what a deployment gets when it sets
// nothing. The managed cloud is exactly this case, so "disabled" has to be the
// outcome rather than a half-configured client that fails at login time.
//
// UserFilter/EmailAttr/NameAttr stay empty here on purpose: the OpenLDAP
// defaults live in auth.normalizeLDAPConfig so that a hand-built LDAPConfig and
// the env path agree. TestNewLDAPAuthenticatorAppliesDefaults in internal/auth
// owns that contract; repeating the literals here would let the two drift.
func TestLDAPConfigFromEnvDefaults(t *testing.T) {
	clearLdapEnv(t)

	cfg := ldapConfigFromEnv()
	if cfg.Enabled {
		t.Fatal("unset LDAP_ENABLED must leave directory login disabled")
	}
	if cfg.TLSInsecure {
		t.Error("LDAP_TLS_INSECURE must default to false; skipping certificate checks is never a default")
	}
	if cfg.Timeout != auth.DefaultLDAPTimeout {
		t.Errorf("LDAP_TIMEOUT: got %s, want the documented default %s", cfg.Timeout, auth.DefaultLDAPTimeout)
	}
	if cfg.BindPassword != "" {
		t.Errorf("LDAP_BIND_PASSWORD: got %q, want empty", cfg.BindPassword)
	}
}

// TestLDAPConfigFromEnvReadsEveryVariable is the ten-variable contract with
// .env.example: each variable has to actually reach the struct, with surrounding
// whitespace removed so a stray space in a DN does not silently become a failed
// bind the operator cannot see in their own file.
func TestLDAPConfigFromEnvReadsEveryVariable(t *testing.T) {
	clearLdapEnv(t)
	t.Setenv("LDAP_ENABLED", "true")
	t.Setenv("LDAP_URL", " ldaps://openldap.corp.example.com:636 ")
	t.Setenv("LDAP_BASE_DN", " ou=people,dc=corp,dc=example,dc=com ")
	t.Setenv("LDAP_BIND_DN", " cn=lookup,dc=corp,dc=example,dc=com ")
	t.Setenv("LDAP_BIND_PASSWORD", " s3cr3t with spaces ")
	t.Setenv("LDAP_USER_FILTER", " (sAMAccountName=%s) ")
	t.Setenv("LDAP_EMAIL_ATTR", " userPrincipalName ")
	t.Setenv("LDAP_NAME_ATTR", " cn ")
	t.Setenv("LDAP_TLS_INSECURE", "true")
	t.Setenv("LDAP_TIMEOUT", "90s")

	cfg := ldapConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("LDAP_ENABLED=true must enable directory login")
	}
	// The password is the one field taken verbatim. A directory password may
	// legitimately carry leading or trailing spaces, and trimming it would
	// produce a bind failure the operator cannot read out of the config file.
	if cfg.BindPassword != " s3cr3t with spaces " {
		t.Errorf("LDAP_BIND_PASSWORD: got %q, want it passed through untrimmed", cfg.BindPassword)
	}
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"LDAP_URL", cfg.URL, "ldaps://openldap.corp.example.com:636"},
		{"LDAP_BASE_DN", cfg.BaseDN, "ou=people,dc=corp,dc=example,dc=com"},
		{"LDAP_BIND_DN", cfg.BindDN, "cn=lookup,dc=corp,dc=example,dc=com"},
		{"LDAP_USER_FILTER", cfg.UserFilter, "(sAMAccountName=%s)"},
		{"LDAP_EMAIL_ATTR", cfg.EmailAttr, "userPrincipalName"},
		{"LDAP_NAME_ATTR", cfg.NameAttr, "cn"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if !cfg.TLSInsecure {
		t.Error("LDAP_TLS_INSECURE=true must be read")
	}
	if cfg.Timeout != 90*time.Second {
		t.Errorf("LDAP_TIMEOUT: got %s, want 90s", cfg.Timeout)
	}
}

// TestLDAPConfigEnabledIsExactOnTheDocumentedValue pins the accepted spellings
// of the switch. .env.example says `true`; "1"/"yes"/"on" are what an operator
// reaches for next. Accepting them silently would make the documented contract
// a lie, so they stay false - which is exactly why this is asserted rather than
// left to whatever strings.EqualFold happens to match.
func TestLDAPConfigEnabledIsExactOnTheDocumentedValue(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"true", true},
		{"TRUE", true},
		{" true ", true},
		{"false", false},
		{"", false},
		{"1", false},
		{"yes", false},
		{"on", false},
	} {
		t.Run("LDAP_ENABLED="+tc.raw, func(t *testing.T) {
			clearLdapEnv(t)
			t.Setenv("LDAP_ENABLED", tc.raw)
			if got := ldapConfigFromEnv().Enabled; got != tc.want {
				t.Errorf("LDAP_ENABLED=%q: got Enabled=%v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestLDAPConfigTimeoutFallsBackToDefault: an unparsable or non-positive
// timeout must not become a zero-duration client that fails every login
// instantly. LDAP_TIMEOUT is documented as a Go duration, so anything else is
// operator error and gets the default with a warning, not a broken auth path.
func TestLDAPConfigTimeoutFallsBackToDefault(t *testing.T) {
	for _, raw := range []string{"nonsense", "5", "-3s", "0s"} {
		t.Run(raw, func(t *testing.T) {
			clearLdapEnv(t)
			t.Setenv("LDAP_TIMEOUT", raw)
			if got := ldapConfigFromEnv().Timeout; got != auth.DefaultLDAPTimeout {
				t.Errorf("LDAP_TIMEOUT=%q: got %s, want the default %s", raw, got, auth.DefaultLDAPTimeout)
			}
		})
	}
}

// TestLDAPAuthenticatorForGate is decision D4 in one place: LDAP_ENABLED is the
// whole switch, and everything downstream (the handler's 503, the omitted
// ldap_enabled on /api/config) depends on a disabled deployment holding a nil
// authenticator even when every other variable was filled in.
func TestLDAPAuthenticatorForGate(t *testing.T) {
	enabled := auth.LDAPConfig{
		Enabled: true,
		URL:     "ldaps://openldap.corp.example.com:636",
		BaseDN:  "ou=people,dc=corp,dc=example,dc=com",
		BindDN:  "cn=lookup,dc=corp,dc=example,dc=com",
	}

	disabled := enabled
	disabled.Enabled = false
	if a := ldapAuthenticatorFor(disabled); a != nil {
		t.Fatal("a config without Enabled must not construct an authenticator, however complete its other fields are")
	}
	if ldapAuthenticatorFor(enabled) == nil {
		t.Fatal("Enabled=true must construct an authenticator")
	}
}

// TestLDAPAuthenticatorForConstructsWithoutDialing keeps boot safe: an enabled
// config pointing at a dead directory, or at a URL that cannot even be parsed,
// must still produce a client. Building one performs no I/O, so an unreachable
// directory fails the first login instead of taking the whole API down with it.
func TestLDAPAuthenticatorForConstructsWithoutDialing(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
	}{
		{"unreachable host", "ldaps://no-such-directory.invalid:636"},
		{"unparsable scheme", "not-a-url"},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ldapAuthenticatorFor(auth.LDAPConfig{Enabled: true, URL: tc.url}) == nil {
				t.Fatalf("URL=%q: got a nil authenticator, want a client that fails at first use", tc.url)
			}
		})
	}
}
