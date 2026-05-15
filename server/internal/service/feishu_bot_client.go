package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type FeishuBotClient struct {
	client    *http.Client
	baseURL   string
	appID     string
	appSecret string
}

type FeishuUserInfo struct {
	Email string
	Name  string
}

func NewFeishuBotClient(appID, appSecret string) *FeishuBotClient {
	return &FeishuBotClient{
		client:    &http.Client{Timeout: 5 * time.Second},
		baseURL:   feishuDefaultBaseURL,
		appID:     strings.TrimSpace(appID),
		appSecret: strings.TrimSpace(appSecret),
	}
}

func (c *FeishuBotClient) SendText(ctx context.Context, receiveIDType, receiveID, text string) error {
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{
		"receive_id": receiveID,
		"msg_type":   "text",
		"content":    string(content),
	})
	if err != nil {
		return err
	}
	endpoint := c.baseURL + "/open-apis/im/v1/messages?receive_id_type=" + url.QueryEscape(receiveIDType)
	return c.doFeishuPost(ctx, token, endpoint, body, "Feishu message send failed")
}

func (c *FeishuBotClient) SendCard(ctx context.Context, receiveIDType, receiveID string, card map[string]any) error {
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	content, err := json.Marshal(card)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{
		"receive_id": receiveID,
		"msg_type":   "interactive",
		"content":    string(content),
	})
	if err != nil {
		return err
	}
	endpoint := c.baseURL + "/open-apis/im/v1/messages?receive_id_type=" + url.QueryEscape(receiveIDType)
	return c.doFeishuPost(ctx, token, endpoint, body, "Feishu card send failed")
}

func (c *FeishuBotClient) ReplyText(ctx context.Context, messageID, text string) error {
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{
		"msg_type": "text",
		"content":  string(content),
	})
	if err != nil {
		return err
	}
	endpoint := c.baseURL + "/open-apis/im/v1/messages/" + url.PathEscape(messageID) + "/reply"
	return c.doFeishuPost(ctx, token, endpoint, body, "Feishu message reply failed")
}

func (c *FeishuBotClient) GetUserInfo(ctx context.Context, openID string) (FeishuUserInfo, error) {
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return FeishuUserInfo{}, err
	}
	endpoint := c.baseURL + "/open-apis/contact/v3/users/" + url.PathEscape(openID) + "?user_id_type=open_id"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return FeishuUserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.client.Do(req)
	if err != nil {
		return FeishuUserInfo{}, err
	}
	defer resp.Body.Close()
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			User struct {
				Email           string `json:"email"`
				EnterpriseEmail string `json:"enterprise_email"`
				Name            string `json:"name"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return FeishuUserInfo{}, fmt.Errorf("decode Feishu user response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || parsed.Code != 0 {
		return FeishuUserInfo{}, fmt.Errorf("Feishu user lookup failed: status=%d code=%d msg=%s", resp.StatusCode, parsed.Code, parsed.Msg)
	}
	email := strings.TrimSpace(parsed.Data.User.EnterpriseEmail)
	if email == "" {
		email = strings.TrimSpace(parsed.Data.User.Email)
	}
	return FeishuUserInfo{Email: strings.ToLower(email), Name: parsed.Data.User.Name}, nil
}

func (c *FeishuBotClient) tenantAccessToken(ctx context.Context) (string, error) {
	body, err := json.Marshal(map[string]string{
		"app_id":     c.appID,
		"app_secret": c.appSecret,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var parsed struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode Feishu token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || parsed.Code != 0 || parsed.TenantAccessToken == "" {
		return "", fmt.Errorf("Feishu tenant token failed: status=%d code=%d msg=%s", resp.StatusCode, parsed.Code, parsed.Msg)
	}
	return parsed.TenantAccessToken, nil
}

func (c *FeishuBotClient) doFeishuPost(ctx context.Context, token, endpoint string, body []byte, failurePrefix string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("decode Feishu response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || parsed.Code != 0 {
		return fmt.Errorf("%s: status=%d code=%d msg=%s", failurePrefix, resp.StatusCode, parsed.Code, parsed.Msg)
	}
	return nil
}
