package lark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// ListNativeHistoryPage makes one list request, with no authorization retry.
// Its cursor is used only inside one bounded task read, never accepted from an
// agent or persisted as authorization for a later read.
func (c *httpAPIClient) ListNativeHistoryPage(ctx context.Context, creds InstallationCredentials, p ListMessagesParams, cursor string) (NativeHistoryPage, error) {
	if p.ChatID == "" || len(cursor) > 4096 {
		return NativeHistoryPage{}, errors.New("invalid native history request")
	}
	q := url.Values{"container_id_type": {"chat"}, "container_id": {string(p.ChatID)}, "sort_type": {"ByCreateTimeDesc"}, "page_size": {"50"}, "user_id_type": {"open_id"}}
	if p.ThreadID != "" {
		q.Set("container_id_type", "thread")
		q.Set("container_id", p.ThreadID)
	}
	if cursor != "" {
		q.Set("page_token", cursor)
	}
	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return NativeHistoryPage{}, errors.New("native history authentication unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolveBaseURL(creds)+"/open-apis/im/v1/messages?"+q.Encode(), nil)
	if err != nil {
		return NativeHistoryPage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return NativeHistoryPage{}, errors.New("native history request failed")
	}
	defer resp.Body.Close()
	const maxBody = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return NativeHistoryPage{}, errors.New("native history response exceeds read budget")
	}
	var envelope struct {
		Code *int `json:"code"`
		Data struct {
			Items     []larkRESTMessageItem `json:"items"`
			HasMore   *bool                 `json:"has_more"`
			PageToken string                `json:"page_token"`
		} `json:"data"`
	}
	decodeErr := json.Unmarshal(body, &envelope)
	// Evict rejected tokens for the next independent read, without replaying
	// this request or changing permission-refusal handling.
	if decodeErr == nil && envelope.Code != nil && isTokenError(*envelope.Code) {
		c.invalidateToken(creds.AppID)
	}
	// Only documented access refusals on successful/business-error responses
	// establish unavailability. Unknown codes and 5xx remain read failures.
	if decodeErr == nil && envelope.Code != nil && (resp.StatusCode >= 200 && resp.StatusCode < 300 || resp.StatusCode == 400 || resp.StatusCode == 403) && nativeHistoryAccessRefused(*envelope.Code) {
		return NativeHistoryPage{}, ErrNativeHistoryUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return NativeHistoryPage{}, errors.New("native history HTTP status " + strconv.Itoa(resp.StatusCode))
	}
	if decodeErr != nil || envelope.Code == nil || *envelope.Code != 0 {
		return NativeHistoryPage{}, errors.New("native history refused or malformed response")
	}
	d := envelope.Data
	if d.HasMore == nil || len(d.Items) > 50 || len(d.PageToken) > 4096 || (*d.HasMore && d.PageToken == "") || (!*d.HasMore && d.PageToken != "") {
		return NativeHistoryPage{}, errors.New("invalid native history pagination")
	}
	out := NativeHistoryPage{HasMore: *d.HasMore, NextToken: d.PageToken}
	for _, item := range d.Items {
		out.Messages = append(out.Messages, item.normalize())
	}
	return out, nil
}

// These access/visibility restrictions are documented at:
// https://open.feishu.cn/document/server-docs/im-v1/message/list
func nativeHistoryAccessRefused(code int) bool {
	switch code {
	case 230002, 230006, 230013, 230027, 230073, 230110, 231203, 231204:
		return true
	default:
		return false
	}
}
