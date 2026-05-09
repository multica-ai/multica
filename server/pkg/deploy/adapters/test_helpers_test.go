package adapters

import (
	"net/http"
	"net/url"
)

// rewriteTransport is a tiny http.RoundTripper that rewrites every
// outbound request's host to a target URL. We use it to point each
// adapter at the local httptest server without modifying the adapter's
// hardcoded provider hostnames — keeps the production adapter code
// straightforward while still letting tests exercise the full HTTP path.
type rewriteRT struct {
	target *url.URL
}

func rewriteTransport(rawURL string) http.RoundTripper {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic("rewriteTransport: bad URL: " + err.Error())
	}
	return &rewriteRT{target: u}
}

func (r *rewriteRT) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = r.target.Scheme
	req.URL.Host = r.target.Host
	return http.DefaultTransport.RoundTrip(req)
}
