package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

var _ docCommentAPIClient = (*httpAPIClient)(nil)

// docCommentPageSize is the replies API's maximum page size.
const docCommentPageSize = 100

// docCommentPath builds a drive comment endpoint path with the query every
// comment call shares: the file type, and open_id so user ids match the ids
// the rest of the package keys on.
func docCommentPath(fileToken, fileType, suffix string, extra url.Values) string {
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	q.Set("file_type", fileType)
	q.Set("user_id_type", "open_id")
	return "/open-apis/drive/v1/files/" + url.PathEscape(fileToken) + "/comments" + suffix + "?" + q.Encode()
}

// callDocAPI performs one drive call and decodes data into out (when
// non-nil). A business error is an *APIError so callers classify it by code.
func (c *httpAPIClient) callDocAPI(ctx context.Context, creds InstallationCredentials, op, method, path string, body, out any) error {
	var resp struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := c.doAuthedJSON(ctx, creds, method, path, body, &resp); err != nil {
		return fmt.Errorf("lark http client: %s: %w", op, err)
	}
	if resp.Code != 0 {
		if isTokenError(resp.Code) {
			c.invalidateToken(creds.AppID)
		}
		return &APIError{Op: op, Code: resp.Code, Msg: resp.Msg}
	}
	if out != nil && len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			return fmt.Errorf("lark http client: %s: decode data: %w", op, err)
		}
	}
	return nil
}

// GetDocComment reads one comment — anchored quote, whole-document flag and
// its first replies — via POST /drive/v1/files/:token/comments/batch_query.
// (The single-comment GET only serves whole-document comments.)
func (c *httpAPIClient) GetDocComment(ctx context.Context, creds InstallationCredentials, fileToken, fileType, commentID string) (DocComment, error) {
	var data struct {
		Items []DocComment `json:"items"`
	}
	path := docCommentPath(fileToken, fileType, "/batch_query", nil)
	if err := c.callDocAPI(ctx, creds, "get doc comment", http.MethodPost, path, map[string]any{"comment_ids": []string{commentID}}, &data); err != nil {
		return DocComment{}, err
	}
	for _, item := range data.Items {
		if item.CommentID == commentID {
			return item, nil
		}
	}
	return DocComment{}, errors.New("lark http client: get doc comment: comment not found")
}

// ListDocCommentReplies reads one page of a comment thread's replies.
func (c *httpAPIClient) ListDocCommentReplies(ctx context.Context, creds InstallationCredentials, fileToken, fileType, commentID, pageToken string) (DocCommentReplyPage, error) {
	q := url.Values{}
	q.Set("page_size", fmt.Sprint(docCommentPageSize))
	if pageToken != "" {
		q.Set("page_token", pageToken)
	}
	var page DocCommentReplyPage
	path := docCommentPath(fileToken, fileType, "/"+url.PathEscape(commentID)+"/replies", q)
	err := c.callDocAPI(ctx, creds, "list doc comment replies", http.MethodGet, path, nil, &page)
	return page, err
}

// GetDocMeta reads a document's title and URL. The URL carries the tenant's
// own domain, which the agent needs to open the document.
func (c *httpAPIClient) GetDocMeta(ctx context.Context, creds InstallationCredentials, fileToken, fileType string) (DocMeta, error) {
	var data struct {
		Metas []struct {
			DocToken string `json:"doc_token"`
			Title    string `json:"title"`
			URL      string `json:"url"`
		} `json:"metas"`
	}
	body := map[string]any{
		"request_docs": []map[string]string{{"doc_token": fileToken, "doc_type": fileType}},
		"with_url":     true,
	}
	if err := c.callDocAPI(ctx, creds, "get doc meta", http.MethodPost, "/open-apis/drive/v1/metas/batch_query", body, &data); err != nil {
		return DocMeta{}, err
	}
	for _, m := range data.Metas {
		if m.DocToken == fileToken {
			return DocMeta{Title: m.Title, URL: m.URL}, nil
		}
	}
	return DocMeta{}, errors.New("lark http client: get doc meta: document not returned")
}

// CreateDocCommentReply adds a reply to an anchored comment thread.
func (c *httpAPIClient) CreateDocCommentReply(ctx context.Context, creds InstallationCredentials, fileToken, fileType, commentID string, elements []DocCommentElement) (string, error) {
	var data struct {
		ReplyID string `json:"reply_id"`
	}
	path := docCommentPath(fileToken, fileType, "/"+url.PathEscape(commentID)+"/replies", nil)
	body := map[string]any{"content": map[string]any{"elements": elements}}
	if err := c.callDocAPI(ctx, creds, "reply to doc comment", http.MethodPost, path, body, &data); err != nil {
		return "", err
	}
	return data.ReplyID, nil
}

// CreateDocComment adds a new whole-document comment.
func (c *httpAPIClient) CreateDocComment(ctx context.Context, creds InstallationCredentials, fileToken, fileType string, elements []DocCommentElement) (string, error) {
	var data struct {
		CommentID string `json:"comment_id"`
	}
	path := docCommentPath(fileToken, fileType, "", nil)
	body := map[string]any{"reply_list": map[string]any{"replies": []any{map[string]any{"content": map[string]any{"elements": elements}}}}}
	if err := c.callDocAPI(ctx, creds, "create doc comment", http.MethodPost, path, body, &data); err != nil {
		return "", err
	}
	return data.CommentID, nil
}

// AddDocCommentReaction adds an emoji reaction to a comment reply.
func (c *httpAPIClient) AddDocCommentReaction(ctx context.Context, creds InstallationCredentials, fileToken, fileType, replyID, reactionType string) error {
	return c.updateDocCommentReaction(ctx, creds, fileToken, fileType, replyID, reactionType, "add")
}

// RemoveDocCommentReaction removes the bot's emoji reaction from a reply.
func (c *httpAPIClient) RemoveDocCommentReaction(ctx context.Context, creds InstallationCredentials, fileToken, fileType, replyID, reactionType string) error {
	return c.updateDocCommentReaction(ctx, creds, fileToken, fileType, replyID, reactionType, "delete")
}

func (c *httpAPIClient) updateDocCommentReaction(ctx context.Context, creds InstallationCredentials, fileToken, fileType, replyID, reactionType, action string) error {
	q := url.Values{}
	q.Set("file_type", fileType)
	path := "/open-apis/drive/v2/files/" + url.PathEscape(fileToken) + "/comments/reaction?" + q.Encode()
	body := map[string]string{"action": action, "reply_id": replyID, "reaction_type": reactionType}
	return c.callDocAPI(ctx, creds, action+" doc comment reaction", http.MethodPost, path, body, nil)
}
