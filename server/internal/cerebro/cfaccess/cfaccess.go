// Package cfaccess verifies Cloudflare Access JWTs and injects Cloudflare
// Access service-token headers. It is a cerebro-only package (L4): it is
// imported by upstream-zone files (the auth middleware, the CLI/daemon HTTP
// clients) via small marked hooks, but never imports upstream auth logic.
//
// Two halves, used at opposite ends of the wall:
//
//   - Server side (Verifier): when a request arrives through Cloudflare Access,
//     Cloudflare injects a short-lived RS256 JWT in the Cf-Access-Jwt-Assertion
//     header. The Verifier validates that JWT against the team's published
//     signing keys + accepted AUD tags and maps the identity email to a Multica
//     user. Trusting the Cloudflare stamp is what lets Multica accept a
//     human who logged in at the edge without a separate Multica login.
//
//   - Client side (ServiceToken): a machine (laptop daemon, cloud runtime)
//     attaches its per-machine CF-Access-Client-Id / CF-Access-Client-Secret
//     headers so Cloudflare Access lets the request through the wall. Cloudflare
//     consumes those headers at the edge; they never reach Multica.
//
// Everything here is inert until configured. A nil Verifier (team domain / AUD
// unset) and an unconfigured ServiceToken (client id / secret unset) both
// behave as "feature off", so the code ships safely before the Cloudflare wall
// is raised and never locks anyone out of an environment that is not behind it.
package cfaccess

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Env vars consumed by NewVerifierFromEnv (server) and ServiceTokenFromEnv
// (client). The CEREBRO_ prefix marks them as cerebro-fork configuration.
const (
	// EnvTeamDomain is the Cloudflare Access team domain — either the bare
	// team name ("firtal") or the full host ("firtal.cloudflareaccess.com").
	EnvTeamDomain = "CEREBRO_CF_ACCESS_TEAM_DOMAIN"
	// EnvAUD is one or more Access application AUD tags, comma-separated. A
	// stamp is accepted only if its aud claim matches one of these.
	EnvAUD = "CEREBRO_CF_ACCESS_AUD"
	// EnvClientID / EnvClientSecret are the per-machine Cloudflare Access
	// service-token credentials a client presents to pass the wall.
	EnvClientID     = "CEREBRO_CF_ACCESS_CLIENT_ID"
	EnvClientSecret = "CEREBRO_CF_ACCESS_CLIENT_SECRET"
)

// Header names. The assertion header is injected by Cloudflare on the way in;
// the client-id/secret headers are sent by a machine on the way out.
const (
	HeaderJWTAssertion = "Cf-Access-Jwt-Assertion"
	HeaderClientID     = "CF-Access-Client-Id"
	HeaderClientSecret = "CF-Access-Client-Secret"
)

// jwksRefreshInterval bounds how often the signing keys are re-fetched on the
// happy path. An unknown kid forces an immediate out-of-band refresh anyway, so
// a key rotation is picked up within one request rather than one interval.
const jwksRefreshInterval = 15 * time.Minute

// Errors returned by Authenticate. Callers distinguish "no stamp, fall through
// to other auth" (ErrNoAssertion) from "stamp present but bad, reject"
// (ErrInvalid) from "stamp valid but unknown user" (ErrNoUser).
var (
	ErrNoAssertion = errors.New("cfaccess: no Cf-Access-Jwt-Assertion header")
	ErrInvalid     = errors.New("cfaccess: invalid Cloudflare Access token")
	ErrNoUser      = errors.New("cfaccess: no Multica user for verified email")
)

// EmailLookup maps a verified, lowercased email to a Multica user id. It
// returns an error (typically sql.ErrNoRows) when no user owns the email.
type EmailLookup func(ctx context.Context, email string) (userID string, err error)

// Identity is the result of a successful Cloudflare Access verification.
type Identity struct {
	// UserID is the Multica user resolved from Email, or empty when the stamp
	// belongs to a machine (service token, no email) or the email maps to no
	// user. A non-empty UserID is always a fully verified human identity.
	UserID string
	// Email is the verified, lowercased email claim — empty for service tokens.
	Email string
	// CommonName is the service-token client-id for a machine stamp — empty for
	// a human stamp.
	CommonName string
}

// Verifier validates Cloudflare Access JWTs against a team's published signing
// keys and accepted AUD tags, then maps the identity to a Multica user. The
// zero value is not usable; build one with NewVerifier / NewVerifierFromEnv. A
// nil *Verifier is a valid "feature off" value accepted by AuthenticatedUser.
// Safe for concurrent use.
type Verifier struct {
	issuer  string
	certURL string
	auds    map[string]struct{}
	lookup  EmailLookup
	http    *http.Client

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

// NewVerifierFromEnv builds a Verifier from the CEREBRO_CF_ACCESS_* env vars.
// It returns nil when the team domain or AUD is unset, so an unconfigured
// environment (local dev, staging before the wall) behaves exactly as before.
func NewVerifierFromEnv(lookup EmailLookup) *Verifier {
	team := strings.TrimSpace(os.Getenv(EnvTeamDomain))
	auds := splitAUDs(os.Getenv(EnvAUD))
	if team == "" || len(auds) == 0 {
		return nil
	}
	return NewVerifier(team, auds, lookup)
}

// NewVerifier builds a verifier for an explicit team domain and AUD set.
// teamDomain accepts either the bare team name ("firtal") or the full host
// ("firtal.cloudflareaccess.com").
func NewVerifier(teamDomain string, auds []string, lookup EmailLookup) *Verifier {
	host := normalizeTeamHost(teamDomain)
	audSet := make(map[string]struct{}, len(auds))
	for _, a := range auds {
		if a = strings.TrimSpace(a); a != "" {
			audSet[a] = struct{}{}
		}
	}
	return &Verifier{
		issuer:  "https://" + host,
		certURL: "https://" + host + "/cdn-cgi/access/certs",
		auds:    audSet,
		lookup:  lookup,
		http:    &http.Client{Timeout: 10 * time.Second},
		keys:    map[string]*rsa.PublicKey{},
	}
}

// AuthenticateRequest verifies the Cf-Access-Jwt-Assertion header on r. It
// returns ErrNoAssertion when the header is absent (the caller should fall
// through to other auth), ErrInvalid when the stamp fails verification (reject),
// and ErrNoUser when the stamp is valid but its email maps to no user.
func (v *Verifier) AuthenticateRequest(ctx context.Context, r *http.Request) (Identity, error) {
	assertion := strings.TrimSpace(r.Header.Get(HeaderJWTAssertion))
	if assertion == "" {
		return Identity{}, ErrNoAssertion
	}
	return v.Authenticate(ctx, assertion)
}

// Authenticate verifies a raw Cloudflare Access JWT string.
func (v *Verifier) Authenticate(ctx context.Context, assertion string) (Identity, error) {
	token, err := jwt.Parse(
		assertion,
		v.keyfunc(ctx),
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return Identity{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return Identity{}, fmt.Errorf("%w: unexpected claims type", ErrInvalid)
	}
	if !v.audMatches(claims["aud"]) {
		return Identity{}, fmt.Errorf("%w: aud mismatch", ErrInvalid)
	}

	id := Identity{}
	if cn, _ := claims["common_name"].(string); cn != "" {
		id.CommonName = cn
	}
	email, _ := claims["email"].(string)
	id.Email = strings.ToLower(strings.TrimSpace(email))
	if id.Email == "" {
		// Service-token (machine) stamp: valid at the edge but there is no
		// email to map. The caller decides whether a machine identity alone is
		// enough; for the auth-middleware fallback it is not — a machine's
		// Multica identity comes from its inner Bearer token instead.
		return id, nil
	}
	if v.lookup != nil {
		userID, err := v.lookup(ctx, id.Email)
		if err != nil {
			return id, fmt.Errorf("%w: %s", ErrNoUser, id.Email)
		}
		id.UserID = userID
	}
	return id, nil
}

// AuthenticatedUser verifies a Cloudflare Access stamp on r and returns the
// mapped Multica user id. ok is false (caller falls through to other auth or
// 401) whenever v is nil, no stamp is present, the stamp is invalid, or it maps
// to no user / a machine with no user. It never partially authenticates: a
// non-empty userID is always a fully verified human identity.
func AuthenticatedUser(ctx context.Context, v *Verifier, r *http.Request) (userID string, ok bool) {
	if v == nil {
		return "", false
	}
	id, err := v.AuthenticateRequest(ctx, r)
	if err != nil || id.UserID == "" {
		return "", false
	}
	return id.UserID, true
}

func (v *Verifier) keyfunc(ctx context.Context) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("missing kid")
		}
		if key := v.cachedKey(kid); key != nil {
			return key, nil
		}
		if err := v.refresh(ctx); err != nil {
			return nil, err
		}
		if key := v.cachedKey(kid); key != nil {
			return key, nil
		}
		return nil, fmt.Errorf("unknown kid %q", kid)
	}
}

// cachedKey returns the cached key for kid, or nil when the cache is stale
// (older than jwksRefreshInterval) so the caller refreshes.
func (v *Verifier) cachedKey(kid string) *rsa.PublicKey {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if time.Since(v.fetched) > jwksRefreshInterval {
		return nil
	}
	return v.keys[kid]
}

func (v *Verifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cfaccess: certs endpoint returned %d", resp.StatusCode)
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("cfaccess: decode JWKS: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if !strings.EqualFold(k.Kty, "RSA") || k.Kid == "" {
			continue
		}
		pub, err := k.rsaPublicKey()
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return errors.New("cfaccess: no usable RSA keys in JWKS")
	}
	v.mu.Lock()
	v.keys = keys
	v.fetched = time.Now()
	v.mu.Unlock()
	return nil
}

func (v *Verifier) audMatches(raw any) bool {
	switch a := raw.(type) {
	case string:
		_, ok := v.auds[a]
		return ok
	case []any:
		for _, item := range a {
			if s, ok := item.(string); ok {
				if _, ok := v.auds[s]; ok {
					return true
				}
			}
		}
	}
	return false
}

// jwk is one entry of a Cloudflare Access JWKS document.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (k jwk) rsaPublicKey() (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.Int64() < 2 {
		return nil, errors.New("invalid exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(e.Int64())}, nil
}

// ServiceToken is a per-machine Cloudflare Access service-token credential. A
// machine presents it so Cloudflare lets the request through the wall.
type ServiceToken struct {
	ClientID     string
	ClientSecret string
}

// ServiceTokenFromEnv reads the service-token credential from the
// CEREBRO_CF_ACCESS_CLIENT_ID / _SECRET env vars. An unset pair yields an
// unconfigured (no-op) token.
func ServiceTokenFromEnv() ServiceToken {
	return ServiceToken{
		ClientID:     strings.TrimSpace(os.Getenv(EnvClientID)),
		ClientSecret: strings.TrimSpace(os.Getenv(EnvClientSecret)),
	}
}

// Configured reports whether both halves of the credential are present.
func (t ServiceToken) Configured() bool {
	return t.ClientID != "" && t.ClientSecret != ""
}

// Apply sets the CF-Access-Client-Id / CF-Access-Client-Secret headers on h
// when the token is configured. It is a no-op otherwise, so calling it
// unconditionally on every outbound request is safe.
func (t ServiceToken) Apply(h http.Header) {
	if !t.Configured() {
		return
	}
	h.Set(HeaderClientID, t.ClientID)
	h.Set(HeaderClientSecret, t.ClientSecret)
}

// normalizeTeamHost turns a bare team name into a full cloudflareaccess.com
// host and strips any scheme / trailing slash from a host that was passed whole.
func normalizeTeamHost(teamDomain string) string {
	host := strings.TrimSpace(teamDomain)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")
	if !strings.Contains(host, ".") {
		host += ".cloudflareaccess.com"
	}
	return host
}

func splitAUDs(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
