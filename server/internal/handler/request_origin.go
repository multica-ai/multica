package handler

import (
	"net/http"
	"net/url"
	"strings"
)

func requestAppOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	if origin := normalizeAppOrigin(r.Header.Get("Origin")); origin != "" {
		return origin
	}
	if origin := normalizeAppOrigin(r.Header.Get("Referer")); origin != "" {
		return origin
	}
	if origin := forwardedAppOrigin(r.Header.Get("Forwarded")); origin != "" {
		return origin
	}
	if host := firstHeaderValue(r.Header.Get("X-Forwarded-Host")); host != "" {
		return originFromHost(firstHeaderValue(r.Header.Get("X-Forwarded-Proto")), host)
	}
	return ""
}

func normalizeAppOrigin(raw string) string {
	raw = strings.TrimSpace(firstHeaderValue(raw))
	if raw == "" || strings.EqualFold(raw, "null") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func forwardedAppOrigin(raw string) string {
	raw = strings.TrimSpace(firstHeaderValue(raw))
	if raw == "" {
		return ""
	}

	proto := ""
	host := ""
	for _, part := range strings.Split(raw, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "proto":
			proto = value
		case "host":
			host = value
		}
	}
	return originFromHost(proto, host)
}

func originFromHost(proto, host string) string {
	host = strings.TrimSpace(firstHeaderValue(host))
	if host == "" || strings.ContainsAny(host, `/\`) || strings.Contains(host, "://") {
		return ""
	}

	proto = strings.ToLower(strings.TrimSpace(firstHeaderValue(proto)))
	if proto != "https" {
		proto = "http"
	}
	return proto + "://" + host
}

func firstHeaderValue(raw string) string {
	if i := strings.Index(raw, ","); i >= 0 {
		return strings.TrimSpace(raw[:i])
	}
	return strings.TrimSpace(raw)
}
