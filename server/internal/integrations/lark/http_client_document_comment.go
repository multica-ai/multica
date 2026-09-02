package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (c *httpAPIClient) GetDocumentCommentContext(ctx context.Context, creds InstallationCredentials, p DocumentCommentParams) (DocumentCommentContext, error) {
	if p.FileToken == "" || p.FileType == "" || p.CommentID == "" {
		return DocumentCommentContext{}, errors.New("lark document comment: missing file or comment identifier")
	}
	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return DocumentCommentContext{}, err
	}
	var metaResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Metas []struct {
				Title string `json:"title"`
				URL   string `json:"url"`
			} `json:"metas"`
		} `json:"data"`
	}
	metaBody := map[string]any{"request_docs": []any{map[string]string{"doc_token": p.FileToken, "doc_type": p.FileType}}, "with_url": true}
	if err := c.doJSON(ctx, c.resolveBaseURL(creds), http.MethodPost, "/open-apis/drive/v1/metas/batch_query", token, metaBody, &metaResp); err != nil {
		return DocumentCommentContext{}, fmt.Errorf("lark document comment meta: %w", err)
	}
	if metaResp.Code != 0 {
		return DocumentCommentContext{}, &APIError{Op: "document comment meta", Code: metaResp.Code, Msg: metaResp.Msg}
	}
	var commentResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []driveComment `json:"items"`
		} `json:"data"`
	}
	q := url.Values{"file_type": {p.FileType}, "user_id_type": {"open_id"}}
	path := "/open-apis/drive/v1/files/" + url.PathEscape(p.FileToken) + "/comments/batch_query?" + q.Encode()
	if err := c.doJSON(ctx, c.resolveBaseURL(creds), http.MethodPost, path, token, map[string]any{"comment_ids": []string{p.CommentID}}, &commentResp); err != nil {
		return DocumentCommentContext{}, fmt.Errorf("lark document comment query: %w", err)
	}
	if commentResp.Code != 0 {
		return DocumentCommentContext{}, &APIError{Op: "document comment query", Code: commentResp.Code, Msg: commentResp.Msg}
	}
	if len(commentResp.Data.Items) == 0 {
		return DocumentCommentContext{}, errors.New("lark document comment query: comment not found")
	}
	item := commentResp.Data.Items[0]
	out := DocumentCommentContext{Quote: item.Quote, IsWhole: item.IsWhole}
	if len(metaResp.Data.Metas) > 0 {
		out.Title, out.URL = metaResp.Data.Metas[0].Title, metaResp.Data.Metas[0].URL
	}
	for _, reply := range item.ReplyList.Replies {
		out.Timeline = append(out.Timeline, DocumentCommentEntry{ReplyID: reply.ReplyID, UserID: reply.UserID.OpenID, Text: driveCommentText(reply.Content)})
	}
	return out, nil
}

func (c *httpAPIClient) ReplyDocumentComment(ctx context.Context, creds InstallationCredentials, p DocumentCommentReplyParams) (string, error) {
	if p.FileToken == "" || p.FileType == "" || p.CommentID == "" || strings.TrimSpace(p.Text) == "" {
		return "", errors.New("lark document comment reply: missing target or text")
	}
	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return "", err
	}
	q := url.Values{"file_type": {p.FileType}}
	path := "/open-apis/drive/v1/files/" + url.PathEscape(p.FileToken) + "/comments/" + url.PathEscape(p.CommentID) + "/replies?" + q.Encode()
	body := map[string]any{"content": map[string]any{"elements": []any{map[string]any{"type": "text_run", "text_run": map[string]string{"text": p.Text}}}}}
	if p.IsWhole {
		path = "/open-apis/drive/v1/files/" + url.PathEscape(p.FileToken) + "/new_comments"
		body = map[string]any{"file_type": p.FileType, "reply_elements": []any{map[string]string{"type": "text", "text": p.Text}}}
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ReplyID   string `json:"reply_id"`
			CommentID string `json:"comment_id"`
			Reply     struct {
				ReplyID string `json:"reply_id"`
			} `json:"reply"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(creds), http.MethodPost, path, token, body, &resp); err != nil {
		return "", fmt.Errorf("lark document comment reply: %w", err)
	}
	if resp.Code != 0 {
		return "", &APIError{Op: "document comment reply", Code: resp.Code, Msg: resp.Msg}
	}
	id := resp.Data.ReplyID
	if id == "" {
		id = resp.Data.Reply.ReplyID
	}
	if id == "" && p.IsWhole {
		id = resp.Data.CommentID
	}
	if id == "" {
		return "", errors.New("lark document comment reply: missing reply_id")
	}
	return id, nil
}

func (c *httpAPIClient) VerifyDocumentCommentReply(ctx context.Context, creds InstallationCredentials, p DocumentCommentParams, replyID string) error {
	if replyID == "" {
		return errors.New("lark document comment verify: missing reply_id")
	}
	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return err
	}
	if p.IsWhole {
		q := url.Values{"file_type": {p.FileType}, "is_whole": {"true"}, "page_size": {"100"}, "user_id_type": {"open_id"}}
		path := "/open-apis/drive/v1/files/" + url.PathEscape(p.FileToken) + "/comments?" + q.Encode()
		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				Items []struct {
					CommentID string `json:"comment_id"`
				} `json:"items"`
			} `json:"data"`
		}
		if err := c.doJSON(ctx, c.resolveBaseURL(creds), http.MethodGet, path, token, nil, &resp); err != nil {
			return fmt.Errorf("lark document comment verify: %w", err)
		}
		if resp.Code != 0 {
			return &APIError{Op: "document comment verify", Code: resp.Code, Msg: resp.Msg}
		}
		for _, item := range resp.Data.Items {
			if item.CommentID == replyID {
				return nil
			}
		}
		return fmt.Errorf("lark document comment verify: whole comment %s not found", replyID)
	}
	q := url.Values{"file_type": {p.FileType}, "user_id_type": {"open_id"}, "page_size": {"100"}}
	path := "/open-apis/drive/v1/files/" + url.PathEscape(p.FileToken) + "/comments/" + url.PathEscape(p.CommentID) + "/replies?" + q.Encode()
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []struct {
				ReplyID string `json:"reply_id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(creds), http.MethodGet, path, token, nil, &resp); err != nil {
		return fmt.Errorf("lark document comment verify: %w", err)
	}
	if resp.Code != 0 {
		return &APIError{Op: "document comment verify", Code: resp.Code, Msg: resp.Msg}
	}
	for _, item := range resp.Data.Items {
		if item.ReplyID == replyID {
			return nil
		}
	}
	return fmt.Errorf("lark document comment verify: reply %s not found", replyID)
}

type driveComment struct {
	CommentID string `json:"comment_id"`
	IsWhole   bool   `json:"is_whole"`
	Quote     string `json:"quote"`
	ReplyList struct {
		Replies []driveCommentReply `json:"replies"`
	} `json:"reply_list"`
}
type driveCommentReply struct {
	ReplyID string `json:"reply_id"`
	UserID  struct {
		OpenID string `json:"open_id"`
	} `json:"user_id"`
	Content json.RawMessage `json:"content"`
}

func driveCommentText(raw json.RawMessage) string {
	var content struct {
		Elements []struct {
			Type    string `json:"type"`
			TextRun struct {
				Text string `json:"text"`
			} `json:"text_run"`
			Person struct {
				UserID string `json:"user_id"`
			} `json:"person"`
		} `json:"elements"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &content) != nil {
		return ""
	}
	var b strings.Builder
	for _, e := range content.Elements {
		if e.Type == "text_run" {
			b.WriteString(e.TextRun.Text)
		} else if e.Type == "person" && e.Person.UserID != "" {
			b.WriteString("@" + e.Person.UserID)
		}
	}
	return strings.TrimSpace(b.String())
}
