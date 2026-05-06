package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"google.golang.org/api/idtoken"
)

type googleIDTokenPayload struct {
	Subject string
	Email   string
	Name    string
	Picture string
}

var validateGoogleIDToken = func(ctx context.Context, idToken, audience string) (googleIDTokenPayload, error) {
	payload, err := idtoken.Validate(ctx, idToken, audience)
	if err != nil {
		return googleIDTokenPayload{}, err
	}

	claims := payload.Claims
	email, _ := claims["email"].(string)
	name, _ := claims["name"].(string)
	picture, _ := claims["picture"].(string)

	return googleIDTokenPayload{
		Subject: payload.Subject,
		Email:   email,
		Name:    name,
		Picture: picture,
	}, nil
}

func googleMobileAudience() string {
	return strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
}

func (h *Handler) GoogleMobileLogin(w http.ResponseWriter, r *http.Request) {
	var req GoogleMobileLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.IDToken = strings.TrimSpace(req.IDToken)
	if req.IDToken == "" {
		writeError(w, http.StatusBadRequest, "id_token is required")
		return
	}

	audience := googleMobileAudience()
	if audience == "" {
		writeError(w, http.StatusServiceUnavailable, "Google mobile login is not configured")
		return
	}

	payload, err := validateGoogleIDToken(r.Context(), req.IDToken, audience)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid Google token")
		return
	}
	if payload.Subject == "" {
		writeError(w, http.StatusUnauthorized, "invalid Google token")
		return
	}
	if strings.TrimSpace(payload.Email) == "" {
		writeError(w, http.StatusBadRequest, "Google account has no email")
		return
	}

	h.loginWithGoogleUser(w, r, googleUserInfo{
		ID:      payload.Subject,
		Email:   payload.Email,
		Name:    payload.Name,
		Picture: payload.Picture,
	})
}

func withGoogleIDTokenValidator(
	validator func(context.Context, string, string) (googleIDTokenPayload, error),
) func() {
	previous := validateGoogleIDToken
	validateGoogleIDToken = validator
	return func() {
		validateGoogleIDToken = previous
	}
}

var errInvalidGoogleIDToken = errors.New("invalid google id token")
