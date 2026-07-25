package handler

import (
	"net"
	"net/url"
	"os"
	"strings"
)

const cerebroStagingLoginHost = "cerebro.firtal.com"

// cerebroStaticLoginAllowed keeps the explicitly configured static login code
// available on Cerebro staging and local development without exposing the same
// bypass on the production Multica host.
func cerebroStaticLoginAllowed() bool {
	rawAppURL := strings.TrimSpace(os.Getenv("MULTICA_APP_URL"))
	if rawAppURL == "" {
		return !isProductionEnv()
	}

	appURL, err := url.Parse(rawAppURL)
	if err != nil || appURL.User != nil {
		return false
	}

	host := strings.TrimSuffix(strings.ToLower(appURL.Hostname()), ".")
	if host == cerebroStagingLoginHost {
		return strings.EqualFold(appURL.Scheme, "https")
	}

	if isProductionEnv() {
		return false
	}

	if host == "localhost" {
		return localLoginScheme(appURL.Scheme)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return localLoginScheme(appURL.Scheme)
	}

	return false
}

func localLoginScheme(scheme string) bool {
	return strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")
}
