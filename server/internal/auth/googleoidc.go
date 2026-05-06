package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// GoogleIssuer is the canonical issuer URL Google publishes in its OIDC
// discovery document. Pinned because mismatched issuers (e.g.
// "accounts.google.com" without https) are a known phishing vector.
const GoogleIssuer = "https://accounts.google.com"

// GoogleIdentity is the trusted view of a Google user, populated only from
// claims pulled out of a verified ID token.
type GoogleIdentity struct {
	Sub           string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
}

// GoogleVerifier verifies Google ID tokens. It owns the JWKs cache and the
// audience binding (aud == client_id). One instance per server process.
type GoogleVerifier struct {
	v *oidc.IDTokenVerifier
}

// NewGoogleVerifier connects to the real Google issuer.
func NewGoogleVerifier(ctx context.Context, clientID string) (*GoogleVerifier, error) {
	return NewGoogleVerifierForIssuer(ctx, GoogleIssuer, clientID)
}

// NewGoogleVerifierForIssuer is exposed for tests with a fake issuer.
func NewGoogleVerifierForIssuer(ctx context.Context, issuer, clientID string) (*GoogleVerifier, error) {
	if clientID == "" {
		return nil, errors.New("googleoidc: clientID required")
	}
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("googleoidc: discovery: %w", err)
	}
	return &GoogleVerifier{v: p.Verifier(&oidc.Config{ClientID: clientID})}, nil
}

// Verify validates signature, iss, aud, exp and returns the trusted identity.
// Callers MUST check EmailVerified before linking accounts by email.
func (g *GoogleVerifier) Verify(ctx context.Context, rawIDToken string) (GoogleIdentity, error) {
	tok, err := g.v.Verify(ctx, rawIDToken)
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("googleoidc: verify: %w", err)
	}
	var c struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := tok.Claims(&c); err != nil {
		return GoogleIdentity{}, fmt.Errorf("googleoidc: claims: %w", err)
	}
	return GoogleIdentity{
		Sub:           tok.Subject,
		Email:         c.Email,
		EmailVerified: c.EmailVerified,
		Name:          c.Name,
		Picture:       c.Picture,
	}, nil
}
