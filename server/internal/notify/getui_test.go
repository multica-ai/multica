package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGetuiPushSingleByCID_AuthenticatesAndSends(t *testing.T) {
	var authCalls int
	var pushCalls int

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/app-success/auth":
			authCalls++
			if r.Method != http.MethodPost {
				t.Fatalf("auth method = %s, want POST", r.Method)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode auth body: %v", err)
			}
			if body["appkey"] != "key-success" || body["timestamp"] == "" || body["sign"] == "" {
				t.Fatalf("unexpected auth body: %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"expire_time":"` + futureMillis() + `","token":"token-success"}}`))
		case "/v2/app-success/push/single/cid":
			pushCalls++
			if r.Header.Get("token") != "token-success" {
				t.Fatalf("token header = %q, want token-success", r.Header.Get("token"))
			}
			var body struct {
				RequestID string `json:"request_id"`
				Settings  struct {
					TTL      int64          `json:"ttl"`
					Strategy map[string]int `json:"strategy"`
				} `json:"settings"`
				Audience struct {
					CID []string `json:"cid"`
				} `json:"audience"`
				PushMessage struct {
					Notification struct {
						Title     string `json:"title"`
						Body      string `json:"body"`
						ClickType string `json:"click_type"`
						Payload   string `json:"payload"`
					} `json:"notification"`
				} `json:"push_message"`
				PushChannel struct {
					Android struct {
						UPS struct {
							Notification struct {
								Title     string `json:"title"`
								Body      string `json:"body"`
								ClickType string `json:"click_type"`
							} `json:"notification"`
						} `json:"ups"`
					} `json:"android"`
				} `json:"push_channel"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode push body: %v", err)
			}
			if len(body.Audience.CID) != 1 || body.Audience.CID[0] != "cid-success" {
				t.Fatalf("unexpected cid audience: %#v", body.Audience.CID)
			}
			if body.PushMessage.Notification.Title != "OPE-1" || body.PushMessage.Notification.Body != "hello" || body.PushMessage.Notification.ClickType != "payload" || body.PushMessage.Notification.Payload != "wujieai-multicam://issues/issue-1?commentId=comment-1" {
				t.Fatalf("unexpected notification: %#v", body.PushMessage.Notification)
			}
			if body.PushChannel.Android.UPS.Notification.Title != "OPE-1" || body.PushChannel.Android.UPS.Notification.Body != "hello" || body.PushChannel.Android.UPS.Notification.ClickType != "startapp" {
				t.Fatalf("unexpected offline notification: %#v", body.PushChannel.Android.UPS.Notification)
			}
			if body.Settings.Strategy["default"] != 1 || body.Settings.Strategy["hw"] != 1 || body.Settings.Strategy["ho"] != 1 {
				t.Fatalf("unexpected strategy: %#v", body.Settings.Strategy)
			}
			if len(body.RequestID) != 32 {
				t.Fatalf("request_id length = %d, want 32", len(body.RequestID))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"task-1":{"cid-success":"successed_online"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(apiServer.Close)

	cfg := GetuiConfig{
		AppID:        "app-success",
		AppKey:       "key-success",
		MasterSecret: "secret-success",
		BaseURL:      apiServer.URL + "/v2",
		HTTPClient:   apiServer.Client(),
	}

	result, err := cfg.PushSingleByCID(context.Background(), GetuiPushMessage{
		CID:       "cid-success",
		RequestID: "123456789012345678901234567890123456",
		Title:     "OPE-1",
		Body:      "hello",
		ClickURL:  "wujieai-multicam://issues/issue-1?commentId=comment-1",
	})
	if err != nil {
		t.Fatalf("PushSingleByCID: %v", err)
	}
	if result.TaskID != "task-1" || result.Status != "successed_online" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if authCalls != 1 || pushCalls != 1 {
		t.Fatalf("expected one auth and one push, got auth=%d push=%d", authCalls, pushCalls)
	}
}

func TestGetuiPushSingleByCID_RefreshesExpiredToken(t *testing.T) {
	var authCalls int
	var pushTokens []string

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/app-refresh/auth":
			authCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"expire_time":"` + futureMillis() + `","token":"token-` + strconv.Itoa(authCalls) + `"}}`))
		case "/v2/app-refresh/push/single/cid":
			pushTokens = append(pushTokens, r.Header.Get("token"))
			w.Header().Set("Content-Type", "application/json")
			if len(pushTokens) == 1 {
				_, _ = w.Write([]byte(`{"code":10001,"msg":"token invalid","data":{}}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"task-refresh":{"cid-refresh":"successed_online"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(apiServer.Close)

	cfg := GetuiConfig{
		AppID:        "app-refresh",
		AppKey:       "key-refresh",
		MasterSecret: "secret-refresh",
		BaseURL:      apiServer.URL + "/v2",
		HTTPClient:   apiServer.Client(),
	}

	if _, err := cfg.PushSingleByCID(context.Background(), GetuiPushMessage{
		CID:   "cid-refresh",
		Title: "refresh",
		Body:  "token",
	}); err != nil {
		t.Fatalf("PushSingleByCID: %v", err)
	}
	if authCalls != 2 {
		t.Fatalf("expected token refresh to call auth twice, got %d", authCalls)
	}
	if strings.Join(pushTokens, ",") != "token-1,token-2" {
		t.Fatalf("unexpected push tokens: %#v", pushTokens)
	}
}

func futureMillis() string {
	return strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
}
