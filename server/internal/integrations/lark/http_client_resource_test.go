package lark

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMessageResourceErrorDoesNotExposeCause(t *testing.T) {
	t.Parallel()
	err := &messageResourceError{
		retryable: true,
		category:  "request failed",
		cause:     errors.New("GET https://example.test/resources/sensitive-resource-key: secret response"),
	}
	if got := err.Error(); got != "lark message resource: request failed" {
		t.Fatalf("Error() = %q", got)
	}
	if !errors.Is(err, err.cause) {
		t.Fatal("underlying cause must remain available for internal classification")
	}
}

func TestHTTPAPIClientDownloadMessageResource(t *testing.T) {
	t.Parallel()
	fake := newLarkFake(t)
	fake.stubToken("tenant-token", 7200)
	fake.mux.HandleFunc("/open-apis/im/v1/messages/om_123/resources/file_456", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.URL.Query().Get("type"); got != "file" {
			t.Fatalf("type = %q, want file", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"`)
		w.Header().Set("Content-Length", "7")
		_, _ = io.WriteString(w, "payload")
	})

	api := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()})
	downloader, ok := api.(MessageResourceDownloader)
	if !ok {
		t.Fatal("HTTP API client does not implement MessageResourceDownloader")
	}
	resource, err := downloader.DownloadMessageResource(context.Background(), InstallationCredentials{
		AppID: "cli_app", AppSecret: "secret",
	}, "om_123", "file_456", MessageResourceFile)
	if err != nil {
		t.Fatalf("DownloadMessageResource: %v", err)
	}
	defer resource.Body.Close()
	got, err := io.ReadAll(resource.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" || resource.ContentType != "application/pdf" || resource.Filename != "report.pdf" || resource.ContentLength != 7 {
		t.Fatalf("resource = %#v body=%q", resource, got)
	}
}

func TestHTTPAPIClientDownloadMessageResourceRejectsInvalidTypeAndOversize(t *testing.T) {
	t.Parallel()
	api := NewHTTPAPIClient(HTTPClientConfig{})
	downloader := api.(MessageResourceDownloader)
	_, err := downloader.DownloadMessageResource(context.Background(), InstallationCredentials{}, "om", "key", MessageResourceType("video"))
	if err == nil || !strings.Contains(err.Error(), "invalid resource type") {
		t.Fatalf("invalid type error = %v", err)
	}

	fake := newLarkFake(t)
	fake.stubToken("tenant-token", 7200)
	fake.mux.HandleFunc("/open-apis/im/v1/messages/om/resources/key", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "104857601")
		w.WriteHeader(http.StatusOK)
	})
	api = NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()})
	_, err = api.(MessageResourceDownloader).DownloadMessageResource(context.Background(), InstallationCredentials{
		AppID: "cli_app", AppSecret: "secret",
	}, "om", "key", MessageResourceFile)
	if err == nil || !strings.Contains(err.Error(), "resource exceeds 100 MB") {
		t.Fatalf("oversize error = %v", err)
	}
}

func TestHTTPAPIClientDownloadMessageResourceClassifiesFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status    int
		retryable bool
	}{
		{status: http.StatusForbidden, retryable: false},
		{status: http.StatusTooManyRequests, retryable: true},
		{status: http.StatusBadGateway, retryable: true},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			fake := newLarkFake(t)
			fake.stubToken("tenant-token", 7200)
			fake.mux.HandleFunc("/open-apis/im/v1/messages/om/resources/key", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"code":1,"msg":"upstream detail must not leak"}`)
			})
			api := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()})
			_, err := api.(MessageResourceDownloader).DownloadMessageResource(context.Background(), InstallationCredentials{
				AppID: "cli_app", AppSecret: "secret",
			}, "om", "key", MessageResourceFile)
			if err == nil {
				t.Fatal("expected error")
			}
			if got := IsRetryableResourceError(err); got != tc.retryable {
				t.Fatalf("IsRetryableResourceError = %v, want %v: %v", got, tc.retryable, err)
			}
			if strings.Contains(err.Error(), "upstream detail") || strings.Contains(err.Error(), "key") {
				t.Fatalf("error leaked upstream detail or resource key: %v", err)
			}
		})
	}
}

func TestHTTPAPIClientDownloadMessageResourceInvalidatesExpiredToken(t *testing.T) {
	t.Parallel()
	fake := newLarkFake(t)
	fake.stubToken("tenant-token", 7200)
	var calls atomic.Int32
	fake.mux.HandleFunc("/open-apis/im/v1/messages/om/resources/key", func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})
	api := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()}).(MessageResourceDownloader)
	creds := InstallationCredentials{AppID: "cli_app", AppSecret: "secret"}
	if _, err := api.DownloadMessageResource(context.Background(), creds, "om", "key", MessageResourceFile); err == nil || !IsRetryableResourceError(err) {
		t.Fatalf("first call error = %v", err)
	}
	resource, err := api.DownloadMessageResource(context.Background(), creds, "om", "key", MessageResourceFile)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	resource.Body.Close()
	if fake.tokenN.Load() != 2 {
		t.Fatalf("token endpoint calls = %d, want 2", fake.tokenN.Load())
	}
}
