package sourcechannel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeSettingStore struct {
	value string
}

func (f fakeSettingStore) GetOrCreateSystemSetting(context.Context, db.GetOrCreateSystemSettingParams) (string, error) {
	return f.value, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func recordingClient(t *testing.T, got chan<- Report) *http.Client {
	t.Helper()
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Errorf("method: want POST, got %s", req.Method)
			}
			if req.URL.String() != OfficialMulticaAPIURL+ReportPath {
				t.Errorf("url: want %s, got %s", OfficialMulticaAPIURL+ReportPath, req.URL.String())
			}
			var payload Report
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Errorf("decode payload: %v", err)
			}
			got <- payload
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}),
	}
}

func TestSenderReportsChannelOtherTextAndAnonymousHashes(t *testing.T) {
	got := make(chan Report, 1)
	sender := MustNewSender(fakeSettingStore{value: strings.Repeat("f", 64)}, SenderConfig{
		HTTPClient: recordingClient(t, got),
		Timeout:    time.Second,
	})
	sender.ReportSelfHostSourceChannel("user-123", "Other", "  a podcast  ", "https://Example.com:443/path", true)

	select {
	case payload := <-got:
		if payload.SchemaVersion != SchemaVersion {
			t.Fatalf("schema_version: want %d, got %d", SchemaVersion, payload.SchemaVersion)
		}
		if payload.Channel != "other" {
			t.Fatalf("channel: want other, got %q", payload.Channel)
		}
		if payload.SourceOther != "a podcast" {
			t.Fatalf("source_other: want trimmed text, got %q", payload.SourceOther)
		}
		if payload.Domain == nil || *payload.Domain != "example.com" {
			t.Fatalf("domain: want example.com, got %+v", payload.Domain)
		}
		if payload.DomainMD5 != DomainMD5("example.com") {
			t.Fatalf("domain_md5: want %q, got %q", DomainMD5("example.com"), payload.DomainMD5)
		}
		if !ValidHash(payload.InstanceHash) || !ValidHash(payload.SubjectHash) {
			t.Fatalf("hashes should be 64-char lowercase hex: %+v", payload)
		}
		if payload.SubjectHash == "user-123" || strings.Contains(payload.SubjectHash, "user-123") {
			t.Fatalf("subject_hash leaked raw user id: %q", payload.SubjectHash)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for source channel report")
	}
}

func TestSenderCanReportDomainHashWithoutPlaintextDomain(t *testing.T) {
	got := make(chan Report, 1)
	sender := MustNewSender(fakeSettingStore{value: strings.Repeat("f", 64)}, SenderConfig{
		HTTPClient: recordingClient(t, got),
		Timeout:    time.Second,
	})
	sender.ReportSelfHostSourceChannel("user-123", "search", "", "https://Example.com:443/path", false)

	select {
	case payload := <-got:
		if payload.Domain != nil {
			t.Fatalf("domain: want nil when plaintext consent is false, got %+v", payload.Domain)
		}
		if payload.DomainMD5 != DomainMD5("example.com") {
			t.Fatalf("domain_md5: want %q, got %q", DomainMD5("example.com"), payload.DomainMD5)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for source channel report")
	}
}

func TestSenderDropsUnknownChannel(t *testing.T) {
	got := make(chan struct{}, 1)
	sender := MustNewSender(fakeSettingStore{value: strings.Repeat("f", 64)}, SenderConfig{
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				got <- struct{}{}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("")),
					Request:    req,
				}, nil
			}),
		},
		Timeout: 50 * time.Millisecond,
	})
	sender.ReportSelfHostSourceChannel("user-123", "private_text", "text", "example.com", true)

	select {
	case <-got:
		t.Fatal("unexpected report for invalid channel")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSenderDropsOfficialMulticaAPIURL(t *testing.T) {
	got := make(chan struct{}, 1)
	sender := MustNewSender(fakeSettingStore{value: strings.Repeat("f", 64)}, SenderConfig{
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				got <- struct{}{}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("")),
					Request:    req,
				}, nil
			}),
		},
		Timeout: 50 * time.Millisecond,
	})
	sender.ReportSelfHostSourceChannel("user-123", "search", "", OfficialMulticaAPIURL, true)

	select {
	case <-got:
		t.Fatal("unexpected report for official Multica domain")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDomainHelpers(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "https://Example.com:443/path?q=1", want: "example.com"},
		{raw: "api.customer.example.", want: "api.customer.example"},
		{raw: "backend:8080", want: "backend"},
		{raw: "[::1]:8080", want: "::1"},
		{raw: "", want: ""},
	}
	for _, tt := range tests {
		if got := NormalizeDomain(tt.raw); got != tt.want {
			t.Fatalf("NormalizeDomain(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}

	if !IsOfficialMulticaAPIURL(OfficialMulticaAPIURL) {
		t.Fatalf("%s should be official", OfficialMulticaAPIURL)
	}
	if IsOfficialMulticaAPIURL("http://api.multica.ai") {
		t.Fatal("http://api.multica.ai should not be official")
	}
	if IsOfficialMulticaAPIURL("https://multica.ai") {
		t.Fatal("https://multica.ai should not be the official API")
	}
	if !ShouldReportAPIBaseURL("https://api.customer.example") {
		t.Fatal("customer API URL should report")
	}
	if got := DomainMD5("Example.com"); got != "5ababd603b22780302dd8d83498e5172" {
		t.Fatalf("DomainMD5 = %q", got)
	}
}
