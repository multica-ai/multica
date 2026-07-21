package apps

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var errRuntimeUnavailable = errors.New("app runtime is unavailable")

type RuntimeDeploymentRequest struct {
	AppID        string `json:"app_id"`
	AppName      string `json:"app_name"`
	Version      string `json:"version"`
	BundleSHA256 string `json:"bundle_sha256"`
}

type RuntimeClient struct {
	baseURL    string
	secret     string
	httpClient *http.Client
	now        func() time.Time
}

func NewRuntimeClient(baseURL, secret string) *RuntimeClient {
	return &RuntimeClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		secret:     secret,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		now:        time.Now,
	}
}

func (c *RuntimeClient) Deploy(ctx context.Context, deployment RuntimeDeploymentRequest) error {
	body, err := json.Marshal(deployment)
	if err != nil {
		return errRuntimeUnavailable
	}
	const requestPath = "/deployments"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+requestPath, bytes.NewReader(body))
	if err != nil {
		return errRuntimeUnavailable
	}
	timestamp := c.now().UTC().Format(time.RFC3339)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Multica-Timestamp", timestamp)
	req.Header.Set("X-Multica-Signature", signRuntimeRequest(c.secret, req.Method, requestPath, body, timestamp))

	response, err := c.httpClient.Do(req)
	if err != nil {
		return errRuntimeUnavailable
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode != http.StatusAccepted {
		return errRuntimeUnavailable
	}
	return nil
}

func (c *RuntimeClient) Invoke(ctx context.Context, appID, version string, input json.RawMessage) (json.RawMessage, error) {
	requestPath := fmt.Sprintf("/workers/%s/%s/invoke", appID, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+requestPath, bytes.NewReader(input))
	if err != nil {
		return nil, errRuntimeUnavailable
	}
	timestamp := c.now().UTC().Format(time.RFC3339)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Multica-Timestamp", timestamp)
	req.Header.Set("X-Multica-Signature", signRuntimeRequest(c.secret, req.Method, requestPath, input, timestamp))
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errRuntimeUnavailable
	}
	defer response.Body.Close()
	output, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || response.StatusCode != http.StatusOK || len(output) > 1<<20 || !json.Valid(output) {
		return nil, errRuntimeUnavailable
	}
	return json.RawMessage(output), nil
}

func (c *RuntimeClient) Lifecycle(ctx context.Context, action, serviceID string) error {
	if action != "pause" && action != "delete" {
		return errRuntimeUnavailable
	}
	body, err := json.Marshal(map[string]string{"service_id": serviceID})
	if err != nil {
		return errRuntimeUnavailable
	}
	requestPath := "/lifecycle/" + action
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+requestPath, bytes.NewReader(body))
	if err != nil {
		return errRuntimeUnavailable
	}
	timestamp := c.now().UTC().Format(time.RFC3339)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Multica-Timestamp", timestamp)
	req.Header.Set("X-Multica-Signature", signRuntimeRequest(c.secret, req.Method, requestPath, body, timestamp))
	response, err := c.httpClient.Do(req)
	if err != nil {
		return errRuntimeUnavailable
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode != http.StatusNoContent {
		return errRuntimeUnavailable
	}
	return nil
}

func signRuntimeRequest(secret, method, requestPath string, body []byte, timestamp string) string {
	bodyHash := sha256.Sum256(body)
	canonical := fmt.Sprintf("%s\n%s\n%s\n%s", method, requestPath, hex.EncodeToString(bodyHash[:]), timestamp)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func verifyRuntimeSignature(secret, method, requestPath string, body []byte, timestamp, signature string, now time.Time) error {
	signedAt, err := time.Parse(time.RFC3339, timestamp)
	if err != nil || now.Sub(signedAt) > 2*time.Minute || signedAt.Sub(now) > 2*time.Minute {
		return errors.New("runtime request timestamp is invalid")
	}
	want := signRuntimeRequest(secret, method, requestPath, body, timestamp)
	if !hmac.Equal([]byte(want), []byte(signature)) {
		return errors.New("runtime request signature is invalid")
	}
	return nil
}

type bundleTokenPayload struct {
	AppID   string `json:"app_id"`
	Version string `json:"version"`
	Expires int64  `json:"exp"`
}

type invocationGrant struct {
	AppID       string `json:"app_id"`
	Version     string `json:"version"`
	WorkspaceID string `json:"workspace_id"`
	MemberID    string `json:"member_id"`
	Expires     int64  `json:"exp"`
}

func mintInvocationGrant(secret string, grant invocationGrant, expires time.Time) string {
	grant.Expires = expires.Unix()
	payload, _ := json.Marshal(grant)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("invoke." + encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyInvocationGrant(secret, token string, now time.Time) (invocationGrant, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return invocationGrant{}, errors.New("invalid invocation grant")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("invoke." + parts[0]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return invocationGrant{}, errors.New("invalid invocation grant")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return invocationGrant{}, errors.New("invalid invocation grant")
	}
	var grant invocationGrant
	if json.Unmarshal(raw, &grant) != nil || grant.AppID == "" || grant.Version == "" || grant.WorkspaceID == "" || grant.MemberID == "" || now.Unix() >= grant.Expires {
		return invocationGrant{}, errors.New("invalid invocation grant")
	}
	return grant, nil
}

func mintBundleToken(secret, appID, version string, expires time.Time) string {
	payload, _ := json.Marshal(bundleTokenPayload{AppID: appID, Version: version, Expires: expires.Unix()})
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyBundleToken(secret, token, appID, version string, now time.Time) error {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return errors.New("invalid bundle token")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return errors.New("invalid bundle token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errors.New("invalid bundle token")
	}
	var payload bundleTokenPayload
	if json.Unmarshal(raw, &payload) != nil || payload.AppID != appID || payload.Version != version || now.Unix() >= payload.Expires {
		return errors.New("invalid bundle token")
	}
	return nil
}
