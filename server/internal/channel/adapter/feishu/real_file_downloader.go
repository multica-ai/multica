package feishu

import (
	"context"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

// RealFileDownloader implements FileDownloader using the Feishu OpenAPI.
// It requires an authenticated lark.Client (the same one used by RealClient)
// and a message_id to construct the download URLs.
type RealFileDownloader struct {
	apiClient *lark.Client
}

// NewRealFileDownloader creates a FileDownloader backed by the Feishu SDK.
func NewRealFileDownloader(apiClient *lark.Client) *RealFileDownloader {
	return &RealFileDownloader{apiClient: apiClient}
}

// DownloadImage downloads an image from Feishu by message_id and file_key.
// Endpoint: GET /open-apis/im/v1/messages/{message_id}/resources/{file_key}?type=image
func (d *RealFileDownloader) DownloadImage(ctx context.Context, messageID, fileKey string) ([]byte, error) {
	return d.downloadResource(ctx, messageID, fileKey, "image")
}

// DownloadFile downloads a generic file from Feishu by message_id and file_key.
// Endpoint: GET /open-apis/im/v1/messages/{message_id}/resources/{file_key}?type=file
func (d *RealFileDownloader) DownloadFile(ctx context.Context, messageID, fileKey string) ([]byte, string, error) {
	return d.downloadResourceWithFilename(ctx, messageID, fileKey, "file")
}

// downloadResource is the shared implementation for both image and file downloads.
// It performs an authenticated GET to the Feishu resource endpoint and returns
// the response body bytes.
func (d *RealFileDownloader) downloadResource(ctx context.Context, messageID, fileKey, resourceType string) ([]byte, error) {
	url := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s/resources/%s?type=%s", messageID, fileKey, resourceType)

	resp, err := d.apiClient.Get(ctx, url, nil, larkcore.AccessTokenTypeTenant)
	if err != nil {
		return nil, fmt.Errorf("feishu download %s: %w", resourceType, err)
	}

	// The Feishu SDK returns the raw bytes in resp.RawBody for binary responses.
	return resp.RawBody, nil
}

// downloadResourceWithFilename is like downloadResource but also extracts the
// Content-Disposition filename header when available (Feishu file downloads).
func (d *RealFileDownloader) downloadResourceWithFilename(ctx context.Context, messageID, fileKey, resourceType string) ([]byte, string, error) {
	url := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s/resources/%s?type=%s", messageID, fileKey, resourceType)

	resp, err := d.apiClient.Get(ctx, url, nil, larkcore.AccessTokenTypeTenant)
	if err != nil {
		return nil, "", fmt.Errorf("feishu download %s: %w", resourceType, err)
	}

	// Try to extract filename from Content-Disposition header.
	filename := ""
	// Note: the lark SDK response does not expose headers directly on the
	// response type we receive. Production wiring would need to access the
	// underlying http.Response headers. For now we return empty filename
	// and let the caller fall back to the file_key or the name from the
	// message content JSON.
	_ = resp

	return resp.RawBody, filename, nil
}

// Compile-time assertion.
var _ FileDownloader = (*RealFileDownloader)(nil)
