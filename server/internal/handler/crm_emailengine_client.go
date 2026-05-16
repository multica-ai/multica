package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func useCRMEmailEngine() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("CRM_EMAIL_PROVIDER")), "emailengine")
}

func crmEmailEngineBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("EMAILENGINE_BASE_URL")), "/")
}

func crmEmailEngineAccessToken() string {
	return strings.TrimSpace(os.Getenv("EMAILENGINE_ACCESS_TOKEN"))
}

func crmEmailEngineAccount(cfg crmIMAPMailboxConfig) string {
	if value := strings.TrimSpace(os.Getenv("EMAILENGINE_ACCOUNT")); value != "" {
		return value
	}
	if cfg.ID != "" {
		return cfg.ID
	}
	return cfg.Email
}

type crmEmailEngineStatus struct {
	Enabled          bool                   `json:"enabled"`
	Configured       bool                   `json:"configured"`
	BaseURL          string                 `json:"base_url,omitempty"`
	Account          string                 `json:"account,omitempty"`
	State            string                 `json:"state,omitempty"`
	Syncing          bool                   `json:"syncing"`
	LastError        string                 `json:"last_error,omitempty"`
	Folders          []crmEmailEngineFolder `json:"folders"`
	FallbackProvider string                 `json:"fallback_provider"`
}

type crmEmailEngineFolder struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Special string `json:"special_use,omitempty"`
	Total   int    `json:"total"`
	Unread  int    `json:"unread"`
}

func fetchCRMEmailEngineStatus(cfg crmIMAPMailboxConfig) (crmEmailEngineStatus, error) {
	status := crmEmailEngineStatus{Enabled: useCRMEmailEngine(), Folders: []crmEmailEngineFolder{}, FallbackProvider: "imap_smtp"}
	baseURL := crmEmailEngineBaseURL()
	account := crmEmailEngineAccount(cfg)
	status.BaseURL = baseURL
	status.Account = account
	status.Configured = baseURL != "" && account != ""
	if !status.Enabled || !status.Configured {
		return status, nil
	}

	accountEndpoint := fmt.Sprintf("%s/v1/account/%s", baseURL, url.PathEscape(account))
	var accountPayload struct {
		State     string `json:"state"`
		Syncing   bool   `json:"syncing"`
		LastError any    `json:"lastError"`
	}
	if err := crmEmailEngineRequest(http.MethodGet, accountEndpoint, nil, &accountPayload); err != nil {
		return status, err
	}
	status.State = accountPayload.State
	status.Syncing = accountPayload.Syncing
	if accountPayload.LastError != nil {
		status.LastError = fmt.Sprint(accountPayload.LastError)
	}

	foldersEndpoint := fmt.Sprintf("%s/v1/account/%s/mailboxes", baseURL, url.PathEscape(account))
	var foldersPayload struct {
		Mailboxes []struct {
			Path       string `json:"path"`
			Name       string `json:"name"`
			SpecialUse string `json:"specialUse"`
			Messages   int    `json:"messages"`
			Unseen     int    `json:"unseen"`
		} `json:"mailboxes"`
	}
	if err := crmEmailEngineRequest(http.MethodGet, foldersEndpoint, nil, &foldersPayload); err != nil {
		return status, err
	}
	for _, folder := range foldersPayload.Mailboxes {
		name := folder.Name
		if strings.TrimSpace(name) == "" {
			name = folder.Path
		}
		status.Folders = append(status.Folders, crmEmailEngineFolder{Path: folder.Path, Name: name, Special: folder.SpecialUse, Total: folder.Messages, Unread: folder.Unseen})
	}
	return status, nil
}

func fetchCRMEmailProviderMessages(cfg crmIMAPMailboxConfig, folder string, limit int, rangeDays int, requestedUIDs []string) ([]crmIMAPFetchedMessage, error) {
	if !useCRMEmailEngine() {
		return fetchCRMIMAPMessages(cfg, folder, limit, rangeDays, requestedUIDs)
	}
	return fetchCRMEmailEngineMessages(cfg, folder, limit, rangeDays, requestedUIDs)
}

func sendCRMEmailProvider(cfg crmIMAPMailboxConfig, to, cc, bcc []string, subject, body string) error {
	if !useCRMEmailEngine() {
		return sendCRMSMTP(cfg, to, cc, bcc, subject, body)
	}
	return sendCRMEmailEngine(cfg, to, cc, bcc, subject, body)
}

func fetchCRMEmailEngineMessages(cfg crmIMAPMailboxConfig, folder string, limit int, rangeDays int, requestedUIDs []string) ([]crmIMAPFetchedMessage, error) {
	baseURL := crmEmailEngineBaseURL()
	account := crmEmailEngineAccount(cfg)
	if baseURL == "" || account == "" {
		return nil, fmt.Errorf("EmailEngine base URL and account are required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	values := url.Values{}
	values.Set("path", cleanCRMIMAPFolder(&folder))
	values.Set("pageSize", strconv.Itoa(limit))
	if len(requestedUIDs) > 0 {
		values.Set("uid", strings.Join(requestedUIDs, ","))
	}
	endpoint := fmt.Sprintf("%s/v1/account/%s/messages?%s", baseURL, url.PathEscape(account), values.Encode())
	var payload struct {
		Messages []struct {
			ID        string `json:"id"`
			UID       any    `json:"uid"`
			MessageID string `json:"messageId"`
			Subject   string `json:"subject"`
			Text      struct {
				ID string `json:"id"`
			} `json:"text"`
			Date    string                         `json:"date"`
			From    struct{ Name, Address string } `json:"from"`
			To      []struct{ Address string }     `json:"to"`
			Cc      []struct{ Address string }     `json:"cc"`
			Preview string                         `json:"preview"`
			Size    int                            `json:"size"`
		} `json:"messages"`
	}
	if err := crmEmailEngineRequest(http.MethodGet, endpoint, nil, &payload); err != nil {
		return nil, err
	}
	cutoff := time.Time{}
	if rangeDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -rangeDays)
	}
	out := make([]crmIMAPFetchedMessage, 0, len(payload.Messages))
	for _, item := range payload.Messages {
		messageDate, _ := time.Parse(time.RFC3339, item.Date)
		if !cutoff.IsZero() && !messageDate.IsZero() && messageDate.Before(cutoff) {
			continue
		}
		bodyText := item.Preview
		bodyHTML := ""
		if item.Text.ID != "" {
			if text, html, err := fetchCRMEmailEngineText(baseURL, account, item.Text.ID); err == nil {
				bodyText = text
				bodyHTML = html
			}
		}
		out = append(out, crmIMAPFetchedMessage{
			UID: item.ID, MessageID: item.MessageID, Subject: item.Subject,
			FromEmail: item.From.Address, FromName: item.From.Name, ToEmails: emailEngineAddressList(item.To),
			CcEmails: emailEngineAddressList(item.Cc), Date: messageDate, BodyText: bodyText, BodyHTML: bodyHTML, Snippet: item.Preview, RawSize: item.Size,
		})
	}
	return out, nil
}

func fetchCRMEmailEngineText(baseURL, account, textID string) (string, string, error) {
	endpoint := fmt.Sprintf("%s/v1/account/%s/text/%s", baseURL, url.PathEscape(account), url.PathEscape(textID))
	var payload struct {
		Plain string `json:"plain"`
		HTML  string `json:"html"`
	}
	if err := crmEmailEngineRequest(http.MethodGet, endpoint, nil, &payload); err != nil {
		return "", "", err
	}
	return payload.Plain, payload.HTML, nil
}

func sendCRMEmailEngine(cfg crmIMAPMailboxConfig, to, cc, bcc []string, subject, body string) error {
	baseURL := crmEmailEngineBaseURL()
	account := crmEmailEngineAccount(cfg)
	if baseURL == "" || account == "" {
		return fmt.Errorf("EmailEngine base URL and account are required")
	}
	payload := map[string]any{
		"from": map[string]string{"name": cfg.Label, "address": cfg.Email},
		"to":   emailEngineRecipients(to), "cc": emailEngineRecipients(cc), "bcc": emailEngineRecipients(bcc),
		"subject": subject,
		"text":    body,
	}
	endpoint := fmt.Sprintf("%s/v1/account/%s/submit", baseURL, url.PathEscape(account))
	return crmEmailEngineRequest(http.MethodPost, endpoint, payload, nil)
}

func crmEmailEngineRequest(method, endpoint string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := crmEmailEngineAccessToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("EmailEngine %s %s failed: %s", method, endpoint, strings.TrimSpace(string(data)))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func emailEngineAddressList(values []struct{ Address string }) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.Address) != "" {
			out = append(out, strings.TrimSpace(value.Address))
		}
	}
	return out
}

func emailEngineRecipients(values []string) []map[string]string {
	out := make([]map[string]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, map[string]string{"address": strings.TrimSpace(value)})
		}
	}
	return out
}
