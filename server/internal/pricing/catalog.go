// Package pricing owns public API reference rates and deterministic model resolution.
package pricing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// Row uses USD per million tokens, including for subscription API equivalents.
type Row struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Provider   string  `json:"provider,omitempty"`
	Model      string  `json:"model,omitempty"`
	Source     string  `json:"source,omitempty"`
	SourceURL  string  `json:"source_url,omitempty"`
}

func (r *Row) UnmarshalJSON(data []byte) error {
	type plain Row
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range []string{"input", "output", "cacheRead", "cacheWrite"} {
		v, ok := fields[key]
		if !ok || string(v) == "null" {
			return fmt.Errorf("missing price: %s", key)
		}
	}
	*r = Row(value)
	return r.Validate()
}

func (r Row) Validate() error {
	for _, value := range []float64{r.Input, r.Output, r.CacheRead, r.CacheWrite} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1e9 {
			return fmt.Errorf("invalid token price")
		}
	}
	return nil
}

type Catalog struct {
	Version string            `json:"version"`
	Rows    map[string]Row    `json:"rows"`
	Aliases map[string]string `json:"aliases"`
}

func Bundled() Catalog {
	var c Catalog
	if err := json.Unmarshal([]byte(bundledJSON), &c); err != nil {
		panic(err)
	}
	if c.Aliases == nil {
		c.Aliases = map[string]string{}
	}
	for key, row := range c.Rows {
		row.Provider = providerForModel(key)
		row.Model = key
		row.Source = "bundled"
		c.Rows[key] = row
	}
	return c
}

var routePrefix = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var contextTag = regexp.MustCompile(`\[[^\]]+\]$`)
var dateSuffix = regexp.MustCompile(`-(20\d{2}-\d{2}-\d{2}|20\d{6}|latest)$`)
var claudeVersion = regexp.MustCompile(`(\d)\.(\d)`)

// Candidates retains each routing layer so an explicit serving-provider price
// wins before considering the provider-independent API reference rate.
func Candidates(model, provider string) []string {
	var base []string
	seen := map[string]bool{}
	push := func(s string) {
		if s == "" {
			return
		}
		if !seen[s] {
			seen[s] = true
			base = append(base, s)
		}
		// Keep an exact transport spelling first, then its catalog spelling.
		if sep := strings.IndexByte(s, ':'); sep > 0 && routePrefix.MatchString(s[:sep]) {
			qualified := s[:sep] + "/" + s[sep+1:]
			if !seen[qualified] {
				seen[qualified] = true
				base = append(base, qualified)
			}
		}
	}
	raw := strings.ToLower(strings.TrimSpace(model))
	for raw != "" {
		push(raw)
		plain := contextTag.ReplaceAllString(raw, "")
		push(plain)
		if strings.HasPrefix(plain, "claude-") {
			plain = claudeVersion.ReplaceAllString(plain, "$1-$2")
			push(plain)
		}
		push(dateSuffix.ReplaceAllString(plain, ""))
		sep := strings.IndexAny(raw, "/:")
		if sep <= 0 || !routePrefix.MatchString(raw[:sep]) {
			break
		}
		raw = raw[sep+1:]
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return base
	}
	out := make([]string, 0, len(base)*2)
	for _, key := range base {
		if strings.HasPrefix(key, provider+"/") || strings.HasPrefix(key, provider+":") {
			out = append(out, key)
		} else {
			out = append(out, provider+"/"+key)
		}
	}
	return append(out, base...)
}

func aliasTarget(c Catalog, key string) string {
	seen := map[string]bool{}
	for c.Aliases[key] != "" && !seen[key] {
		seen[key] = true
		key = c.Aliases[key]
	}
	return key
}

func Resolve(c Catalog, overrides map[string]Row, model, provider string) (Row, bool) {
	keys := Candidates(model, provider)
	for _, key := range keys {
		if row, ok := overrides[key]; ok {
			return row, true
		}
	}
	for _, key := range keys {
		target := aliasTarget(c, key)
		if row, ok := overrides[target]; ok {
			return row, true
		}
		bare := target[strings.LastIndex(target, "/")+1:]
		if c.Aliases[bare] == target {
			if row, ok := overrides[bare]; ok {
				return row, true
			}
		}
	}
	for _, key := range keys {
		if row, ok := c.Rows[aliasTarget(c, key)]; ok {
			return row, true
		}
		if row, ok := c.Rows[key]; ok {
			return row, true
		}
	}
	return Row{}, false
}

func subscriptionProvider(provider string) bool {
	// These directory IDs explicitly identify subscription plans. Their zero
	// rates describe included usage, not free public API inference.
	provider = strings.ReplaceAll(provider, "_", "-")
	if strings.Contains(provider, "-coding-plan") || strings.Contains(provider, "-token-plan") || strings.HasSuffix(provider, "-step-plan") {
		return true
	}
	switch provider {
	case "kimi-for-coding", "github-copilot", "github-copilot-enterprise", "openai-codex", "opencode-go":
		return true
	default:
		return false
	}
}

func canonicalProvider(provider string) string {
	switch provider {
	case "moonshot":
		return "moonshotai"
	case "gemini":
		return "google"
	case "dashscope", "qwen":
		return "alibaba"
	case "zai", "z-ai", "zhipu":
		return "zai"
	default:
		return provider
	}
}

func providerForModel(model string) string {
	for prefix, provider := range map[string]string{"claude-": "anthropic", "gpt-": "openai", "o1": "openai", "o3": "openai", "o4": "openai", "kimi-": "moonshotai", "deepseek-": "deepseek", "grok-": "xai", "qwen": "alibaba", "gemini-": "google", "minimax-": "minimax", "glm-": "zai", "cursor": "cursor"} {
		if strings.HasPrefix(model, prefix) {
			return provider
		}
	}
	return ""
}

func normalizedID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if strings.HasPrefix(id, "claude-") {
		id = claudeVersion.ReplaceAllString(id, "$1-$2")
	}
	return id
}

type liteRow struct {
	Provider   string   `json:"litellm_provider"`
	Mode       string   `json:"mode"`
	Input      *float64 `json:"input_cost_per_token"`
	Output     *float64 `json:"output_cost_per_token"`
	CacheRead  *float64 `json:"cache_read_input_token_cost"`
	CacheWrite *float64 `json:"cache_creation_input_token_cost"`
	Source     string   `json:"source"`
}

type modelsRow struct {
	Family string `json:"family"`
	Cost   struct {
		Input  *float64 `json:"input"`
		Output *float64 `json:"output"`
		Read   *float64 `json:"cache_read"`
		Write  *float64 `json:"cache_write"`
	} `json:"cost"`
}
type modelsProvider struct {
	Models map[string]modelsRow `json:"models"`
}

func optionalRate(value *float64, fallback, multiplier float64) float64 {
	if value == nil {
		return fallback
	}
	return *value * multiplier
}

// BuildCatalog keeps the bundled rates as an offline floor. First-party rows
// alone may define a bare model's default; reseller prices stay qualified.
func BuildCatalog(liteJSON, modelsJSON []byte) (Catalog, error) {
	var lite map[string]json.RawMessage
	var models map[string]modelsProvider
	if err := json.Unmarshal(liteJSON, &lite); err != nil {
		return Catalog{}, err
	}
	if err := json.Unmarshal(modelsJSON, &models); err != nil {
		return Catalog{}, err
	}
	c := Bundled()
	count := 0
	modelsCount := 0
	add := func(provider, id string, row Row, overwrite bool) error {
		provider, id = canonicalProvider(provider), normalizedID(id)
		if provider == "" || id == "" {
			return nil
		}
		if err := row.Validate(); err != nil {
			return err
		}
		key := provider + "/" + id
		row.Provider, row.Model = provider, id
		if _, exists := c.Rows[key]; !exists || overwrite {
			c.Rows[key] = row
		}
		if providerForModel(id) == provider {
			c.Aliases[id] = key
		}
		count++
		return nil
	}
	// Sorted iteration prevents equivalent provider spellings from making the
	// snapshot version depend on Go map iteration order.
	for _, provider := range sortedKeys(models) {
		for _, id := range sortedKeys(models[provider].Models) {
			m := models[provider].Models[id]
			if m.Cost.Input == nil || m.Cost.Output == nil {
				continue
			}
			// Subscription catalogs often encode included usage as zero. Resolve
			// their identity below instead of treating the API equivalent as free.
			modelsCount++
			if subscriptionProvider(provider) && *m.Cost.Input == 0 && *m.Cost.Output == 0 {
				continue
			}
			row := Row{Input: *m.Cost.Input, Output: *m.Cost.Output, Source: "models.dev", SourceURL: "https://models.dev"}
			row.CacheRead = optionalRate(m.Cost.Read, row.Input, 1)
			row.CacheWrite = optionalRate(m.Cost.Write, row.Input, 1)
			if err := add(provider, id, row, false); err != nil {
				return Catalog{}, fmt.Errorf("models.dev %s/%s: %w", provider, id, err)
			}
		}
	}
	liteCount := 0
	for _, key := range sortedKeys(lite) {
		var m liteRow
		if err := json.Unmarshal(lite[key], &m); err != nil {
			continue
		}
		if m.Mode != "chat" || m.Input == nil || m.Output == nil {
			continue
		}
		if subscriptionProvider(m.Provider) && *m.Input == 0 && *m.Output == 0 {
			continue
		}
		id := strings.TrimPrefix(key, m.Provider+"/")
		row := Row{Input: *m.Input * 1e6, Output: *m.Output * 1e6, Source: "litellm", SourceURL: m.Source}
		row.CacheRead = optionalRate(m.CacheRead, row.Input, 1e6)
		row.CacheWrite = optionalRate(m.CacheWrite, row.Input, 1e6)
		if err := row.Validate(); err != nil {
			return Catalog{}, fmt.Errorf("litellm %s: %w", key, err)
		}
		if err := add(m.Provider, id, row, true); err != nil {
			return Catalog{}, err
		}
		canonical := canonicalProvider(m.Provider) + "/" + normalizedID(id)
		if _, ok := c.Rows[canonical]; ok {
			if strings.Contains(key, "/") || providerForModel(normalizedID(id)) == canonicalProvider(m.Provider) {
				c.Aliases[normalizedID(key)] = canonical
			}
			liteCount++
		}
	}
	if liteCount == 0 || count == 0 || modelsCount == 0 {
		return Catalog{}, fmt.Errorf("price feed contains no usable chat prices")
	}
	for _, provider := range sortedKeys(models) {
		for _, id := range sortedKeys(models[provider].Models) {
			m := models[provider].Models[id]
			if !subscriptionProvider(provider) || m.Cost.Input == nil || m.Cost.Output == nil || *m.Cost.Input != 0 || *m.Cost.Output != 0 {
				continue
			}
			// A family must name an actual priced SKU, not a broad family such
			// as "gpt". Never pick the cheapest/latest member heuristically.
			target := c.Aliases[normalizedID(id)]
			if target == "" {
				target = c.Aliases[normalizedID(m.Family)]
			}
			if target == "" {
				if _, ok := c.Rows[normalizedID(m.Family)]; ok {
					target = normalizedID(m.Family)
				}
			}
			if target == "" {
				continue
			}
			c.Aliases[provider+"/"+normalizedID(id)] = target
			// Only distinctive, known subscription IDs can cross harnesses.
			if provider == "kimi-for-coding" && (id == "k3" || id == "k3-256k" || strings.HasPrefix(id, "kimi-")) {
				c.Aliases[normalizedID(id)] = target
			}
		}
	}
	payload, err := json.Marshal(struct {
		Rows    map[string]Row
		Aliases map[string]string
	}{c.Rows, c.Aliases})
	if err != nil {
		return Catalog{}, err
	}
	hash := sha256.Sum256(payload)
	c.Version = hex.EncodeToString(hash[:])[:16]
	return c, nil
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
