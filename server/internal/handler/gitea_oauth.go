package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

const giteaOAuthStateTTL = 10 * time.Minute

type giteaOAuthStatePayload struct {
	Nonce       string `json:"nonce"`
	ClientState string `json:"client_state,omitempty"`
}

type giteaOAuthStateCookie struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
}

func giteaIssuerURL() (*url.URL, error) {
	raw := strings.TrimRight(strings.TrimSpace(os.Getenv("GITEA_ISSUER_URL")), "/")
	if raw == "" {
		return nil, errors.New("Gitea issuer URL is not configured")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("Gitea issuer URL must be an absolute HTTP(S) URL without query or fragment")
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return nil, errors.New("Gitea issuer URL must use HTTP or HTTPS")
	}
	return u, nil
}

func giteaRedirectURI() (string, error) {
	redirectURI := strings.TrimSpace(os.Getenv("GITEA_REDIRECT_URI"))
	if redirectURI == "" {
		return "", errors.New("Gitea redirect URI is not configured")
	}
	u, err := url.Parse(redirectURI)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", errors.New("Gitea redirect URI must be an absolute HTTP(S) URL")
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", errors.New("Gitea redirect URI must use HTTP or HTTPS")
	}
	return redirectURI, nil
}

func giteaBackendURL(path string) string {
	issuer, err := giteaIssuerURL()
	if err != nil {
		return ""
	}
	return strings.TrimRight(issuer.String(), "/") + path
}

func giteaHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

func randomURLToken(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func giteaPKCEChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func signGiteaState(encodedPayload string) string {
	mac := hmac.New(sha256.New, auth.JWTSecret())
	mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func newGiteaOAuthState(clientState string) (state, cookieValue, verifier string, err error) {
	if len(clientState) > 2048 {
		return "", "", "", errors.New("OAuth state is too large")
	}
	nonce, err := randomURLToken(32)
	if err != nil {
		return "", "", "", err
	}
	verifier, err = randomURLToken(32)
	if err != nil {
		return "", "", "", err
	}
	payload, err := json.Marshal(giteaOAuthStatePayload{Nonce: nonce, ClientState: clientState})
	if err != nil {
		return "", "", "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	state = encodedPayload + "." + signGiteaState(encodedPayload)
	cookie, err := json.Marshal(giteaOAuthStateCookie{State: state, Verifier: verifier})
	if err != nil {
		return "", "", "", err
	}
	return state, base64.RawURLEncoding.EncodeToString(cookie), verifier, nil
}

func validateGiteaOAuthState(r *http.Request, state string) (string, error) {
	parts := strings.Split(state, ".")
	if len(parts) != 2 || parts[0] == "" || !hmac.Equal([]byte(parts[1]), []byte(signGiteaState(parts[0]))) {
		return "", errors.New("invalid Gitea OAuth state")
	}
	encodedPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("invalid Gitea OAuth state")
	}
	var payload giteaOAuthStatePayload
	if json.Unmarshal(encodedPayload, &payload) != nil || payload.Nonce == "" {
		return "", errors.New("invalid Gitea OAuth state")
	}
	cookie, err := r.Cookie(auth.OAuthStateCookieName)
	if err != nil {
		return "", errors.New("missing Gitea OAuth state")
	}
	var stored giteaOAuthStateCookie
	encodedCookie, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || json.Unmarshal(encodedCookie, &stored) != nil || stored.State == "" || stored.Verifier == "" {
		return "", errors.New("invalid Gitea OAuth state")
	}
	if !hmac.Equal([]byte(stored.State), []byte(state)) {
		return "", errors.New("invalid Gitea OAuth state")
	}
	return stored.Verifier, nil
}

func decodeGiteaOAuthState(state string) (giteaOAuthStatePayload, bool) {
	parts := strings.Split(state, ".")
	if len(parts) != 2 {
		return giteaOAuthStatePayload{}, false
	}
	encoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return giteaOAuthStatePayload{}, false
	}
	var payload giteaOAuthStatePayload
	if json.Unmarshal(encoded, &payload) != nil || payload.Nonce == "" {
		return giteaOAuthStatePayload{}, false
	}
	return payload, true
}

func giteaAuthorizeURL(issuer *url.URL, clientID, redirectURI, state, challenge string) string {
	values := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {"read:user"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return strings.TrimRight(issuer.String(), "/") + "/login/oauth/authorize?" + values.Encode()
}
