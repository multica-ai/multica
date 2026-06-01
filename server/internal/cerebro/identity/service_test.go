package identity

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeDomainsHappyPath(t *testing.T) {
	in := []string{"firtal.com", "Skjold.dk ", " bureau.io"}
	got, err := normalizeDomains(in)
	if err != nil {
		t.Fatalf("normalizeDomains returned error: %v", err)
	}
	want := []string{"firtal.com", "skjold.dk", "bureau.io"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeDomains = %v, want %v", got, want)
	}
}

func TestNormalizeDomainsDedupes(t *testing.T) {
	got, err := normalizeDomains([]string{"firtal.com", "FIRTAL.com", "firtal.com"})
	if err != nil {
		t.Fatalf("normalizeDomains returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"firtal.com"}) {
		t.Fatalf("normalizeDomains = %v, want [firtal.com]", got)
	}
}

func TestNormalizeDomainsRejectsAtSign(t *testing.T) {
	if _, err := normalizeDomains([]string{"jesper@firtal.com"}); !errors.Is(err, ErrInvalidDomain) {
		t.Fatalf("normalizeDomains accepted '@' in domain, err=%v", err)
	}
}

func TestNormalizeDomainsRejectsWhitespaceInside(t *testing.T) {
	if _, err := normalizeDomains([]string{"firtal com"}); !errors.Is(err, ErrInvalidDomain) {
		t.Fatalf("normalizeDomains accepted whitespace inside domain, err=%v", err)
	}
}

func TestNormalizeDomainsRejectsEmpty(t *testing.T) {
	if _, err := normalizeDomains([]string{""}); !errors.Is(err, ErrInvalidDomain) {
		t.Fatalf("normalizeDomains accepted empty domain, err=%v", err)
	}
	if _, err := normalizeDomains([]string{"  "}); !errors.Is(err, ErrInvalidDomain) {
		t.Fatalf("normalizeDomains accepted whitespace-only domain, err=%v", err)
	}
}

func TestEmailDomain(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"jesper@firtal.com", "firtal.com"},
		{"foo.bar+tag@subdom.example.co", "subdom.example.co"},
		{"no-at-sign", ""},
		{"trailing@", ""},
		{"@leading.com", "leading.com"},
		{"", ""},
	}
	for _, c := range cases {
		if got := emailDomain(c.in); got != c.want {
			t.Errorf("emailDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsUniqueViolationMatchesSQLState23505(t *testing.T) {
	if !isUniqueViolation(errors.New("ERROR: duplicate key (SQLSTATE 23505)")) {
		t.Fatal("isUniqueViolation should match SQLSTATE 23505")
	}
	if isUniqueViolation(errors.New("ERROR: not-null (SQLSTATE 23502)")) {
		t.Fatal("isUniqueViolation should not match SQLSTATE 23502")
	}
	if isUniqueViolation(nil) {
		t.Fatal("isUniqueViolation(nil) should be false")
	}
}
