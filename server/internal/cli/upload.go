package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Streaming attachment upload.
//
// Attachments used to travel as one multipart body through the API. That puts
// every byte through whatever reverse proxy fronts the deployment, so a proxy's
// request-body limit rejects large files outright and a slow link keeps a
// request open for the whole transfer. The direct path inverts both: the file
// is PUT straight to object storage with an explicit Content-Length, and only
// metadata reaches the API.
//
// The file is streamed, never buffered. A caller that hands in a path passes it
// through LocalUploadSource, which reopens the file per attempt and reports its
// size up front, so a 100 MiB upload costs the CLI one file descriptor and a
// copy buffer rather than 100 MiB of heap — and three of them cost the same.
//
// Compatibility is the other half of the contract. A server that predates the
// endpoint answers 404/405, and one whose storage backend cannot presign — or
// that declines an upload it cannot express, such as an empty file — answers
// 501 with a stable code; all of those fall back to the multipart path. Every
// other failure (401, 403, a rejected PUT, a 5xx) is reported instead of being
// retried through the old path, because the upload may already have consumed
// the user's bytes and a blind retry would either duplicate the object or
// double the transfer.

// directUploadCapabilityUnsupportedCode is the machine-readable code a server
// returns when its storage backend cannot serve presigned uploads. It is the
// only 501 the client treats as "fall back"; a 501 with any other body is a
// server bug and is reported.
const directUploadCapabilityUnsupportedCode = "attachment_presign_unsupported"

// directUploadFallbackStatuses are the statuses that mean "this server does not
// have the direct path" rather than "the direct path failed": the route is
// absent (404 from a build without it, 405 from a router that rejects the
// method) or the capability is declined (501). Nothing else falls back.
func directUploadFallsBack(status int, body []byte) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return true
	case http.StatusNotImplemented:
		// A 501 without the capability code is not a capability declaration;
		// treating it as one would silently route a genuinely broken direct
		// path through the legacy endpoint.
		var payload struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return false
		}
		return payload.Code == directUploadCapabilityUnsupportedCode
	}
	return false
}

// UploadSource is one local file offered to the uploader. Size must be the
// number of bytes Open will produce; the direct path signs it into the PUT so
// the store itself rejects a mismatch.
//
// Open is called once per attempt, which is what lets a failed transfer be
// retried without seeking: the file is reopened from byte zero. The reader must
// be closed by the caller (uploadAttachment closes it).
type UploadSource struct {
	// Path is the local path, used for the multipart filename and error text.
	Path string
	// Size is the file's length in bytes.
	Size int64
	// Open returns a fresh reader positioned at byte zero.
	Open func() (io.ReadCloser, error)
}

// NewLocalUploadSource builds an UploadSource for a local path. It stats the
// file once here so callers can preflight every attachment (including their
// size) before any upload starts, and so the size signed into the PUT is the
// size that will be sent.
func NewLocalUploadSource(path string) (UploadSource, error) {
	info, err := os.Stat(path)
	if err != nil {
		return UploadSource{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return UploadSource{}, fmt.Errorf("%s is a directory, not a file", path)
	}
	return UploadSource{
		Path: path,
		Size: info.Size(),
		Open: func() (io.ReadCloser, error) {
			file, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			// The file may have been replaced between Stat and Open. Reporting
			// the current length here (through the reader's own size check
			// below) would silently sign a different length than the preflight
			// measured, so re-check and fail instead.
			if current, err := file.Stat(); err == nil && current.Size() != info.Size() {
				file.Close()
				return nil, fmt.Errorf("%s changed size while uploading (%d → %d bytes)", path, info.Size(), current.Size())
			}
			return file, nil
		},
	}, nil
}

// DirectUploadTarget names the binding an upload should carry. At most one of
// IssueID/CommentID, or ChatSessionID/TaskID, is set — the server rejects a
// combination the multipart endpoint would reject too.
type DirectUploadTarget struct {
	IssueID       string
	CommentID     string
	ChatSessionID string
	TaskID        string
}

// uploadAttachment uploads one source and returns the attachment metadata.
//
// It tries the direct path first and falls back to the legacy multipart request
// only when the server says the direct path does not exist here (see
// directUploadFallsBack). Any other failure is returned as-is: a 401/403 means
// the credential is wrong, and a PUT or complete-upload failure means bytes may
// already be stored — retrying through the API would duplicate them.
func (c *APIClient) uploadAttachment(ctx context.Context, source UploadSource, target DirectUploadTarget) (AttachmentResponse, error) {
	direct, err := c.directUploadAttachment(ctx, source, target)
	if err == nil {
		return direct, nil
	}
	if !errors.Is(err, errDirectUploadUnavailable) {
		return AttachmentResponse{}, err
	}
	return c.multipartUploadAttachment(ctx, source, target)
}

// errDirectUploadUnavailable marks "this server cannot do direct upload" — the
// only error uploadAttachment converts into a multipart attempt.
var errDirectUploadUnavailable = errors.New("direct upload unavailable on this server")

// directUploadAttachment performs presign → PUT → complete.
func (c *APIClient) directUploadAttachment(ctx context.Context, source UploadSource, target DirectUploadTarget) (AttachmentResponse, error) {
	if source.Size < 0 {
		return AttachmentResponse{}, fmt.Errorf("upload %s: unknown file size", source.Path)
	}
	presign, err := c.presignAttachmentUpload(ctx, source, target)
	if err != nil {
		return AttachmentResponse{}, err
	}

	if err := c.putAttachmentObject(ctx, presign.UploadURL, presign.Headers, source); err != nil {
		return AttachmentResponse{}, err
	}

	// complete-upload is idempotent, so a transient failure is retried in
	// place: the bytes are already in the bucket and the session is still
	// 'pending' until this succeeds.
	return c.completeAttachmentUpload(ctx, presign.AttachmentID)
}

type presignUploadResponse struct {
	AttachmentID string            `json:"attachment_id"`
	UploadURL    string            `json:"upload_url"`
	Method       string            `json:"method"`
	Headers      map[string]string `json:"headers"`
	ExpiresIn    int64             `json:"expires_in"`
}

// presignAttachmentUpload asks the server for a PUT credential.
func (c *APIClient) presignAttachmentUpload(ctx context.Context, source UploadSource, target DirectUploadTarget) (presignUploadResponse, error) {
	body := map[string]any{
		"filename":     filepath.Base(source.Path),
		"content_type": DetectUploadContentType(source.Path),
		"size_bytes":   source.Size,
	}
	if target.IssueID != "" {
		body["issue_id"] = target.IssueID
	}
	if target.CommentID != "" {
		body["comment_id"] = target.CommentID
	}
	if target.ChatSessionID != "" {
		body["chat_session_id"] = target.ChatSessionID
	}
	if target.TaskID != "" {
		body["task_id"] = target.TaskID
	}

	req, err := c.newJSONRequest(ctx, http.MethodPost, "/api/attachments/presign-upload", body)
	if err != nil {
		return presignUploadResponse{}, err
	}
	resp, err := c.HTTPClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return presignUploadResponse{}, err
	}
	defer resp.Body.Close()

	// The status is inspected before any fallback decision because the decision
	// depends on the body, and the body must be read before the connection can
	// be reused for the fallback request anyway.
	payload, readErr := readCappedBody(resp.Body)
	if readErr != nil {
		return presignUploadResponse{}, fmt.Errorf("read presign response: %w", wrapBodyRead(req, readErr))
	}
	if resp.StatusCode >= 400 {
		if directUploadFallsBack(resp.StatusCode, payload) {
			return presignUploadResponse{}, fmt.Errorf("%w: %s %s returned %d",
				errDirectUploadUnavailable, http.MethodPost, "/api/attachments/presign-upload", resp.StatusCode)
		}
		return presignUploadResponse{}, newHTTPErrorWithBody(http.MethodPost, "/api/attachments/presign-upload", resp.StatusCode, payload)
	}

	var out presignUploadResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return presignUploadResponse{}, fmt.Errorf("decode presign response: %w", err)
	}
	if out.AttachmentID == "" || out.UploadURL == "" {
		return presignUploadResponse{}, fmt.Errorf("presign response missing attachment_id or upload_url")
	}
	if out.Method != "" && !strings.EqualFold(out.Method, http.MethodPut) {
		return presignUploadResponse{}, fmt.Errorf("presign response asked for an unsupported method %q", out.Method)
	}
	return out, nil
}

// putAttachmentObject streams the file to the presigned URL.
//
// The request deliberately does not carry this client's headers: no
// Authorization, no cookie, no workspace or client-version header. The storage
// host is a different origin operated by the bucket owner, and sending an API
// token to it would hand a credential to whatever answers that URL. The last
// line of defense is that the URL is opaque here — nothing about it is logged,
// which is why the errors below name the file rather than the request.
func (c *APIClient) putAttachmentObject(ctx context.Context, uploadURL string, headers map[string]string, source UploadSource) error {
	reader, err := source.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", source.Path, err)
	}
	defer reader.Close()

	// A zero-byte body is sent as http.NoBody with an explicit zero length.
	// Handing net/http a non-nil reader with ContentLength 0 makes it fall back
	// to chunked transfer encoding, and S3-compatible servers reject that for a
	// presigned PUT ("UnexpectedContent: This request does not support
	// content"). NoBody is what makes the request carry no body at all, which is
	// the only well-formed empty PUT.
	//
	// Current servers never ask for one: a zero-length credential binds no size,
	// so presign-upload declines it and this client falls back to multipart
	// before reaching here. A server that predates that rule still issues the
	// URL, and a client must not turn its empty upload into a chunked request.
	var payload io.Reader = reader
	if source.Size == 0 {
		payload = http.NoBody
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, payload)
	if err != nil {
		return fmt.Errorf("build upload request: %w", err)
	}
	// Sign with the exact length the credential was issued for. A stream of
	// unknown length would go out chunked, and the signature covers the length.
	req.ContentLength = source.Size
	req.Header.Set("Content-Type", headers["Content-Type"])
	for name, value := range headers {
		if strings.EqualFold(name, "Content-Length") {
			// Set through ContentLength so net/http writes it as the framing
			// length rather than a duplicated header.
			continue
		}
		req.Header.Set(name, value)
	}

	// A dedicated client: the storage host is not the API host, so it must not
	// inherit the API client's timeout (a fixed budget would kill a large
	// upload mid-body) or any per-host state. The ceiling scales with the file
	// size at a deliberately pessimistic floor rate, so a healthily slow
	// transfer is never truncated and a wedged one still ends.
	client := newUploadHTTPClient(source.Size)
	resp, err := client.Do(req)
	if err != nil {
		// The URL is not interpolated: a wrapped *url.Error carries the full
		// signed query.
		return fmt.Errorf("upload %s to storage: %w", source.Path, sanitizeUploadTransportError(err))
	}
	defer resp.Body.Close()
	body, _ := readCappedBody(resp.Body)
	if resp.StatusCode >= 400 {
		// Object-storage errors are XML/JSON with a code but no useful remedy
		// beyond the status; include a bounded body because a signature
		// mismatch is otherwise indistinguishable from a permissions failure.
		return fmt.Errorf("upload %s to storage failed: %s", source.Path, describeStorageRejection(resp.StatusCode, body))
	}
	return nil
}

// completeAttachmentUpload publishes the uploaded object. It retries transport
// failures only: the call is idempotent (a repeated complete returns the same
// attachment), whereas a 4xx means the session or the object is gone and
// another attempt cannot fix that.
func (c *APIClient) completeAttachmentUpload(ctx context.Context, attachmentID string) (AttachmentResponse, error) {
	path := "/api/attachments/" + url.PathEscape(attachmentID) + "/complete-upload"

	var lastErr error
	for attempt := 1; attempt <= completeUploadAttempts; attempt++ {
		if attempt > 1 {
			if err := sleepCtx(ctx, completeUploadRetryDelay(attempt-1)); err != nil {
				return AttachmentResponse{}, err
			}
		}

		req, err := c.newJSONRequest(ctx, http.MethodPost, path, struct{}{})
		if err != nil {
			return AttachmentResponse{}, err
		}
		resp, err := c.HTTPClient.Do(req)
		err = wrapTransport(req, err)
		if err != nil {
			if ctx.Err() != nil {
				return AttachmentResponse{}, err
			}
			lastErr = err
			continue
		}

		payload, readErr := readCappedBody(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read complete-upload response: %w", wrapBodyRead(req, readErr))
			continue
		}
		if resp.StatusCode >= 500 {
			// A 5xx may or may not have published the attachment. Retrying is
			// safe because the call is idempotent, and stopping here would
			// leave the client unable to tell which happened.
			lastErr = newHTTPErrorWithBody(http.MethodPost, path, resp.StatusCode, payload)
			continue
		}
		if resp.StatusCode >= 400 {
			return AttachmentResponse{}, newHTTPErrorWithBody(http.MethodPost, path, resp.StatusCode, payload)
		}

		var att AttachmentResponse
		if err := json.Unmarshal(payload, &att); err != nil {
			return AttachmentResponse{}, fmt.Errorf("decode complete-upload response: %w", err)
		}
		if att.ID == "" {
			return AttachmentResponse{}, fmt.Errorf("complete-upload response missing attachment id")
		}
		return att, nil
	}
	return AttachmentResponse{}, lastErr
}

const (
	// completeUploadAttempts bounds complete-upload's in-place retries. Three
	// covers a transient network blip and one server restart without turning a
	// hard failure into a long hang.
	completeUploadAttempts = 3

	// uploadRateFloorBytesPerSecond is the pessimistic throughput used to size a
	// PUT's whole-request ceiling. 64 KiB/s is far below any link that can
	// finish a 100 MiB upload in reasonable time, so the ceiling only fires for
	// a transfer that has genuinely wedged; the stall guard handles the case
	// where it goes quiet.
	uploadRateFloorBytesPerSecond = 64 << 10

	// uploadCommandOverhead is added to a sized transfer budget for the
	// metadata requests around the transfer: presign, complete-upload, and the
	// command's own API calls.
	uploadCommandOverhead = 2 * time.Minute

	// uploadTimeoutCeiling bounds the whole command even for the largest
	// accepted file, so a wedged transfer cannot hold a terminal forever. It is
	// derived from the same pessimistic rate the transfer ceiling uses, with
	// headroom, and is a hard cap rather than a target.
	uploadTimeoutCeiling = 2 * time.Hour
)

func completeUploadRetryDelay(attempt int) time.Duration {
	return time.Duration(attempt) * time.Second
}

// uploadTransferCeiling sizes a PUT's backstop from the file length at the
// pessimistic floor rate, plus a fixed allowance for connection setup.
//
// The ceiling never drops below the shared transfer ceiling, so a small upload
// keeps exactly the transport behaviour every other command has.
func uploadTransferCeiling(size int64) time.Duration {
	seconds := size / uploadRateFloorBytesPerSecond
	ceiling := time.Duration(seconds)*time.Second + 2*time.Minute
	if floor := TransferCeiling(); ceiling < floor {
		return floor
	}
	return ceiling
}

// UploadTimeout returns the command-level budget for a bulk attachment
// transfer.
//
// The per-request HTTP timeouts below cannot help when the PARENT context
// expires first: a context.WithTimeout created before the upload starts caps
// the whole command, and no inner client timeout can extend it. Callers with
// attachments therefore size their context from this function instead of the
// historical flat 60s, which is what truncated large uploads at exactly 60.001s
// on a healthy transfer.
//
// totalBytes is the sum of every attachment in the command, so a multi-file
// call budgets for all of them rather than each one in isolation. The budget is
// bounded: the user's own cancellation still wins, and the result never exceeds
// uploadTimeoutCeiling.
func UploadTimeout(totalBytes int64) time.Duration {
	budget := APITimeout()
	if sized := uploadTransferCeiling(totalBytes) + uploadCommandOverhead; sized > budget {
		budget = sized
	}
	if budget > uploadTimeoutCeiling {
		return uploadTimeoutCeiling
	}
	return budget
}

// UploadSourcesTotalBytes sums the sizes of the files a command will upload, so
// multi-attachment commands get one budget covering all of them.
func UploadSourcesTotalBytes(sources []UploadSource) int64 {
	var total int64
	for _, source := range sources {
		if source.Size > 0 {
			total += source.Size
		}
	}
	return total
}

// newUploadHTTPClient builds the HTTP client a bulk transfer uses.
//
// It is deliberately local to this file rather than a change to
// NewStallAwareHTTPClient: every other command must keep the shared transport's
// behaviour, and only an upload of known size has a legitimate reason to widen
// the whole-request ceiling. The progress guard (StallTimeout) is unchanged, so
// a transfer that goes quiet still fails at the shared threshold; only the
// backstop that would otherwise kill a healthy long transfer moves.
func newUploadHTTPClient(size int64) *http.Client {
	client := NewStallAwareHTTPClient()
	if ceiling := uploadTransferCeiling(size); ceiling > client.Timeout {
		client.Timeout = ceiling
	}
	return client
}

// multipartUploadAttachment is the legacy path, used only when the server has no
// direct upload. It streams the source through the multipart writer instead of
// buffering it: the previous implementation read the whole file into memory
// before building the request, which for three attachments meant three files
// resident at once.
func (c *APIClient) multipartUploadAttachment(ctx context.Context, source UploadSource, target DirectUploadTarget) (AttachmentResponse, error) {
	reader, err := source.Open()
	if err != nil {
		return AttachmentResponse{}, fmt.Errorf("open %s: %w", source.Path, err)
	}
	defer reader.Close()

	// Bounded memory: the multipart body is assembled through an io.Pipe, so
	// only the copy buffer is resident no matter how large the file is.
	pipeReader, pipeWriter := io.Pipe()
	formType := make(chan string, 1)
	go func() {
		writer := multipart.NewWriter(pipeWriter)
		formType <- writer.FormDataContentType()

		part, err := writer.CreateFormFile("file", filepath.Base(source.Path))
		if err == nil {
			_, err = io.Copy(part, reader)
		}
		if err == nil {
			for field, value := range target.formFields() {
				if err = writer.WriteField(field, value); err != nil {
					break
				}
			}
		}
		if closeErr := writer.Close(); err == nil {
			err = closeErr
		}
		pipeWriter.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/upload-file", pipeReader)
	if err != nil {
		return AttachmentResponse{}, err
	}
	req.Header.Set("Content-Type", <-formType)
	c.setHeaders(req)

	// The body length is unknown (it is generated as it is read), so the
	// multipart request is chunked and the size ceiling cannot be signed. Use a
	// stall-aware client sized for the file rather than the API's fixed timeout,
	// which is what made large uploads fail deterministically.
	httpClient := newUploadHTTPClient(source.Size)
	resp, err := httpClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return AttachmentResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return AttachmentResponse{}, newHTTPError(http.MethodPost, "/api/upload-file", resp)
	}
	var result AttachmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return AttachmentResponse{}, fmt.Errorf("decode upload response: %w", wrapBodyRead(req, err))
	}
	if result.ID == "" {
		return AttachmentResponse{}, fmt.Errorf("upload response missing attachment id")
	}
	return result, nil
}

// formFields renders the target as multipart form fields.
func (t DirectUploadTarget) formFields() map[string]string {
	fields := map[string]string{}
	if t.IssueID != "" {
		fields["issue_id"] = t.IssueID
	}
	if t.CommentID != "" {
		fields["comment_id"] = t.CommentID
	}
	if t.ChatSessionID != "" {
		fields["chat_session_id"] = t.ChatSessionID
	}
	if t.TaskID != "" {
		fields["task_id"] = t.TaskID
	}
	return fields
}

// UploadAttachmentFile uploads a local file to an issue attachment slot. An
// empty issueID leaves the row unbound, which is what `issue create` needs.
func (c *APIClient) UploadAttachmentFile(ctx context.Context, path, issueID string) (AttachmentResponse, error) {
	source, err := NewLocalUploadSource(path)
	if err != nil {
		return AttachmentResponse{}, err
	}
	return c.uploadAttachment(ctx, source, DirectUploadTarget{IssueID: issueID})
}

// UploadChatAttachmentFile uploads a local file tagged with a chat task. The
// server binds the row to the assistant reply the task produces on completion.
func (c *APIClient) UploadChatAttachmentFile(ctx context.Context, path, taskID string) (AttachmentResponse, error) {
	source, err := NewLocalUploadSource(path)
	if err != nil {
		return AttachmentResponse{}, err
	}
	return c.uploadAttachment(ctx, source, DirectUploadTarget{TaskID: taskID})
}

// UploadFileWithURLStream uploads a local file without associating it with an
// issue or comment, returning the attachment id and URL.
func (c *APIClient) UploadFileWithURLStream(ctx context.Context, path string) (string, string, error) {
	source, err := NewLocalUploadSource(path)
	if err != nil {
		return "", "", err
	}
	att, err := c.uploadAttachment(ctx, source, DirectUploadTarget{})
	if err != nil {
		return "", "", err
	}
	return att.ID, att.URL, nil
}

// DetectUploadContentType infers the media type declared to the server from the
// filename extension. The server re-sniffs the stored bytes before publishing,
// so this value only shapes the PUT signature; naming it from the extension
// keeps a .svg or .json file from being declared text/plain.
func DetectUploadContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".svg":
		return "image/svg+xml"
	case ".css":
		return "text/css"
	case ".js", ".mjs":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".wasm":
		return "application/wasm"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".md", ".txt", ".log", ".csv":
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

// sanitizeUploadTransportError strips the request URL from a transport error.
// A *url.Error's string is "<method> <url>: <cause>", and for a presigned PUT
// the URL carries the signature.
func sanitizeUploadTransportError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s storage host: %w", urlErr.Op, urlErr.Err)
	}
	return err
}

// describeStorageRejection renders an object-storage rejection without echoing a
// signature: a bounded body is enough to tell a precondition failure from an
// access-denied.
func describeStorageRejection(status int, body []byte) string {
	const maxDetail = 512
	detail := strings.TrimSpace(string(body))
	if len(detail) > maxDetail {
		detail = detail[:maxDetail] + "…"
	}
	if detail == "" {
		return fmt.Sprintf("HTTP %d", status)
	}
	return fmt.Sprintf("HTTP %d: %s", status, detail)
}

// uploadStatusError carries a status code and body for a response whose body was
// already consumed by the caller.
func newHTTPErrorWithBody(method, path string, status int, body []byte) *HTTPError {
	err := &HTTPError{Method: method, Path: path, StatusCode: status, Body: strings.TrimSpace(string(body))}
	if len(err.Body) > maxErrorBodyBytes {
		err.Body = err.Body[:maxErrorBodyBytes]
	}
	return err
}

// newJSONRequest builds a JSON POST whose body is body.
func (c *APIClient) newJSONRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, strings.NewReader(string(encoded)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)
	return req, nil
}

// readCappedBody reads at most maxErrorBodyBytes so a hostile or broken response
// cannot exhaust memory.
func readCappedBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxErrorBodyBytes))
}

// sleepCtx waits for d, returning early if ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
