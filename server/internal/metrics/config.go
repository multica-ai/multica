package metrics

import (
	"net"
	"os"
	"strings"
)

type Config struct {
	Addr string
}

func ConfigFromEnv() Config {
	return Config{Addr: strings.TrimSpace(os.Getenv("METRICS_ADDR"))}
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.Addr) != ""
}

func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
