package lark

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// capturingRoundTripper records the host of every outbound request and
// replies with a canned Lark-style JSON body that satisfies every decode
// path the client takes (token mint, bot info, contact union_id). It lets
// a test assert WHICH open-platform host a call targeted without dialing
// the real public Feishu / Lark domains.
type capturingRoundTripper struct {
	hosts []string
}

func (rt *capturingRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.hosts = append(rt.hosts, r.URL.Host)
	const body = `{"code":0,"msg":"ok","tenant_access_token":"t","expire":7200,` +
		`"bot":{"open_id":"ou_x"},"data":{"user":{"union_id":"on_x"}}}`
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// TestRegion_OpenPlatformBaseURL pins the region→host mapping that both
// the REST client and the WS bootstrap depend on.
func TestRegion_OpenPlatformBaseURL(t *testing.T) {
	cases := []struct {
		region Region
		want   string
	}{
		{RegionFeishu, "https://open.feishu.cn"},
		{RegionLark, "https://open.larksuite.com"},
		{Region(""), "https://open.feishu.cn"},
		{Region("bogus"), "https://open.feishu.cn"},
	}
	for _, tc := range cases {
		if got := tc.region.OpenPlatformBaseURL(); got != tc.want {
			t.Errorf("Region(%q).OpenPlatformBaseURL() = %q, want %q", tc.region, got, tc.want)
		}
	}
}

// TestRegionOrDefault pins the normalization used at every credential-
// build site: unknown / empty strings collapse to Feishu so a malformed
// row never yields an empty host or a CHECK-violating write.
func TestRegionOrDefault(t *testing.T) {
	cases := map[string]Region{
		"feishu": RegionFeishu,
		"lark":   RegionLark,
		"":       RegionFeishu,
		"LARK":   RegionFeishu, // case-sensitive on purpose; CHECK stores lowercase
		"intl":   RegionFeishu,
	}
	for in, want := range cases {
		if got := RegionOrDefault(in); got != want {
			t.Errorf("RegionOrDefault(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIsLarkInternationalHost gates the upgrade-repair backfill: only a
// deployment-wide override pointing at open.larksuite.com should relabel
// legacy installs. Mainland, empty, mock/staging, and scheme-less values
// must NOT trigger it.
func TestIsLarkInternationalHost(t *testing.T) {
	cases := map[string]bool{
		"https://open.larksuite.com":  true,
		"https://open.larksuite.com/": true,
		"https://OPEN.LARKSUITE.COM":  true, // host compare is case-insensitive
		"https://open.feishu.cn":      false,
		"":                            false,
		"   ":                         false,
		"https://mock.internal:8080":  false,
		"open.larksuite.com":          false, // no scheme → not a usable override anyway
	}
	for in, want := range cases {
		if got := isLarkInternationalHost(in); got != want {
			t.Errorf("isLarkInternationalHost(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestHTTPClient_ResolvesHostFromRegion is the core dual-region guarantee:
// with NO deployment-wide BaseURL override, the open-platform host is
// chosen per call from InstallationCredentials.Region, so Feishu and Lark
// installations served by one process each reach their own cloud.
func TestHTTPClient_ResolvesHostFromRegion(t *testing.T) {
	cases := []struct {
		name   string
		region Region
		host   string
	}{
		{"feishu", RegionFeishu, "open.feishu.cn"},
		{"lark", RegionLark, "open.larksuite.com"},
		{"empty defaults to feishu", Region(""), "open.feishu.cn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := &capturingRoundTripper{}
			// No BaseURL → region resolution governs the host.
			c := NewHTTPAPIClient(HTTPClientConfig{HTTPClient: &http.Client{Transport: rt}})
			if _, err := c.GetBotInfo(context.Background(), InstallationCredentials{
				AppID: "cli_x", AppSecret: "s", Region: tc.region,
			}); err != nil {
				t.Fatalf("GetBotInfo: %v", err)
			}
			if len(rt.hosts) == 0 {
				t.Fatalf("no requests captured")
			}
			for _, h := range rt.hosts {
				if h != tc.host {
					t.Errorf("request targeted host %q, want %q", h, tc.host)
				}
			}
		})
	}
}

// TestHTTPClient_BaseURLOverridesRegion pins the test / staging seam: an
// explicit cfg.BaseURL forces every region to that host, which is how the
// existing test suite (and MULTICA_LARK_HTTP_BASE_URL) keeps working.
func TestHTTPClient_BaseURLOverridesRegion(t *testing.T) {
	rt := &capturingRoundTripper{}
	c := NewHTTPAPIClient(HTTPClientConfig{
		BaseURL:    "https://override.example.com",
		HTTPClient: &http.Client{Transport: rt},
	})
	if _, err := c.GetBotInfo(context.Background(), InstallationCredentials{
		AppID: "cli_x", AppSecret: "s", Region: RegionLark, // would be larksuite, but override wins
	}); err != nil {
		t.Fatalf("GetBotInfo: %v", err)
	}
	for _, h := range rt.hosts {
		if h != "override.example.com" {
			t.Errorf("override not honored: host=%q, want override.example.com", h)
		}
	}
}

// TestWSEndpoint_ResolvesHostFromRegion pins that the long-conn bootstrap
// POST (/callback/ws/endpoint) also targets the per-installation region
// host when no deployment-wide override is set.
func TestWSEndpoint_ResolvesHostFromRegion(t *testing.T) {
	cases := []struct {
		region Region
		host   string
	}{
		{RegionFeishu, "open.feishu.cn"},
		{RegionLark, "open.larksuite.com"},
		{Region(""), "open.feishu.cn"},
	}
	for _, tc := range cases {
		rt := &wsEndpointRoundTripper{}
		f, err := NewHTTPConnectionTokenFetcher(HTTPConnectionTokenConfig{
			HTTPClient: &http.Client{Transport: rt},
		})
		if err != nil {
			t.Fatalf("NewHTTPConnectionTokenFetcher: %v", err)
		}
		if _, err := f.Endpoint(context.Background(), InstallationCredentials{
			AppID: "cli_x", AppSecret: "s", Region: tc.region,
		}); err != nil {
			t.Fatalf("Endpoint(region=%q): %v", tc.region, err)
		}
		if rt.host != tc.host {
			t.Errorf("ws bootstrap targeted host %q, want %q (region=%q)", rt.host, tc.host, tc.region)
		}
		if rt.path != "/callback/ws/endpoint" {
			t.Errorf("ws bootstrap path = %q, want /callback/ws/endpoint", rt.path)
		}
	}
}

// wsEndpointRoundTripper returns a valid endpointResponse so Endpoint's
// decode succeeds, while recording the host + path it was asked to reach.
type wsEndpointRoundTripper struct {
	host string
	path string
}

func (rt *wsEndpointRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.host = r.URL.Host
	rt.path = r.URL.Path
	const body = `{"code":0,"msg":"ok","data":{"URL":"wss://example/ws?service_id=1&device_id=d",` +
		`"ClientConfig":{"ReconnectCount":1,"ReconnectInterval":120,"ReconnectNonce":30,"PingInterval":120}}}`
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}
