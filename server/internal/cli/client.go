package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ClientVersion is the CLI version sent on every request as X-Client-Version.
// Set by the multica binary at init() so the package doesn't depend on the
// concrete cmd package. Defaults to "dev" when running unset (e.g. tests).
var ClientVersion = "dev"

// ClientPlatform identifies this client to the server. Override for tests
// or alternative entry points; defaults to "cli".
var ClientPlatform = "cli"

// ClientOS is the normalized operating system string sent as X-Client-OS.
// Computed once from runtime.GOOS so the server doesn't need to reverse-map
// Go's os names ("darwin"/"windows"/"linux") into the protocol vocabulary.
var ClientOS = normalizeGOOS(runtime.GOOS)

func normalizeGOOS(goos string) string {
	switch goos {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return goos
	}
}

// APIClient is a REST client for the Multica server API.
// Used by ctrl subcommands (agent, runtime, status, etc.). Requests
// automatically include auth and execution context headers when configured.
type APIClient struct {
	BaseURL     string
	WorkspaceID string
	Token       string
	AgentID     string // When set, requests are attributed to this agent instead of the user.
	TaskID      string // When set, sent as X-Task-ID for agent-task validation.
	HTTPClient  *http.Client

	// Identity overrides. Empty values fall back to the package-level
	// ClientPlatform / ClientVersion / ClientOS.
	Platform string
	Version  string
	OS       string
}

type HTTPError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s returned %d: %s", e.Method, e.Path, e.StatusCode, strings.TrimSpace(e.Body))
}

// NewAPIClient creates a new API client for ctrl commands.
func NewAPIClient(baseURL, workspaceID, token string) *APIClient {
	return &APIClient{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		WorkspaceID: workspaceID,
		Token:       token,
		HTTPClient:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *APIClient) setHeaders(req *http.Request) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.WorkspaceID != "" {
		req.Header.Set("X-Workspace-ID", c.WorkspaceID)
	}
	if c.AgentID != "" {
		req.Header.Set("X-Agent-ID", c.AgentID)
	}
	if c.TaskID != "" {
		req.Header.Set("X-Task-ID", c.TaskID)
	}

	platform := c.Platform
	if platform == "" {
		platform = ClientPlatform
	}
	if platform != "" {
		req.Header.Set("X-Client-Platform", platform)
	}
	version := c.Version
	if version == "" {
		version = ClientVersion
	}
	if version != "" {
		req.Header.Set("X-Client-Version", version)
	}
	osName := c.OS
	if osName == "" {
		osName = ClientOS
	}
	if osName != "" {
		req.Header.Set("X-Client-OS", osName)
	}
}

// GetJSON performs a GET request and decodes the JSON response.
func (c *APIClient) GetJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GET %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// GetJSONWithHeaders performs a GET request, decodes the JSON response, and
// returns the response headers. Useful when callers need header values like
// X-Total-Count for pagination.
func (c *APIClient) GetJSONWithHeaders(ctx context.Context, path string, out any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GET %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.Header, err
		}
	}
	return resp.Header, nil
}

// DeleteJSON performs a DELETE request.
func (c *APIClient) DeleteJSON(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("DELETE %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

// DeleteJSONWithBody performs a DELETE request with a JSON body.
func (c *APIClient) DeleteJSONWithBody(ctx context.Context, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respData, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("DELETE %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(respData)))
	}
	return nil
}

// PostJSON performs a POST request with a JSON body.
func (c *APIClient) PostJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respData, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPError{
			Method:     http.MethodPost,
			Path:       path,
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(respData)),
		}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// PutJSON performs a PUT request with a JSON body.
func (c *APIClient) PutJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respData, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("PUT %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(respData)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// PatchJSON performs a PATCH request with a JSON body.
func (c *APIClient) PatchJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respData, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("PATCH %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(respData)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// AttachmentResponse mirrors the server's upload-file response.
type AttachmentResponse struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	DownloadURL string `json:"download_url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   string `json:"created_at"`
}

// UploadFile uploads a file via multipart form to /api/upload-file.
// It returns the attachment ID from the server response.
func (c *APIClient) UploadFile(ctx context.Context, fileData []byte, filename string, issueID string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return "", fmt.Errorf("write file data: %w", err)
	}

	if issueID != "" {
		if err := writer.WriteField("issue_id", issueID); err != nil {
			return "", fmt.Errorf("write issue_id field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/upload-file", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respData, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("upload file returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respData)))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}

	id, _ := result["id"].(string)
	if id == "" {
		return "", fmt.Errorf("upload response missing attachment id")
	}
	return id, nil
}

// UploadFileWithURL uploads a file via multipart form to /api/upload-file
// without associating it with an issue or comment. It decodes the full
// AttachmentResponse and returns the attachment ID and URL.
func (c *APIClient) UploadFileWithURL(ctx context.Context, fileData []byte, filename string) (string, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return "", "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return "", "", fmt.Errorf("write file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/upload-file", &body)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setHeaders(req)

	// Use a client that respects the context deadline for slow uploads
	// (e.g. avatar uploads with 5MB files). The default 15s HTTP client
	// timeout shadows any longer context deadline.
	httpClient := c.HTTPClient
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > httpClient.Timeout {
			clientCopy := *httpClient
			clientCopy.Timeout = remaining
			httpClient = &clientCopy
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respData, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", "", fmt.Errorf("upload file returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respData)))
	}

	var result AttachmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("decode upload response: %w", err)
	}
	if result.URL == "" {
		return "", "", fmt.Errorf("upload response missing attachment url")
	}
	// Allow empty ID: the server returns id="" in the fallback path where
	// S3 upload succeeded but the attachment DB record failed. The file
	// is still usable via its URL.
	return result.ID, result.URL, nil
}

// DownloadFile downloads a file from the given URL and returns the response body.
// This is used for downloading attachments via their signed download_url.
// Downloads are limited to 100 MB to match the upload size limit.
//
// The URL may be absolute (a signed CloudFront/S3 URL) or relative
// (a server-relative path like "/uploads/...") depending on how the
// server is configured. Relative URLs are resolved against the client's
// BaseURL and sent with the standard auth headers; absolute URLs are
// used as-is so that their query-string signatures are not disturbed.
func (c *APIClient) DownloadFile(ctx context.Context, downloadURL string) ([]byte, error) {
	isRelative := !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://")
	if isRelative {
		if c.BaseURL == "" {
			return nil, fmt.Errorf("download URL %q is relative but client has no BaseURL", downloadURL)
		}
		downloadURL = c.BaseURL + downloadURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	if isRelative {
		c.setHeaders(req)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("download returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	const maxDownloadSize = 100 << 20 // 100 MB
	return io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize))
}

// HealthCheck hits the /health endpoint and returns the response body.
func (c *APIClient) HealthCheck(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("health check returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return strings.TrimSpace(string(data)), nil
}
