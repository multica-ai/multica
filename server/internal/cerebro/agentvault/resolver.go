package agentvault

import "strings"

// Proxy-trust env var names set on the agent child so every HTTP(S) client it
// runs (curl, node fetch, python requests, git) routes through the broker and
// trusts the MITM CA. Mirrors the variables Agent Vault's own `run` wrapper
// sets, but here the proxy points at the Multica backend relay (which holds the
// agent-vault.internal path) rather than at Agent Vault directly — the agent
// machine never needs to reach the internal path itself.
const (
	envHTTPSProxy   = "HTTPS_PROXY"
	envHTTPProxy    = "HTTP_PROXY"
	envSSLCertFile  = "SSL_CERT_FILE"
	envNodeExtraCA  = "NODE_EXTRA_CA_CERTS"
	envRequestsCA   = "REQUESTS_CA_BUNDLE"
	envCurlCABundle = "CURL_CA_BUNDLE"
)

// SpawnEnv is the per-agent environment that routes the agent through the
// broker with its own scoped token. It is injected via the agent's custom_env
// at claim time (the daemon already merges custom_env into the child, and
// redacts it from agent-actor API reads).
type SpawnEnv map[string]string

// BuildSpawnEnv returns the env vars that wire one agent through the broker.
// proxyURL is the Multica backend relay URL (already carrying the per-agent
// session token as proxy auth); caPath is the on-disk path to the broker's
// MITM CA PEM. An empty proxyURL yields a nil map (no brokering for this agent).
func BuildSpawnEnv(proxyURL, caPath string) SpawnEnv {
	if strings.TrimSpace(proxyURL) == "" {
		return nil
	}
	env := SpawnEnv{
		envHTTPSProxy: proxyURL,
		envHTTPProxy:  proxyURL,
	}
	if strings.TrimSpace(caPath) != "" {
		env[envSSLCertFile] = caPath
		env[envNodeExtraCA] = caPath
		env[envRequestsCA] = caPath
		env[envCurlCABundle] = caPath
	}
	return env
}

// AllowedVaults returns the distinct vault names from an access list, in input
// order, skipping blanks. Used to scope the per-agent token to exactly the
// boxes the admin granted.
func AllowedVaults(access []Access) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, a := range access {
		v := strings.TrimSpace(a.Vault)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
