package webhookgateway

import (
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestParsePrincipal(t *testing.T) {
	uuid := "11111111-1111-1111-1111-111111111111"
	cases := []struct {
		name    string
		raw     string
		wantOK  bool
		wantTyp string
	}{
		{"agent", "agent:" + uuid, true, "agent"},
		{"member", "member:" + uuid, true, "member"},
		{"trimmed", "  agent:" + uuid + "  ", true, "agent"},
		{"unknown type", "service:" + uuid, false, ""},
		{"missing colon", "agent" + uuid, false, ""},
		{"empty type", ":" + uuid, false, ""},
		{"bad uuid", "agent:not-a-uuid", false, ""},
		{"empty", "", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, ok := parsePrincipal(c.raw)
			if ok != c.wantOK {
				t.Fatalf("parsePrincipal(%q) ok = %v, want %v", c.raw, ok, c.wantOK)
			}
			if ok && p.Type != c.wantTyp {
				t.Fatalf("parsePrincipal(%q) type = %q, want %q", c.raw, p.Type, c.wantTyp)
			}
		})
	}
}

func TestAuthorized(t *testing.T) {
	d := &Deliverer{Token: "s3cr3t"}
	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"correct", "Bearer s3cr3t", true},
		{"wrong", "Bearer nope", false},
		{"no prefix", "s3cr3t", false},
		{"empty", "", false},
		{"prefix only", "Bearer ", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/webhooks/gateway/channel-message", nil)
			if c.header != "" {
				r.Header.Set("Authorization", c.header)
			}
			if got := d.authorized(r); got != c.want {
				t.Fatalf("authorized(%q) = %v, want %v", c.header, got, c.want)
			}
		})
	}
}

func TestEnabled(t *testing.T) {
	if (&Deliverer{}).Enabled() {
		t.Fatal("zero Deliverer should be disabled")
	}
	if (&Deliverer{Queries: &db.Queries{}}).Enabled() {
		t.Fatal("no token should be disabled")
	}
	if !(&Deliverer{Queries: &db.Queries{}, Token: "t"}).Enabled() {
		t.Fatal("queries + token should be enabled")
	}
}

func TestDisabledReturns404(t *testing.T) {
	d := &Deliverer{} // no queries, no token
	r := httptest.NewRequest("POST", "/api/webhooks/gateway/channel-message", nil)
	r.Header.Set("Authorization", "Bearer anything")
	w := httptest.NewRecorder()
	d.Handle(w, r)
	if w.Code != 404 {
		t.Fatalf("disabled deliverer should 404, got %d", w.Code)
	}
}
