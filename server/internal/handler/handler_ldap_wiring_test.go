package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

// TestNewWiresLdapAuthenticator is the Task 2 contract: New is a pass-through
// for the directory client. Whether a deployment has one is decided by the
// caller (cmd/server), so New must not invent a client when it is handed nil,
// and must not drop one when it is handed a real authenticator.
//
// h.LDAPAuth == nil is load-bearing, not cosmetic: LDAPLogin answers 503 and
// GetConfig omits ldap_enabled on that check alone. A typed-nil
// (*fakeLDAPAuthenticator)(nil) stored into the interface would satisfy neither,
// which is why the disabled case asserts against the untyped nil literal.
func TestNewWiresLdapAuthenticator(t *testing.T) {
	build := func(ldapAuth auth.LDAPAuthenticator) *Handler {
		t.Helper()
		return New(nil, nil, nil, nil, nil, nil, nil, ldapAuth, nil, Config{
			LDAPConfig: auth.LDAPConfig{Enabled: false},
		})
	}

	if h := build(nil); h.LDAPAuth != nil {
		t.Errorf("LDAPAuth: got %v for a disabled deployment, want nil so the handler reports the feature as unavailable", h.LDAPAuth)
	}

	dir := &fakeLDAPAuthenticator{}
	h := build(dir)
	if h.LDAPAuth == nil {
		t.Fatal("LDAPAuth: got nil, want the authenticator the caller supplied")
	}
	if _, ok := h.LDAPAuth.(*fakeLDAPAuthenticator); !ok {
		t.Errorf("LDAPAuth: got %T, want the caller-supplied authenticator passed through unchanged", h.LDAPAuth)
	}
}
