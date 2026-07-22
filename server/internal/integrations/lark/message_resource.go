package lark

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const maxMessageResourceBytes int64 = 100 << 20

type MessageResourceType string

const (
	MessageResourceImage MessageResourceType = "image"
	MessageResourceFile  MessageResourceType = "file"
)

type DownloadedMessageResource struct {
	Body          io.ReadCloser
	ContentType   string
	Filename      string
	ContentLength int64
}

type MessageResourceDownloader interface {
	DownloadMessageResource(ctx context.Context, creds InstallationCredentials, messageID, resourceKey string, resourceType MessageResourceType) (DownloadedMessageResource, error)
}

type messageResourceError struct {
	retryable bool
	category  string
	cause     error
}

func (e *messageResourceError) Error() string {
	return "lark message resource: " + e.category
}

func (e *messageResourceError) Unwrap() error { return e.cause }

func IsRetryableResourceError(err error) bool {
	var target *messageResourceError
	return errors.As(err, &target) && target.retryable
}

func (c *httpAPIClient) DownloadMessageResource(ctx context.Context, creds InstallationCredentials, messageID, resourceKey string, resourceType MessageResourceType) (DownloadedMessageResource, error) {
	if resourceType != MessageResourceImage && resourceType != MessageResourceFile {
		return DownloadedMessageResource{}, &messageResourceError{category: "invalid resource type"}
	}
	if messageID == "" || resourceKey == "" {
		return DownloadedMessageResource{}, &messageResourceError{category: "missing message resource reference"}
	}
	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return DownloadedMessageResource{}, &messageResourceError{retryable: true, category: "authentication unavailable", cause: err}
	}

	path := "/open-apis/im/v1/messages/" + url.PathEscape(messageID) + "/resources/" + url.PathEscape(resourceKey)
	query := url.Values{"type": []string{string(resourceType)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolveBaseURL(creds)+path+"?"+query.Encode(), nil)
	if err != nil {
		return DownloadedMessageResource{}, &messageResourceError{category: "build request", cause: err}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return DownloadedMessageResource{}, &messageResourceError{retryable: true, category: "request failed", cause: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			c.invalidateToken(creds.AppID)
		}
		retryable := resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return DownloadedMessageResource{}, &messageResourceError{
			retryable: retryable,
			category:  fmt.Sprintf("upstream HTTP %d", resp.StatusCode),
		}
	}
	if resp.ContentLength > maxMessageResourceBytes {
		_ = resp.Body.Close()
		return DownloadedMessageResource{}, &messageResourceError{category: "resource exceeds 100 MB"}
	}

	filename := ""
	if disposition := resp.Header.Get("Content-Disposition"); disposition != "" {
		if _, params, parseErr := mime.ParseMediaType(disposition); parseErr == nil {
			filename = params["filename"]
		}
	}
	contentLength := resp.ContentLength
	if contentLength < 0 {
		if raw := strings.TrimSpace(resp.Header.Get("Content-Length")); raw != "" {
			if parsed, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil {
				contentLength = parsed
			}
		}
	}
	return DownloadedMessageResource{
		Body:          resp.Body,
		ContentType:   resp.Header.Get("Content-Type"),
		Filename:      filename,
		ContentLength: contentLength,
	}, nil
}

var _ MessageResourceDownloader = (*httpAPIClient)(nil)
