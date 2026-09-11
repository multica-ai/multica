package lark

// http_client_media.go — the transport half of agent-file delivery.
//
// Four methods, and they are declared on the concrete client rather than
// added to APIClient on purpose. APIClient is the seam every consumer in
// this package is wired through, including the production stub and four
// test fakes; four more methods there would mean four more no-op stubs in
// each of them for a capability exactly one call site uses. The delivery
// path asks for the narrow interface in outbound_media.go instead and
// degrades to "attachments are not deliverable here" when the client does
// not implement it — which is precisely what the stub means.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
)

const (
	// larkImageUploadMaxBytes is Lark's ceiling for image_type=message.
	// Enforced here rather than left to Lark because a file that is too
	// big has to reach the user as a sentence, and by the time Lark
	// refuses the body the delivery path has already told itself the
	// file was sendable.
	larkImageUploadMaxBytes = 10 << 20 // 10 MiB

	// larkFileUploadMaxBytes is Lark's ceiling for POST /im/v1/files.
	larkFileUploadMaxBytes = 30 << 20 // 30 MiB

	// The two upload endpoints. Both take multipart/form-data and both
	// answer with the usual {code,msg,data} envelope.
	larkUploadPathImage = "/open-apis/im/v1/images"
	larkUploadPathFile  = "/open-apis/im/v1/files"
)

// mediaUploadForm is one multipart POST to a Lark upload endpoint: a few
// text fields followed by a single file part. Both endpoints share that
// shape (image_type + image; file_type + file_name + file), so one
// builder serves both.
type mediaUploadForm struct {
	// textFields are written in order, ahead of the file part.
	textFields [][2]string
	fileField  string
	filename   string
	data       []byte
}

// build renders the form into a fresh buffer.
//
// Called once per attempt, not once per upload: the token-refresh retry
// in doAuthedUpload re-sends, and a multipart body is a one-shot reader.
func (f mediaUploadForm) build() (*bytes.Buffer, string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, kv := range f.textFields {
		if err := w.WriteField(kv[0], kv[1]); err != nil {
			return nil, "", fmt.Errorf("write field %s: %w", kv[0], err)
		}
	}
	// CreateFormFile rather than CreatePart: it escapes the filename for
	// us, and Lark reads the declared content type as a hint only — the
	// bytes are what it validates a file against.
	part, err := w.CreateFormFile(f.fileField, filepath.Base(f.filename))
	if err != nil {
		return nil, "", fmt.Errorf("create file part: %w", err)
	}
	if _, err := part.Write(f.data); err != nil {
		return nil, "", fmt.Errorf("write file part: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}
	return &body, w.FormDataContentType(), nil
}

// doAuthedUpload posts one multipart form to a Lark upload endpoint,
// minting a tenant_access_token first and refreshing it once on a token
// rejection.
//
// That contract is copied from doAuthedJSON because it is needed for the
// same reason: an app_secret can be rotated under a long-lived process,
// and the cached token then fails every call until something invalidates
// it. An upload that skipped the retry would report a credential event to
// the user as "your file could not be sent".
func (c *httpAPIClient) doAuthedUpload(ctx context.Context, creds InstallationCredentials, path string, form mediaUploadForm, out any) error {
	attempt := func(token string) error {
		body, contentType, err := form.build()
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.resolveBaseURL(creds)+path, body)
		if err != nil {
			return fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := c.cfg.HTTPClient.Do(req)
		if err != nil {
			return fmt.Errorf("http do: %w", err)
		}
		defer resp.Body.Close()
		rawBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read body: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			// Same reasoning as doJSON: Lark reports its business code in
			// the body of a non-2xx reply, so parse it instead of
			// collapsing the response into an opaque transport error.
			code, msg := parseLarkErrorBody(rawBody)
			return &larkAPIStatusError{
				StatusCode: resp.StatusCode,
				Code:       code,
				Msg:        msg,
				Raw:        truncate(string(rawBody), 512),
			}
		}
		if out != nil && len(rawBody) > 0 {
			if err := json.Unmarshal(rawBody, out); err != nil {
				return fmt.Errorf("decode body: %w (raw=%s)", err, truncate(string(rawBody), 256))
			}
		}
		return nil
	}

	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return err
	}
	err = attempt(token)
	if !isTokenError(larkErrorCode(err)) {
		return err
	}
	c.cfg.Logger.Warn("lark http client: tenant_access_token rejected on upload; refreshing and retrying once",
		"app_id", creds.AppID, "path", path, "err", err)
	c.invalidateToken(creds.AppID)
	fresh, refreshErr := c.tenantAccessToken(ctx, creds)
	if refreshErr != nil {
		return fmt.Errorf("%w (token refresh failed: %v)", err, refreshErr)
	}
	return attempt(fresh)
}

// UploadImage puts image bytes into Lark's media store and returns the
// image_key the senders consume.
//
// The size ceiling lives here rather than in the caller because the two
// endpoints disagree about it and the caller reasons in "an image" versus
// "a file" — this is the layer that knows which endpoint is which.
func (c *httpAPIClient) UploadImage(ctx context.Context, p UploadImageParams) (string, error) {
	if len(p.Data) == 0 {
		return "", errors.New("lark http client: empty image body")
	}
	if len(p.Data) > larkImageUploadMaxBytes {
		return "", fmt.Errorf("lark http client: image %q is %d bytes, over the %d-byte limit",
			p.Filename, len(p.Data), larkImageUploadMaxBytes)
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ImageKey string `json:"image_key"`
		} `json:"data"`
	}
	form := mediaUploadForm{
		// image_type=message is the only value that yields a key usable
		// in a chat; avatar keys are refused by the message senders.
		textFields: [][2]string{{"image_type", "message"}},
		fileField:  "image",
		filename:   filenameOr(p.Filename, "image"),
		data:       p.Data,
	}
	if err := c.doAuthedUpload(ctx, p.InstallationID, larkUploadPathImage, form, &resp); err != nil {
		return "", fmt.Errorf("lark http client: upload image: %w", err)
	}
	if resp.Code != 0 || resp.Data.ImageKey == "" {
		return "", &APIError{Op: "upload image", Code: resp.Code, Msg: resp.Msg}
	}
	return resp.Data.ImageKey, nil
}

// UploadFile is UploadImage's non-image sibling: bytes to Lark's file
// endpoint, file_key back.
func (c *httpAPIClient) UploadFile(ctx context.Context, p UploadFileParams) (string, error) {
	if len(p.Data) == 0 {
		return "", errors.New("lark http client: empty file body")
	}
	if len(p.Data) > larkFileUploadMaxBytes {
		return "", fmt.Errorf("lark http client: file %q is %d bytes, over the %d-byte limit",
			p.Filename, len(p.Data), larkFileUploadMaxBytes)
	}
	name := filenameOr(p.Filename, "file")
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			FileKey string `json:"file_key"`
		} `json:"data"`
	}
	form := mediaUploadForm{
		textFields: [][2]string{
			{"file_type", p.FileType},
			{"file_name", name},
		},
		fileField: "file",
		filename:  name,
		data:      p.Data,
	}
	if err := c.doAuthedUpload(ctx, p.InstallationID, larkUploadPathFile, form, &resp); err != nil {
		return "", fmt.Errorf("lark http client: upload file: %w", err)
	}
	if resp.Code != 0 || resp.Data.FileKey == "" {
		return "", &APIError{Op: "upload file", Code: resp.Code, Msg: resp.Msg}
	}
	return resp.Data.FileKey, nil
}

// SendImageMessage posts an uploaded image as its own message.
func (c *httpAPIClient) SendImageMessage(ctx context.Context, p SendImageParams) (string, error) {
	if p.ChatID == "" {
		return "", errors.New("lark http client: missing chat_id")
	}
	if p.ImageKey == "" {
		return "", errors.New("lark http client: missing image_key")
	}
	content, err := json.Marshal(map[string]string{"image_key": p.ImageKey})
	if err != nil {
		return "", fmt.Errorf("lark http client: encode image content: %w", err)
	}
	return c.sendTypedMessage(ctx, p.InstallationID, p.ChatID, "image", string(content), p.ReplyTarget, "send image message")
}

// SendFileMessage posts an uploaded file as its own message. Lark has no
// way to embed a non-image in an answer, so this is necessarily a second
// message rather than part of the reply.
func (c *httpAPIClient) SendFileMessage(ctx context.Context, p SendFileParams) (string, error) {
	if p.ChatID == "" {
		return "", errors.New("lark http client: missing chat_id")
	}
	if p.FileKey == "" {
		return "", errors.New("lark http client: missing file_key")
	}
	content, err := json.Marshal(map[string]string{"file_key": p.FileKey})
	if err != nil {
		return "", fmt.Errorf("lark http client: encode file content: %w", err)
	}
	return c.sendTypedMessage(ctx, p.InstallationID, p.ChatID, "file", string(content), p.ReplyTarget, "send file message")
}

// sendTypedMessage posts one already-encoded content body and returns
// Lark's message_id.
//
// The three senders above (card / text / markdown) predate it and keep
// their own copies of this envelope; this one exists so the two media
// senders do not add a fourth and fifth.
func (c *httpAPIClient) sendTypedMessage(ctx context.Context, creds InstallationCredentials, chatID ChatID, msgType, content string, target ReplyTarget, op string) (string, error) {
	path, body := outboundMessageRequest(chatID, msgType, content, target)
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	if err := c.doAuthedJSON(ctx, creds, http.MethodPost, path, body, &resp); err != nil {
		return "", fmt.Errorf("lark http client: %s: %w", op, err)
	}
	if resp.Code != 0 || resp.Data.MessageID == "" {
		if isTokenError(resp.Code) {
			c.invalidateToken(creds.AppID)
		}
		return "", &APIError{Op: op, Code: resp.Code, Msg: resp.Msg}
	}
	return resp.Data.MessageID, nil
}

// larkFileTypeFor maps a filename onto Lark's file_type enum: opus, mp4,
// pdf, doc, xls, ppt, stream.
//
// Only extensions that name a kind exactly are mapped, and everything
// else is `stream`. That is deliberate rather than lazy: Lark validates
// the body against the declared type and REFUSES a mismatch instead of
// downgrading it, so a wrong specific type loses the file outright while
// `stream` always sends. We cannot tell from an extension alone whether
// an `.xlsx` is one Lark's `xls` slot will accept, so the honest answer
// is the general one.
func larkFileTypeFor(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".opus":
		return "opus"
	case ".mp4":
		return "mp4"
	case ".pdf":
		return "pdf"
	case ".doc":
		return "doc"
	case ".xls":
		return "xls"
	case ".ppt":
		return "ppt"
	}
	return "stream"
}

// filenameOr keeps every log line and multipart part named, even for an
// attachment row that arrived without a filename.
func filenameOr(name, fallback string) string {
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}
