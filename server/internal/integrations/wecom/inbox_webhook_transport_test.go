package wecom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const inboxWebhookTestKey = "synthetic-webhook-key"
const inboxWebhookTestURL = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + inboxWebhookTestKey

type inboxWebhookRoundTripFunc func(*http.Request) (*http.Response, error)

func (f inboxWebhookRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type inboxWebhookCountingBody struct {
	io.Reader
	read   int
	closed bool
}

type inboxWebhookFailingReader struct{}

func (inboxWebhookFailingReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("response read failed for %s", inboxWebhookTestURL)
}

func (b *inboxWebhookCountingBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}

func (b *inboxWebhookCountingBody) Close() error {
	b.closed = true
	return nil
}

func TestInboxWebhookURL(t *testing.T) {
	const base = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send"
	for _, tc := range []struct {
		name string
		raw  string
		key  string
		ok   bool
	}{
		{"simple key", inboxWebhookTestURL, inboxWebhookTestKey, true},
		{"opaque encoded key", base + "?key=" + url.QueryEscape("a&b=c/+?"), "a&b=c/+?", true},
		{"escaped unicode key", base + "?key=%E4%B8%AD%E6%96%87", "中文", true},
		{"empty", "", "", false},
		{"http", "http://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + inboxWebhookTestKey, "", false},
		{"wrong host", "https://example.com/cgi-bin/webhook/send?key=" + inboxWebhookTestKey, "", false},
		{"host suffix", "https://qyapi.weixin.qq.com.example.com/cgi-bin/webhook/send?key=" + inboxWebhookTestKey, "", false},
		{"userinfo", "https://user@qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + inboxWebhookTestKey, "", false},
		{"userinfo host confusion", "https://qyapi.weixin.qq.com@example.com/cgi-bin/webhook/send?key=" + inboxWebhookTestKey, "", false},
		{"port", "https://qyapi.weixin.qq.com:443/cgi-bin/webhook/send?key=" + inboxWebhookTestKey, "", false},
		{"empty port", "https://qyapi.weixin.qq.com:/cgi-bin/webhook/send?key=" + inboxWebhookTestKey, "", false},
		{"wrong path", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send/other?key=" + inboxWebhookTestKey, "", false},
		{"escaped path separator", "https://qyapi.weixin.qq.com/cgi-bin%2Fwebhook/send?key=" + inboxWebhookTestKey, "", false},
		{"fragment", inboxWebhookTestURL + "#fragment", "", false},
		{"empty fragment", inboxWebhookTestURL + "#", "", false},
		{"missing key", base, "", false},
		{"empty key", base + "?key=", "", false},
		{"other query", inboxWebhookTestURL + "&other=value", "", false},
		{"trailing separator", inboxWebhookTestURL + "&", "", false},
		{"duplicate key", inboxWebhookTestURL + "&key=another", "", false},
		{"encoded duplicate key", inboxWebhookTestURL + "&%6bey=another", "", false},
		{"invalid query encoding", base + "?key=%xx", "", false},
		{"semicolon query", inboxWebhookTestURL + ";other=value", "", false},
		{"raw control", inboxWebhookTestURL + "\n", "", false},
		{"escaped control", inboxWebhookTestURL + "%0A", "", false},
		{"escaped unicode control", inboxWebhookTestURL + "%C2%80", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateInboxWebhookURL(tc.raw)
			if !tc.ok {
				if err == nil {
					t.Fatal("invalid URL accepted")
				}
				if strings.Contains(err.Error(), inboxWebhookTestKey) || (tc.raw != "" && strings.Contains(err.Error(), tc.raw)) {
					t.Fatal("validation error disclosed the URL or key")
				}
				return
			}
			if err != nil {
				t.Fatalf("valid URL rejected: %v", err)
			}
			parsed, err := url.Parse(got)
			if err != nil || parsed.Query().Get("key") != tc.key {
				t.Fatal("validated URL did not preserve the opaque key")
			}
		})
	}
}

func TestInboxWebhookSendResponses(t *testing.T) {
	const maxBody = 8 * 1024
	success := `{"errcode":0}`
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"success", http.StatusOK, success, nil},
		{"success at body limit", http.StatusOK, success + strings.Repeat(" ", maxBody-len(success)), nil},
		{"HTTP rejection", http.StatusBadRequest, `{"errmsg":"` + inboxWebhookTestKey + `"}`, errWebhookHTTPRejected},
		{"HTTP success must be 200", http.StatusCreated, success, errWebhookHTTPRejected},
		{"HTTP rate limit", http.StatusTooManyRequests, `{"errmsg":"` + inboxWebhookTestKey + `"}`, errWebhookRateLimited},
		{"platform rate limit", http.StatusOK, `{"errcode":45009,"errmsg":"` + inboxWebhookTestKey + `"}`, errWebhookRateLimited},
		{"platform rejection", http.StatusOK, `{"errcode":40001,"errmsg":"` + inboxWebhookTestKey + `"}`, errWebhookRejected},
		{"missing code", http.StatusOK, `{"errmsg":"` + inboxWebhookTestKey + `"}`, errWebhookInvalidResponse},
		{"wrong case code", http.StatusOK, `{"ErrCode":0}`, errWebhookInvalidResponse},
		{"null code", http.StatusOK, `{"errcode":null}`, errWebhookInvalidResponse},
		{"string code", http.StatusOK, `{"errcode":"0"}`, errWebhookInvalidResponse},
		{"fractional code", http.StatusOK, `{"errcode":0.0}`, errWebhookInvalidResponse},
		{"boolean code", http.StatusOK, `{"errcode":false}`, errWebhookInvalidResponse},
		{"null response", http.StatusOK, `null`, errWebhookInvalidResponse},
		{"array response", http.StatusOK, `[]`, errWebhookInvalidResponse},
		{"malformed JSON", http.StatusOK, `{"errcode":0`, errWebhookInvalidResponse},
		{"trailing JSON", http.StatusOK, `{"errcode":0}{"errcode":0}`, errWebhookInvalidResponse},
		{"oversize", http.StatusOK, success + strings.Repeat(" ", maxBody-len(success)+100), errWebhookInvalidResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &inboxWebhookCountingBody{Reader: strings.NewReader(tc.body)}
			calls := 0
			client := &http.Client{Transport: inboxWebhookRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/json" || req.URL.String() != inboxWebhookTestURL {
					t.Errorf("request is not a JSON POST to the validated endpoint")
				}
				payload, err := io.ReadAll(req.Body)
				if err != nil || string(payload) != `{"msgtype":"text"}` {
					t.Errorf("request payload was not preserved")
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: body, Request: req}, nil
			})}

			err := sendInboxWebhook(context.Background(), client, inboxWebhookTestURL, []byte(`{"msgtype":"text"}`))
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), inboxWebhookTestKey) {
				t.Error("error disclosed the URL key or response body")
			}
			if calls != 1 {
				t.Errorf("sent %d requests, want exactly one", calls)
			}
			if body.read > maxBody+1 || !body.closed {
				t.Errorf("response body: read %d bytes, closed %v", body.read, body.closed)
			}
		})
	}
}

func TestInboxWebhookReadFailureDoesNotLeakOrRetry(t *testing.T) {
	calls := 0
	body := &inboxWebhookCountingBody{Reader: inboxWebhookFailingReader{}}
	client := &http.Client{Transport: inboxWebhookRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Body: body, Request: req}, nil
	})}
	err := sendInboxWebhook(context.Background(), client, inboxWebhookTestURL, []byte(`{}`))
	if !errors.Is(err, errWebhookInvalidResponse) || errors.Is(err, errWebhookRateLimited) {
		t.Fatalf("response read failure classified as %v", err)
	}
	if strings.Contains(err.Error(), inboxWebhookTestKey) || calls != 1 || !body.closed {
		t.Fatalf("response read failure leaked the key, retried, or left the body open")
	}
}

func TestInboxWebhookTimeoutDoesNotLeakOrRetry(t *testing.T) {
	calls := 0
	client := &http.Client{Timeout: 20 * time.Millisecond, Transport: inboxWebhookRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		<-req.Context().Done()
		return nil, fmt.Errorf("%s: %w", req.URL, req.Context().Err())
	})}
	err := sendInboxWebhook(context.Background(), client, inboxWebhookTestURL, []byte(`{}`))
	if !errors.Is(err, errWebhookTransport) || errors.Is(err, errWebhookRateLimited) {
		t.Fatalf("timeout classified as %v", err)
	}
	if strings.Contains(err.Error(), inboxWebhookTestKey) || strings.Contains(err.Error(), inboxWebhookTestURL) {
		t.Fatal("timeout error disclosed the webhook URL or key")
	}
	if calls != 1 {
		t.Fatalf("timeout caused %d requests, want one", calls)
	}
}

func TestInboxWebhookRedirectIsNotFollowed(t *testing.T) {
	calls, redirects := 0, 0
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { redirects++; return nil },
		Transport: inboxWebhookRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Header:     http.Header{"Location": []string{"https://attacker.example/?key=" + inboxWebhookTestKey}},
				Body:       io.NopCloser(strings.NewReader(inboxWebhookTestKey)),
				Request:    req,
			}, nil
		}),
	}
	err := sendInboxWebhook(context.Background(), client, inboxWebhookTestURL, []byte(`{"secret":"payload"}`))
	if !errors.Is(err, errWebhookHTTPRejected) {
		t.Fatalf("redirect classified as %v", err)
	}
	if strings.Contains(err.Error(), inboxWebhookTestKey) || strings.Contains(err.Error(), "attacker.example") {
		t.Error("redirect error disclosed the URL or response")
	}
	if calls != 1 || redirects != 0 {
		t.Fatalf("redirect followed: %d requests, %d calls to injected policy", calls, redirects)
	}
	if err := client.CheckRedirect(nil, nil); err != nil || redirects != 1 {
		t.Fatal("the injected client's redirect policy was modified")
	}
}

func TestInboxWebhookRejectsInvalidEndpointBeforeSend(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: inboxWebhookRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected request")
	})}
	err := sendInboxWebhook(context.Background(), client, "https://attacker.example/?key="+inboxWebhookTestKey, nil)
	if !errors.Is(err, errWebhookInvalidURL) || calls != 0 {
		t.Fatalf("invalid endpoint: error %v, requests %d", err, calls)
	}
}
