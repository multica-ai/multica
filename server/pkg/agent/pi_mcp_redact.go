package agent

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var piMCPEnvPattern = regexp.MustCompile(`\$\{(\w+)\}|\$env:(\w+)|\{env:(\w+)\}`)

func piMCPSecrets(raw json.RawMessage, env []string) []string {
	var doc struct {
		Servers map[string]struct {
			Env     map[string]string `json:"env"`
			Headers map[string]string `json:"headers"`
			URL     string            `json:"url"`
			Args    []string          `json:"args"`
		} `json:"mcpServers"`
	}
	_ = json.Unmarshal(raw, &doc)
	var secrets []string
	add := func(value string) {
		if value != "" {
			secrets = append(secrets, value)
		}
	}
	expand := func(value string) string {
		return piMCPEnvPattern.ReplaceAllStringFunc(value, func(match string) string {
			groups := piMCPEnvPattern.FindStringSubmatch(match)
			for _, key := range groups[1:] {
				if key != "" {
					value := piEnvValue(env, key)
					add(value)
					return value
				}
			}
			return ""
		})
	}
	for _, server := range doc.Servers {
		for _, values := range []map[string]string{server.Env, server.Headers} {
			for _, value := range values {
				value = expand(value)
				add(value)
				if scheme, token, ok := strings.Cut(value, " "); ok && (strings.EqualFold(scheme, "Bearer") || strings.EqualFold(scheme, "Basic")) {
					add(token)
				}
			}
		}
		for _, arg := range server.Args {
			add(expand(arg))
		}
		endpoint := expand(server.URL)
		add(endpoint)
		if u, err := url.Parse(endpoint); err == nil {
			if u.User != nil {
				add(u.User.Username())
				p, _ := u.User.Password()
				add(p)
			}
			for _, values := range u.Query() {
				for _, value := range values {
					add(value)
				}
			}
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return secrets
}

func (r *piMCPRun) redact(text string) string {
	for _, secret := range r.secrets {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}

func (r *piMCPRun) redactJSON(line string) string {
	var value any
	if json.Unmarshal([]byte(line), &value) != nil {
		return ""
	}
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			return r.redact(x)
		case []any:
			for i, e := range x {
				x[i] = walk(e)
			}
		case map[string]any:
			for k, e := range x {

				x[k] = walk(e)
			}
		}
		return v
	}
	// Protocol discriminators must not be rewritten by a short env value.
	obj, ok := value.(map[string]any)
	if !ok {
		return line
	}
	typ := obj["type"]
	message, _ := obj["message"].(map[string]any)
	var stopReason any
	if typ == "turn_end" && message != nil {
		switch message["stopReason"] {
		case "error", "stop", "length", "toolUse", "aborted":
			stopReason = message["stopReason"]
		}
	}
	assistant, _ := obj["assistantMessageEvent"].(map[string]any)
	var assistantType any
	if assistant != nil {
		assistantType = assistant["type"]
	}
	walk(obj)
	obj["type"] = typ
	if stopReason != nil {
		message["stopReason"] = stopReason
	}
	if assistant != nil {
		assistant["type"] = assistantType
	}
	safe, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return string(safe)
}

// Keep only the suffix that could become a secret in the next stream delta.
// Redacting each JSON event independently is insufficient for streamed text.
func (r *piMCPRun) redactStream(pending *string, delta string, final bool) string {
	text := r.redact(*pending + delta)
	keep := 0
	if !final {
		for _, secret := range r.secrets {
			for n := 1; n < len(secret) && n <= len(text); n++ {
				if n > keep && strings.HasSuffix(text, secret[:n]) {
					keep = n
				}
			}
		}
	}
	*pending = text[len(text)-keep:]
	return text[:len(text)-keep]
}
