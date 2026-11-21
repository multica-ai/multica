package localenv

import (
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
)

func publicHostFromEnv() string {
	host := strings.TrimSpace(os.Getenv("MULTICA_ENV_PUBLIC_HOST"))
	if host == "" {
		return "localhost"
	}
	address := host
	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") && strings.Contains(address, ":") {
		address = address[1 : len(address)-1]
	}
	if net.ParseIP(address) != nil {
		return address
	}
	name := strings.ToLower(strings.TrimSuffix(host, "."))
	// Dotted numbers that failed IP parsing are invalid IP literals, not DNS names.
	if strings.ContainsAny(name, "abcdefghijklmnopqrstuvwxyz") && util.ValidDNSHostname(name) {
		return name
	}
	slog.Warn("invalid MULTICA_ENV_PUBLIC_HOST, using localhost")
	return "localhost"
}

func expandEnvURL(raw string, port int, host string) string {
	urlHost := host
	if strings.Contains(host, ":") {
		urlHost = "[" + host + "]"
	}
	raw = strings.NewReplacer("${MULTICA_HOST}", urlHost, "$MULTICA_HOST", urlHost).Replace(expandPort(raw, port))
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() != "localhost" {
		return raw
	}
	urlPort := u.Port()
	u.Host = urlHost
	if urlPort != "" {
		u.Host = net.JoinHostPort(host, urlPort)
	}
	return u.String()
}

func expandReadyURL(def Definition, port int, host string) string {
	if host != "localhost" && !def.ReadyExplicit && def.URL != "" {
		return expandEnvURL(def.Ready, port, "127.0.0.1")
	}
	return expandEnvURL(def.Ready, port, host)
}

func detectedServerURL(port int) string {
	return "http://" + net.JoinHostPort(publicHostFromEnv(), strconv.Itoa(port))
}
