package feishu

import "context"

// FileDownloader abstracts the Feishu OpenAPI file download operations.
// Both the real SDK-backed implementation and test doubles implement this
// interface so the adapter layer stays unit-testable without network calls.
type FileDownloader interface {
	// DownloadImage fetches an image by its file_key and message_id.
	// message_id is required by the Feishu OpenAPI endpoint.
	// The returned bytes are the raw image data (PNG, JPEG, etc.) ready for storage upload.
	DownloadImage(ctx context.Context, messageID, fileKey string) ([]byte, error)

	// DownloadFile fetches a generic file by its file_key and message_id.
	// message_id is required by the Feishu OpenAPI endpoint.
	// The returned bytes are the raw file data and the original filename.
	DownloadFile(ctx context.Context, messageID, fileKey string) ([]byte, string, error)
}
