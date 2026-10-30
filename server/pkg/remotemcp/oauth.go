package remotemcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxOAuthResponseBytes = 1 << 20

var resourceMetadataParameter = regexp.MustCompile(`(?i)(?:^|[, ]+)resource_metadata="([^"]+)"`)

// OAuthMetadata is the safe, public part of an MCP OAuth discovery result.
// Tokens and client secrets never enter this value.
type OAuthMetadata struct {
	ResourceEndpoint      string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	Scopes                []string
	TokenAuthMethods      []string
}

type OAuthClientRegistration struct {
	ClientID                string
	ClientSecret            string
	TokenEndpointAuthMethod string
}

type OAuthTokenResponse struct {
	AccessToken  string
	TokenType    string
	ExpiresIn    int64
	RefreshToken string
	Scope        string
}

// ValidateOAuthMetadata applies the same SSRF boundary to operator-supplied
// advanced overrides as to discovered metadata.
func ValidateOAuthMetadata(ctx context.Context, metadata OAuthMetadata) error {
	if metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" {
		return errors.New("OAuth metadata is missing required endpoints")
	}
	for _, raw := range []string{metadata.AuthorizationEndpoint, metadata.TokenEndpoint, metadata.RegistrationEndpoint} {
		if raw == "" {
			continue
		}
		if _, err := validateOAuthURL(ctx, raw); err != nil {
			return err
		}
	}
	return nil
}

type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type authorizationServerMetadata struct {
	AuthorizationEndpoint           string   `json:"authorization_endpoint"`
	TokenEndpoint                   string   `json:"token_endpoint"`
	RegistrationEndpoint            string   `json:"registration_endpoint"`
	CodeChallengeMethodsSupported   []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupport []string `json:"token_endpoint_auth_methods_supported"`
}

// DiscoverOAuth follows the MCP authorization discovery chain: a protected
// resource metadata document (RFC 9728), then OAuth authorization-server
// metadata (RFC 8414, with OIDC discovery as a compatibility fallback).
// Every fetched URL is independently checked as public HTTPS and every client
// disables proxies and cross-host redirects.
func DiscoverOAuth(ctx context.Context, rawEndpoint string, allowedHosts []string) (OAuthMetadata, error) {
	endpoint, err := ValidatePublicHTTPSEndpoint(ctx, rawEndpoint, allowedHosts, nil)
	if err != nil {
		return OAuthMetadata{}, err
	}
	metadataURL := probeResourceMetadataURL(ctx, endpoint)
	if metadataURL == "" {
		metadataURL = protectedResourceMetadataURL(endpoint)
	}
	resourceURL, err := validateOAuthURL(ctx, metadataURL)
	if err != nil {
		return OAuthMetadata{}, fmt.Errorf("protected resource metadata URL: %w", err)
	}
	var resource protectedResourceMetadata
	if err := getOAuthJSON(ctx, resourceURL, &resource); err != nil {
		return OAuthMetadata{}, fmt.Errorf("load protected resource metadata: %w", err)
	}
	if resource.Resource != "" && resource.Resource != endpoint.String() {
		return OAuthMetadata{}, errors.New("protected resource metadata does not match the MCP endpoint")
	}
	if len(resource.AuthorizationServers) == 0 {
		return OAuthMetadata{}, errors.New("protected resource metadata does not advertise an authorization server")
	}

	issuer, err := validateOAuthURL(ctx, resource.AuthorizationServers[0])
	if err != nil {
		return OAuthMetadata{}, fmt.Errorf("authorization server: %w", err)
	}
	var server authorizationServerMetadata
	var discoveryErr error
	for _, candidate := range authorizationMetadataURLs(issuer) {
		candidateURL, validateErr := validateOAuthURL(ctx, candidate)
		if validateErr != nil {
			discoveryErr = validateErr
			continue
		}
		if loadErr := getOAuthJSON(ctx, candidateURL, &server); loadErr == nil {
			discoveryErr = nil
			break
		} else {
			discoveryErr = loadErr
		}
	}
	if discoveryErr != nil {
		return OAuthMetadata{}, fmt.Errorf("load authorization server metadata: %w", discoveryErr)
	}
	if server.AuthorizationEndpoint == "" || server.TokenEndpoint == "" {
		return OAuthMetadata{}, errors.New("authorization server metadata is missing required endpoints")
	}
	if len(server.CodeChallengeMethodsSupported) > 0 && !containsString(server.CodeChallengeMethodsSupported, "S256") {
		return OAuthMetadata{}, errors.New("authorization server does not support PKCE S256")
	}
	metadata := OAuthMetadata{
		ResourceEndpoint:      endpoint.String(),
		AuthorizationEndpoint: server.AuthorizationEndpoint,
		TokenEndpoint:         server.TokenEndpoint,
		RegistrationEndpoint:  server.RegistrationEndpoint,
		Scopes:                append([]string(nil), resource.ScopesSupported...),
		TokenAuthMethods:      append([]string(nil), server.TokenEndpointAuthMethodsSupport...),
	}
	if err := ValidateOAuthMetadata(ctx, metadata); err != nil {
		return OAuthMetadata{}, fmt.Errorf("authorization server endpoint: %w", err)
	}
	return metadata, nil
}

func probeResourceMetadataURL(ctx context.Context, endpoint *url.URL) string {
	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"multica-oauth-discovery","version":"1"}}}`)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return ""
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := NewSecureHTTPClient(endpoint).Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxOAuthResponseBytes))
	for _, challenge := range response.Header.Values("WWW-Authenticate") {
		match := resourceMetadataParameter.FindStringSubmatch(challenge)
		if len(match) == 2 {
			return match[1]
		}
	}
	return ""
}

func protectedResourceMetadataURL(resource *url.URL) string {
	result := *resource
	path := strings.TrimSuffix(resource.EscapedPath(), "/")
	if path == "" {
		path = "/"
	}
	if path == "/" {
		result.Path = "/.well-known/oauth-protected-resource"
	} else {
		result.Path = "/.well-known/oauth-protected-resource" + path
	}
	result.RawPath = ""
	result.RawQuery = ""
	result.Fragment = ""
	return result.String()
}

func authorizationMetadataURLs(issuer *url.URL) []string {
	path := strings.TrimSuffix(issuer.EscapedPath(), "/")
	result := make([]string, 0, 2)
	for _, wellKnown := range []string{"oauth-authorization-server", "openid-configuration"} {
		candidate := *issuer
		candidate.Path = "/.well-known/" + wellKnown + path
		candidate.RawPath = ""
		candidate.RawQuery = ""
		candidate.Fragment = ""
		result = append(result, candidate.String())
	}
	return result
}

func validateOAuthURL(ctx context.Context, raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("URL must be public HTTPS without userinfo or fragment")
	}
	check := *parsed
	check.RawQuery = ""
	if _, err := ValidatePublicHTTPSEndpoint(ctx, check.String(), nil, nil); err != nil {
		return nil, err
	}
	return parsed, nil
}

func getOAuthJSON(ctx context.Context, endpoint *url.URL, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	return doOAuthJSON(NewSecureHTTPClient(endpoint), request, target)
}

func doOAuthJSON(client *http.Client, request *http.Request, target any) error {
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxOAuthResponseBytes))
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxOAuthResponseBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxOAuthResponseBytes {
		return errors.New("OAuth response exceeds size limit")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	return nil
}

// RegisterOAuthClient uses RFC 7591 dynamic client registration. A server
// without a registration endpoint can still be connected through the advanced
// path by supplying a pre-registered client id.
func RegisterOAuthClient(ctx context.Context, metadata OAuthMetadata, redirectURI string) (OAuthClientRegistration, error) {
	if metadata.RegistrationEndpoint == "" {
		return OAuthClientRegistration{}, errors.New("authorization server requires a pre-registered OAuth client")
	}
	endpoint, err := validateOAuthURL(ctx, metadata.RegistrationEndpoint)
	if err != nil {
		return OAuthClientRegistration{}, err
	}
	body, _ := json.Marshal(map[string]any{
		"client_name":                "Multica",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return OAuthClientRegistration{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	var response struct {
		ClientID                string `json:"client_id"`
		ClientSecret            string `json:"client_secret"`
		TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
	}
	if err := doOAuthJSON(NewSecureHTTPClient(endpoint), request, &response); err != nil {
		return OAuthClientRegistration{}, fmt.Errorf("dynamic client registration: %w", err)
	}
	if strings.TrimSpace(response.ClientID) == "" {
		return OAuthClientRegistration{}, errors.New("dynamic client registration returned no client id")
	}
	if response.TokenEndpointAuthMethod == "" {
		response.TokenEndpointAuthMethod = "none"
	}
	return OAuthClientRegistration(response), nil
}

func BuildAuthorizationURL(metadata OAuthMetadata, registration OAuthClientRegistration, redirectURI, state, verifier, scope string) (string, error) {
	endpoint, err := url.Parse(metadata.AuthorizationEndpoint)
	if err != nil {
		return "", err
	}
	challenge := sha256.Sum256([]byte(verifier))
	query := endpoint.Query()
	query.Set("response_type", "code")
	query.Set("client_id", registration.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("state", state)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	query.Set("code_challenge_method", "S256")
	query.Set("resource", metadata.ResourceEndpoint)
	if strings.TrimSpace(scope) != "" {
		query.Set("scope", strings.TrimSpace(scope))
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func ExchangeOAuthCode(ctx context.Context, tokenEndpoint, resource, code, redirectURI, verifier string, registration OAuthClientRegistration) (OAuthTokenResponse, error) {
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {registration.ClientID},
		"code_verifier": {verifier},
		"resource":      {resource},
	}
	return requestOAuthToken(ctx, tokenEndpoint, values, registration)
}

func RefreshOAuthToken(ctx context.Context, tokenEndpoint, resource, refreshToken string, registration OAuthClientRegistration) (OAuthTokenResponse, error) {
	values := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {registration.ClientID},
		"resource":      {resource},
	}
	return requestOAuthToken(ctx, tokenEndpoint, values, registration)
}

func requestOAuthToken(ctx context.Context, rawEndpoint string, values url.Values, registration OAuthClientRegistration) (OAuthTokenResponse, error) {
	endpoint, err := validateOAuthURL(ctx, rawEndpoint)
	if err != nil {
		return OAuthTokenResponse{}, err
	}
	if registration.ClientSecret != "" && registration.TokenEndpointAuthMethod == "client_secret_post" {
		values.Set("client_secret", registration.ClientSecret)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(values.Encode()))
	if err != nil {
		return OAuthTokenResponse{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if registration.ClientSecret != "" && registration.TokenEndpointAuthMethod == "client_secret_basic" {
		request.SetBasicAuth(registration.ClientID, registration.ClientSecret)
	}
	var response struct {
		AccessToken  string          `json:"access_token"`
		TokenType    string          `json:"token_type"`
		ExpiresIn    json.RawMessage `json:"expires_in"`
		RefreshToken string          `json:"refresh_token"`
		Scope        string          `json:"scope"`
	}
	if err := doOAuthJSON(NewSecureHTTPClient(endpoint), request, &response); err != nil {
		return OAuthTokenResponse{}, err
	}
	if response.AccessToken == "" || !strings.EqualFold(response.TokenType, "Bearer") {
		return OAuthTokenResponse{}, errors.New("token endpoint did not return a Bearer access token")
	}
	expiresIn := int64(0)
	if len(response.ExpiresIn) > 0 {
		if err := json.Unmarshal(response.ExpiresIn, &expiresIn); err != nil {
			var text string
			if json.Unmarshal(response.ExpiresIn, &text) == nil {
				_, _ = fmt.Sscan(text, &expiresIn)
			}
		}
	}
	return OAuthTokenResponse{
		AccessToken: response.AccessToken, TokenType: "Bearer", ExpiresIn: expiresIn,
		RefreshToken: response.RefreshToken, Scope: response.Scope,
	}, nil
}

func OAuthExpiry(now time.Time, expiresIn int64) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(expiresIn) * time.Second)
}
