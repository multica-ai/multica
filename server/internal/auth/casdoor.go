package auth

import (
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// CasdoorUserInfo holds identity claims extracted from a Casdoor-issued JWT.
type CasdoorUserInfo struct {
	SubjectID         string // from "sub" (required)
	Name              string // from "name"
	PreferredUsername string // from "preferred_username"
	Email             string // from "email"
	Phone             string // from "phone"
}

// ParseCasdoorJWT validates an RS256 JWT signed by Casdoor and extracts user
// claims. jwks provides the public keys used for signature verification.
//
// The parser is intentionally strict:
//   - Only RS256 is accepted (prevents algorithm-confusion attacks).
//   - The token must carry an "exp" claim.
//   - The header must include a "kid" that resolves via jwks.
//   - "sub" must be present and non-empty.
func ParseCasdoorJWT(tokenString string, jwks *JWKSProvider) (*CasdoorUserInfo, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("token header missing kid")
		}
		return jwks.GetKey(kid)
	},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("parse JWT: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	sub, _ := claims["sub"].(string)
	if strings.TrimSpace(sub) == "" {
		return nil, fmt.Errorf("missing or empty sub claim")
	}

	return &CasdoorUserInfo{
		SubjectID:         sub,
		Name:              stringClaim(claims, "name"),
		PreferredUsername: stringClaim(claims, "preferred_username"),
		Email:             stringClaim(claims, "email"),
		Phone:             stringClaim(claims, "phone"),
	}, nil
}

// stringClaim returns a claim as a string, or "" if absent / wrong type.
func stringClaim(claims jwt.MapClaims, key string) string {
	v, _ := claims[key].(string)
	return v
}
