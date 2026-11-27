package vcs

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

type staticResolver map[string][]netip.Addr

func (s staticResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	if addrs, ok := s[strings.ToLower(host)]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func TestValidateInstanceURL(t *testing.T) {
	t.Parallel()

	gitlab := netip.MustParseAddr("1.2.3.4")
	private := netip.MustParseAddr("192.168.10.4")
	metadata := netip.MustParseAddr("169.254.169.254")
	resolver := staticResolver{
		"gitlab.example":           {gitlab},
		"gitea.internal":           {private},
		"evil.example":             {metadata},
		"metadata.google.internal": {gitlab}, // hostname is enough to reject
	}

	tests := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{name: "public https", raw: "https://gitlab.example", wantErr: nil},
		{name: "private self-hosted", raw: "http://gitea.internal:3000", wantErr: nil},
		{name: "metadata ipv4", raw: "http://169.254.169.254/", wantErr: ErrForbiddenInstance},
		{name: "loopback ipv4", raw: "http://127.0.0.1:22", wantErr: ErrForbiddenInstance},
		{name: "loopback ipv6", raw: "http://[::1]/", wantErr: ErrForbiddenInstance},
		{name: "localhost name", raw: "http://localhost:3000", wantErr: ErrForbiddenInstance},
		{name: "metadata hostname", raw: "http://metadata.google.internal/", wantErr: ErrForbiddenInstance},
		{name: "rebinding to metadata", raw: "http://evil.example/", wantErr: ErrForbiddenInstance},
		{name: "userinfo", raw: "https://user:token@gitlab.example", wantErr: ErrForbiddenInstance},
		{name: "alibaba metadata", raw: "http://100.100.100.200/", wantErr: ErrForbiddenInstance},
		{name: "aws ipv6 metadata", raw: "http://[fd00:ec2::254]/", wantErr: ErrForbiddenInstance},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateInstanceURL(context.Background(), tt.raw, resolver)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateInstanceURL(%q) = %v, want nil", tt.raw, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateInstanceURL(%q) = %v, want %v", tt.raw, err, tt.wantErr)
			}
		})
	}
}

func TestInstanceHTTPClientRejectsRedirectToMetadata(t *testing.T) {
	t.Parallel()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	t.Cleanup(redirect.Close)

	client := &http.Client{Timeout: instanceRequestTimeout, CheckRedirect: checkInstanceRedirect}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, redirect.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected redirect to metadata to fail")
	}
	if !errors.Is(err, ErrForbiddenInstance) && !strings.Contains(err.Error(), "permitted provider address") {
		// url.Error wraps the CheckRedirect result
		var urlErr *url.Error
		if !errors.As(err, &urlErr) || !errors.Is(urlErr.Err, ErrForbiddenInstance) {
			t.Fatalf("err = %v, want ErrForbiddenInstance", err)
		}
	}
}

func TestIsForbiddenInstanceAddress(t *testing.T) {
	t.Parallel()

	forbidden := []string{"127.0.0.1", "::1", "169.254.169.254", "0.0.0.0", "224.0.0.1", "100.100.100.200", "fd00:ec2::254"}
	for _, raw := range forbidden {
		addr := netip.MustParseAddr(raw)
		if !isForbiddenInstanceAddress(addr) {
			t.Fatalf("isForbiddenInstanceAddress(%s) = false, want true", raw)
		}
	}
	allowed := []string{"1.1.1.1", "192.168.1.10", "10.0.0.5", "172.16.0.2", "fd12:3456::1"}
	for _, raw := range allowed {
		addr := netip.MustParseAddr(raw)
		if isForbiddenInstanceAddress(addr) {
			t.Fatalf("isForbiddenInstanceAddress(%s) = true, want false", raw)
		}
	}
}
