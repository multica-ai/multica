package localmode

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	EnabledEnv                    = "LIFEOS_LOCAL_MODE"
	BindHostEnv                   = "LIFEOS_BIND_HOST"
	ContainerLoopbackPublishedEnv = "LIFEOS_CONTAINER_LOOPBACK_PUBLISH"
)

// Enabled reports whether the single-user LifeOS mode is active.
func Enabled() bool {
	return envBool(EnabledEnv)
}

// ListenAddress resolves the HTTP listen address while keeping local mode
// fail-closed. Direct local runs bind to loopback by default. Containerized
// runs may bind inside the container only when the compose file explicitly
// attests that the published host port is loopback-only.
func ListenAddress(port string) (string, error) {
	return ResolveListenAddress(
		port,
		strings.TrimSpace(os.Getenv(BindHostEnv)),
		Enabled(),
		envBool(ContainerLoopbackPublishedEnv),
	)
}

func ResolveListenAddress(port, configuredHost string, enabled, containerLoopbackPublished bool) (string, error) {
	host := strings.TrimSpace(configuredHost)
	if !enabled {
		if host == "" {
			return ":" + port, nil
		}
		return net.JoinHostPort(host, port), nil
	}

	if host == "" {
		host = "127.0.0.1"
	}
	if isLoopbackHost(host) {
		return net.JoinHostPort(host, port), nil
	}
	if containerLoopbackPublished && isWildcardHost(host) {
		return net.JoinHostPort(host, port), nil
	}

	return "", fmt.Errorf(
		"%s requires a loopback bind; got %q (container wildcard binds require %s=true)",
		EnabledEnv,
		host,
		ContainerLoopbackPublishedEnv,
	)
}

func envBool(name string) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false
	}
	value, err := strconv.ParseBool(raw)
	return err == nil && value
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isWildcardHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	return host == "0.0.0.0" || host == "::"
}
