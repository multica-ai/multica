package googlechat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

const testWebhookURL = "https://chat.googleapis.com/v1/spaces/AAAA_123/messages?key=test-key&token=test-token"

func TestValidateWebhookURL(t *testing.T) {
	t.Parallel()

	valid := []string{
		testWebhookURL,
		testWebhookURL + "&threadKey=issue-1",
	}
	for _, raw := range valid {
		if _, err := ValidateWebhookURL(raw); err != nil {
			t.Errorf("ValidateWebhookURL(%q): %v", raw, err)
		}
	}

	invalid := []string{
		"",
		"http://chat.googleapis.com/v1/spaces/AAAA/messages?key=k&token=t",
		"https://CHAT.GOOGLEAPIS.COM/v1/spaces/AAAA/messages?key=k&token=t",
		"https://user@chat.googleapis.com/v1/spaces/AAAA/messages?key=k&token=t",
		"https://chat.googleapis.com:443/v1/spaces/AAAA/messages?key=k&token=t",
		"https://chat.googleapis.com/v1/spaces/AAAA/messages/extra?key=k&token=t",
		"https://chat.googleapis.com/v1/spaces/AAAA/messages?key=&token=t",
		"https://chat.googleapis.com/v1/spaces/AAAA/messages?key=k",
		"https://chat.googleapis.com/v1/spaces/AAAA/messages?key=k&token=t#fragment",
		"https://example.com/v1/spaces/AAAA/messages?key=k&token=t",
	}
	for _, raw := range invalid {
		if _, err := ValidateWebhookURL(raw); err == nil {
			t.Errorf("ValidateWebhookURL(%q) succeeded, want error", raw)
		}
	}
}

func TestMaskWebhookURLAndSpaceID(t *testing.T) {
	t.Parallel()

	masked := MaskWebhookURL(testWebhookURL)
	if strings.Contains(masked, "test-key") || strings.Contains(masked, "test-token") {
		t.Fatalf("masked URL exposed credentials: %q", masked)
	}
	if got := SpaceID(testWebhookURL); got != "AAAA_123" {
		t.Fatalf("SpaceID() = %q, want AAAA_123", got)
	}
	if got := MaskWebhookURL("stored-invalid-value"); strings.Contains(got, "stored-invalid-value") {
		t.Fatalf("invalid stored value was not masked: %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestClientSend(t *testing.T) {
	t.Parallel()

	var requestBody string
	client := &Client{httpClient: &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			requestBody = string(body)
			if request.URL.String() != testWebhookURL {
				t.Fatalf("request URL = %q", request.URL.String())
			}
			if got := request.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		}),
	}}

	if err := client.Send(context.Background(), testWebhookURL, "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if requestBody != `{"text":"hello"}` {
		t.Fatalf("request body = %q", requestBody)
	}
}

func TestClientSendTransportErrorOmitsWebhookCredentials(t *testing.T) {
	t.Parallel()

	client := &Client{httpClient: &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: connection refused")
		}),
	}}

	err := client.Send(context.Background(), testWebhookURL, "hello")
	if err == nil {
		t.Fatal("Send succeeded, want transport error")
	}
	message := err.Error()
	if strings.Contains(message, "key=") || strings.Contains(message, "token=") {
		t.Fatalf("transport error exposed webhook credentials: %q", message)
	}
	if !strings.Contains(message, "connection refused") {
		t.Fatalf("transport cause was lost: %q", message)
	}
}

func TestNewClientDisablesRedirectsAndProxy(t *testing.T) {
	t.Parallel()

	client := NewClient()
	if client.httpClient.Timeout != webhookRequestTimeout {
		t.Fatalf("timeout = %s, want %s", client.httpClient.Timeout, webhookRequestTimeout)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err := client.httpClient.CheckRedirect(request, nil); err != http.ErrUseLastResponse {
		t.Fatalf("CheckRedirect error = %v", err)
	}
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatalf("transport proxy is enabled")
	}
}
