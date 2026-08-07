package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	OIDCFlowCookieName = "multica_oidc_flow"
	oidcFlowAudience   = "multica-oidc-flow"
	oidcFlowTTL        = 10 * time.Minute
)

var ErrInvalidOIDCFlow = errors.New("invalid or expired OIDC flow")

type OIDCFlow struct {
	State        string `json:"state"`
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"code_verifier"`
	AppState     string `json:"app_state,omitempty"`
	jwt.RegisteredClaims
}

func NewOIDCFlow(appState, codeVerifier string) (OIDCFlow, error) {
	state, err := randomURLToken(32)
	if err != nil {
		return OIDCFlow{}, err
	}
	nonce, err := randomURLToken(32)
	if err != nil {
		return OIDCFlow{}, err
	}
	now := time.Now()
	return OIDCFlow{
		State:        "oidc." + state,
		Nonce:        nonce,
		CodeVerifier: codeVerifier,
		AppState:     appState,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{oidcFlowAudience},
			Subject:   "oidc-flow",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(oidcFlowTTL)),
		},
	}, nil
}

func SetOIDCFlowCookie(w http.ResponseWriter, flow OIDCFlow) error {
	value, err := jwt.NewWithClaims(jwt.SigningMethodHS256, flow).SignedString(JWTSecret())
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     OIDCFlowCookieName,
		Value:    value,
		Path:     "/auth/oidc",
		Domain:   cookieDomain(),
		MaxAge:   int(oidcFlowTTL.Seconds()),
		Expires:  time.Now().Add(oidcFlowTTL),
		HttpOnly: true,
		Secure:   isSecureCookie(),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func ReadOIDCFlowCookie(r *http.Request) (OIDCFlow, error) {
	cookie, err := r.Cookie(OIDCFlowCookieName)
	if err != nil {
		return OIDCFlow{}, ErrInvalidOIDCFlow
	}
	claims := OIDCFlow{}
	token, err := jwt.ParseWithClaims(cookie.Value, &claims, func(token *jwt.Token) (any, error) {
		return JWTSecret(), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithAudience(oidcFlowAudience), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || claims.State == "" || claims.Nonce == "" || claims.CodeVerifier == "" {
		return OIDCFlow{}, ErrInvalidOIDCFlow
	}
	return claims, nil
}

func ClearOIDCFlowCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     OIDCFlowCookieName,
		Value:    "",
		Path:     "/auth/oidc",
		Domain:   cookieDomain(),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   isSecureCookie(),
		SameSite: http.SameSiteLaxMode,
	})
}

func randomURLToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
