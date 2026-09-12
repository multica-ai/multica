package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const documentRequestTimeout = 30 * time.Second

// DocumentAPIClient is the bounded Docs AI surface used by the document service.
type DocumentAPIClient interface {
	FetchDocument(context.Context, InstallationCredentials, DocumentFetchParams) (DocumentSnapshot, error)
	UpdateDocument(context.Context, InstallationCredentials, DocumentUpdateParams) (DocumentUpdateResult, error)
}

// OpenPlatformClient combines the existing messaging API with Docs AI.
type OpenPlatformClient interface {
	APIClient
	DocumentAPIClient
}

// DocumentFetchParams selects one bounded Docs AI read.
type DocumentFetchParams struct {
	Token        string
	Format       string
	Scope        DocumentFetchScope
	Keyword      string
	StartBlockID string
	EndBlockID   string
}

// DocumentSnapshot is the document state needed for guarded edits.
type DocumentSnapshot struct {
	RevisionID int64
	Content    string
	BlockIDs   []string
}

// DocumentUpdateParams describes one fixed Docs AI update command.
type DocumentUpdateParams struct {
	Token        string
	Format       string
	Command      string
	Content      string
	Pattern      string
	BlockID      string
	StartBlockID string
	EndBlockID   string
	RevisionID   int64
}

// DocumentUpdateResult identifies the committed document revision.
type DocumentUpdateResult struct {
	RevisionID int64
}

type documentAPIEnvelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Result   string `json:"result"`
		Document struct {
			RevisionID int64  `json:"revision_id"`
			Content    string `json:"content"`
		} `json:"document"`
	} `json:"data"`
}

func (c *httpAPIClient) FetchDocument(ctx context.Context, creds InstallationCredentials, p DocumentFetchParams) (DocumentSnapshot, error) {
	body, err := documentFetchBody(p)
	if err != nil {
		return DocumentSnapshot{}, err
	}
	path := "/open-apis/docs_ai/v1/documents/" + url.PathEscape(p.Token) + "/fetch"
	ctx, cancel := context.WithTimeout(ctx, documentRequestTimeout)
	defer cancel()

	var resp documentAPIEnvelope
	for attempt := 0; attempt < 2; attempt++ {
		resp = documentAPIEnvelope{}
		token, tokenErr := c.tenantAccessToken(ctx, creds)
		if tokenErr != nil {
			return DocumentSnapshot{}, fmt.Errorf("lark document fetch: %w", tokenErr)
		}
		err = c.doDocumentJSON(ctx, creds, http.MethodPost, path, token, body, &resp)
		code := larkErrorCode(err)
		if err == nil {
			code = resp.Code
		}
		if !isTokenError(code) || attempt == 1 {
			break
		}
		c.invalidateToken(creds.AppID)
	}
	if err != nil {
		return DocumentSnapshot{}, fmt.Errorf("lark document fetch: %w", err)
	}
	if resp.Code != 0 {
		return DocumentSnapshot{}, &APIError{Op: "fetch document", Code: resp.Code}
	}
	return DocumentSnapshot{
		RevisionID: resp.Data.Document.RevisionID,
		Content:    resp.Data.Document.Content,
		BlockIDs:   documentBlockIDs(resp.Data.Document.Content),
	}, nil
}

func (c *httpAPIClient) UpdateDocument(ctx context.Context, creds InstallationCredentials, p DocumentUpdateParams) (DocumentUpdateResult, error) {
	body, err := documentUpdateBody(p)
	if err != nil {
		return DocumentUpdateResult{}, err
	}
	path := "/open-apis/docs_ai/v1/documents/" + url.PathEscape(p.Token)
	ctx, cancel := context.WithTimeout(ctx, documentRequestTimeout)
	defer cancel()

	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return DocumentUpdateResult{}, fmt.Errorf("lark document update: %w", err)
	}
	var resp documentAPIEnvelope
	err = c.doDocumentJSON(ctx, creds, http.MethodPut, path, token, body, &resp)
	code := larkErrorCode(err)
	if err == nil {
		code = resp.Code
	}
	if isTokenError(code) {
		c.invalidateToken(creds.AppID)
	}
	if err != nil {
		return DocumentUpdateResult{}, fmt.Errorf("lark document update: %w", err)
	}
	if resp.Code != 0 {
		return DocumentUpdateResult{}, &APIError{Op: "update document", Code: resp.Code}
	}
	if resp.Data.Result != "success" {
		return DocumentUpdateResult{}, &documentOperationError{partial: resp.Data.Result == "partial_success"}
	}
	return DocumentUpdateResult{RevisionID: resp.Data.Document.RevisionID}, nil
}

func documentFetchBody(p DocumentFetchParams) (map[string]any, error) {
	if p.Token == "" {
		return nil, errors.New("lark document fetch: missing token")
	}
	if p.Format == "" {
		p.Format = "xml"
	}
	if p.Format != "xml" && p.Format != "markdown" {
		return nil, errors.New("lark document fetch: unsupported format")
	}
	body := map[string]any{
		"format": p.Format,
		"export_option": map[string]any{
			"export_block_id": p.Format == "xml",
		},
	}
	scope := p.Scope
	if scope == "" {
		scope = DocumentFetchScopeFull
	}
	var readOption map[string]any
	switch scope {
	case DocumentFetchScopeFull:
	case DocumentFetchScopeOutline:
		readOption = map[string]any{"read_mode": "outline"}
	case DocumentFetchScopeSection:
		if p.StartBlockID == "" {
			return nil, errors.New("lark document fetch: section requires a start block")
		}
		readOption = map[string]any{"read_mode": "section", "start_block_id": p.StartBlockID}
	case DocumentFetchScopeKeyword:
		if p.Keyword == "" {
			return nil, errors.New("lark document fetch: keyword is required")
		}
		readOption = map[string]any{"read_mode": "keyword", "keyword": p.Keyword}
	case DocumentFetchScopeRange:
		if p.StartBlockID == "" || p.EndBlockID == "" {
			return nil, errors.New("lark document fetch: range requires start and end blocks")
		}
		readOption = map[string]any{"read_mode": "range", "start_block_id": p.StartBlockID, "end_block_id": p.EndBlockID}
	default:
		return nil, errors.New("lark document fetch: unsupported scope")
	}
	if readOption != nil {
		body["read_option"] = readOption
	}
	return body, nil
}

func documentUpdateBody(p DocumentUpdateParams) (map[string]any, error) {
	if p.Token == "" {
		return nil, errors.New("lark document update: missing token")
	}
	if p.Format == "" {
		p.Format = "xml"
	}
	if p.Format != "xml" && p.Format != "markdown" {
		return nil, errors.New("lark document update: unsupported format")
	}
	command := p.Command
	blockID := p.BlockID
	if command == "append" {
		command = "block_insert_after"
		blockID = "-1"
	}
	switch command {
	case "str_replace", "block_insert_after", "block_replace", "block_delete":
	default:
		return nil, errors.New("lark document update: unsupported command")
	}
	body := map[string]any{"format": p.Format, "command": command}
	for key, value := range map[string]string{
		"content": p.Content, "pattern": p.Pattern, "block_id": blockID,
		"start_block_id": p.StartBlockID, "end_block_id": p.EndBlockID,
	} {
		if value != "" {
			body[key] = value
		}
	}
	if p.RevisionID > 0 {
		body["revision_id"] = p.RevisionID
	}
	return body, nil
}

func (c *httpAPIClient) doDocumentJSON(ctx context.Context, creds InstallationCredentials, method, path, token string, body, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return errors.New("lark document request: encode failed")
	}
	if len(encoded) > DocumentMaxRequestBytes {
		return errors.New("lark document request: request too large")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.resolveBaseURL(creds)+path, bytes.NewReader(encoded))
	if err != nil {
		return errors.New("lark document request: create failed")
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return &documentTransportError{cause: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, DocumentMaxResponseBytes+1))
	if err != nil {
		return errors.New("lark document response: read failed")
	}
	if len(raw) > DocumentMaxResponseBytes {
		return errors.New("lark document response: response too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code, _ := parseLarkErrorBody(raw)
		return &larkAPIStatusError{StatusCode: resp.StatusCode, Code: code}
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return errors.New("lark document response: decode failed")
		}
	}
	return nil
}

type documentTransportError struct {
	cause error
}

func (e *documentTransportError) Error() string { return "lark document request: transport failed" }
func (e *documentTransportError) Unwrap() error { return e.cause }

// documentOperationError represents a failed or partial Docs AI write. The
// upstream response is intentionally omitted because it can contain document
// content; callers must treat partial writes as uncertain and verify by reading.
type documentOperationError struct {
	partial bool
}

func (e *documentOperationError) Error() string {
	return "lark document update: operation did not fully succeed"
}

func documentBlockIDs(content string) []string {
	decoder := xml.NewDecoder(strings.NewReader("<root>" + content + "</root>"))
	seen := make(map[string]struct{})
	var ids []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range start.Attr {
			if attr.Name.Local != "block-id" && attr.Name.Local != "block_id" {
				continue
			}
			if _, exists := seen[attr.Value]; exists || attr.Value == "" {
				continue
			}
			seen[attr.Value] = struct{}{}
			ids = append(ids, attr.Value)
		}
	}
	return ids
}
