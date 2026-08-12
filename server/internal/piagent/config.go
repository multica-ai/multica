package piagent

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// APIKeyEnv stays outside the reserved MULTICA_ namespace because agent
// custom_env values in that namespace are stripped before provider launch.
const APIKeyEnv = "PI_MULTICA_API_KEY"

var providerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var supportedAPIs = map[string]struct{}{
	"openai-completions":   {},
	"openai-responses":     {},
	"anthropic-messages":   {},
	"google-generative-ai": {},
}

// Config is the non-secret Pi model connection written to the task-scoped
// models.json. APIKeyEnv supplies the matching secret at process launch.
type Config struct {
	Provider string `json:"provider,omitempty"`
	API      string `json:"api,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	Model    string `json:"model,omitempty"`
}

// Decode validates a serialized Pi model connection. Empty objects are
// reported as configured=false rather than as malformed configuration.
func Decode(raw json.RawMessage) (cfg Config, configured bool, err error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "{}" {
		return Config{}, false, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, false, fmt.Errorf("invalid JSON: %w", err)
	}
	cfg = Normalize(cfg)
	if err := Validate(cfg); err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

// Normalize trims every user-entered field before persistence or execution.
func Normalize(cfg Config) Config {
	cfg.Provider = strings.TrimSpace(cfg.Provider)
	cfg.API = strings.TrimSpace(cfg.API)
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.Model = strings.TrimSpace(cfg.Model)
	return cfg
}

// SameEndpoint reports whether two model configurations use the same
// credential boundary. Models may differ while still sharing one provider
// account and API endpoint.
func SameEndpoint(a, b Config) bool {
	a = Normalize(a)
	b = Normalize(b)
	return a.Provider == b.Provider &&
		a.API == b.API &&
		strings.TrimRight(a.BaseURL, "/") == strings.TrimRight(b.BaseURL, "/")
}

// Validate accepts only complete, safe model connections. Public endpoints
// require HTTPS; loopback HTTP remains available for local model servers.
func Validate(cfg Config) error {
	cfg = Normalize(cfg)
	if cfg.Provider == "" || cfg.API == "" || cfg.BaseURL == "" || cfg.Model == "" {
		return fmt.Errorf("provider, api, base_url, and model are all required")
	}
	if !providerNamePattern.MatchString(cfg.Provider) {
		return fmt.Errorf("provider has an invalid format")
	}
	if _, ok := supportedAPIs[cfg.API]; !ok {
		return fmt.Errorf("unsupported api %q", cfg.API)
	}
	u, err := url.ParseRequestURI(cfg.BaseURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("base_url must be an absolute URL")
	}
	if u.User != nil {
		return fmt.Errorf("base_url must not contain credentials")
	}
	host := u.Hostname()
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(host)) {
		return fmt.Errorf("base_url must use HTTPS (HTTP is allowed only for loopback)")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
}
