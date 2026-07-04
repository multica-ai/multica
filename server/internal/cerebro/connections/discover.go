package connections

// discover.go implements endpoint discovery for REST API connections (TECH-3410).
//
// An admin who points a connection at a REST API should not have to type every
// path and method by hand. Most APIs publish a machine-readable description of
// themselves — an OpenAPI (v3) or Swagger (v2) document — at a well-known URL.
// discoverAPIEndpoints fetches that document and turns its `paths` object into
// the EndpointPermission list the permissions UI already knows how to render
// (one CRUD-gated row per path+method, via toolpolicy.discoverConnectionTools).
//
// This is the API analogue of the MCP tools/list probe in test.go: MCP servers
// advertise their tools, REST APIs advertise their endpoints. When no spec is
// found the connection is still reachable — the admin just adds endpoints by
// hand.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// discoveredEndpoint is one path and the HTTP methods declared on it in the spec.
// Summary is the human-readable label the spec declares for the path — the path
// item's own summary, or the first operation summary in verb order. A
// personalized registry spec labels its per-resource paths with the resource's
// name (e.g. "Execute data source: Orders"), so carrying it through lets the
// permissions UI and the agent-facing tool description show that name instead
// of a bare "<METHOD> /data-sources/<uuid>/execute".
type discoveredEndpoint struct {
	Path    string   `json:"path"`
	Methods []string `json:"methods"`
	Summary string   `json:"summary,omitempty"`
}

// httpVerbs is the set of OpenAPI path-item keys that are real HTTP operations.
// A path item can also carry non-operation keys (parameters, summary, $ref, …)
// which must be ignored.
var httpVerbs = map[string]string{
	"get":     "GET",
	"post":    "POST",
	"put":     "PUT",
	"patch":   "PATCH",
	"delete":  "DELETE",
	"head":    "HEAD",
	"options": "OPTIONS",
}

// specCandidatePaths are the conventional locations an OpenAPI / Swagger
// document is served from, tried relative to both the connection URL and its
// origin. Ordered most-common first.
var specCandidatePaths = []string{
	"/openapi.json",
	"/swagger.json",
	"/openapi.yaml",
	"/openapi.yml",
	"/swagger.yaml",
	"/api-docs",
	"/v3/api-docs",
	"/swagger/v1/swagger.json",
}

// specAuthRejection records a spec candidate that answered 401/403 — the
// strongest signal the admin's credential (not the URL) is the problem. Without
// surfacing this, a rejected key reads as "no spec found" and admins chase the
// URL instead of the key (FIR-2640).
type specAuthRejection struct {
	URL        string
	StatusCode int
}

// discoverAPIEndpoints attempts to find and parse an OpenAPI/Swagger spec for the
// given API base URL, returning the declared endpoints sorted by path. It tries,
// in order: the URL itself (it may already point at a spec), then the well-known
// spec paths appended to the URL, then the same paths on the URL's origin. The
// first candidate that parses into at least one endpoint wins. Returns nil
// endpoints when nothing parses — discovery is best-effort and never fails a
// test — plus the first auth rejection (401/403) seen, so the caller can tell
// the admin their credential was refused rather than pretend no spec exists.
func discoverAPIEndpoints(ctx context.Context, client *http.Client, baseURL string, auth AuthConfig) ([]discoveredEndpoint, *specAuthRejection) {
	var rejection *specAuthRejection
	for _, candidate := range specCandidates(baseURL) {
		body, status, ok := fetchSpec(ctx, client, candidate, auth)
		if !ok {
			if rejection == nil && (status == http.StatusUnauthorized || status == http.StatusForbidden) {
				rejection = &specAuthRejection{URL: candidate, StatusCode: status}
			}
			continue
		}
		if eps := parseSpec(body); len(eps) > 0 {
			return eps, nil
		}
	}
	return nil, rejection
}

// resolveExplicitSpec resolves the endpoint catalogue from an explicitly
// provided spec — an uploaded document (specContent) or a direct spec URL.
// Unlike the best-effort probing above, explicit input gets an explicit error
// message back when it cannot be used, so the admin knows what to fix.
func resolveExplicitSpec(ctx context.Context, client *http.Client, specURL, specContent string, auth AuthConfig) ([]discoveredEndpoint, string) {
	if strings.TrimSpace(specContent) != "" {
		if eps := parseSpec([]byte(specContent)); len(eps) > 0 {
			return eps, ""
		}
		return nil, "the uploaded document is not a parsable OpenAPI/Swagger spec (no endpoints found)"
	}
	specURL = strings.TrimSpace(specURL)
	body, status, ok := fetchSpec(ctx, client, specURL, auth)
	if !ok {
		switch status {
		case 0:
			return nil, "could not fetch the spec URL (unreachable)"
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, fmt.Sprintf("the spec URL rejected the connection's credential (HTTP %d) — check the Bearer token / API key", status)
		default:
			return nil, fmt.Sprintf("could not fetch the spec URL (HTTP %d)", status)
		}
	}
	if eps := parseSpec(body); len(eps) > 0 {
		return eps, ""
	}
	if looksLikeHTML(body) {
		return nil, "the spec URL returned an HTML page instead of an OpenAPI/Swagger document — likely a login wall (e.g. Cloudflare Access) in front of the API"
	}
	return nil, "the spec URL did not return a parsable OpenAPI/Swagger document"
}

// looksLikeHTML reports whether a fetched body is an HTML page rather than a
// JSON/YAML document — the signature of an auth portal or error page served
// with a 200 status.
func looksLikeHTML(body []byte) bool {
	head := strings.ToLower(strings.TrimSpace(string(body[:min(len(body), 512)])))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}

// specCandidates builds the ordered, de-duplicated list of URLs to probe for a
// spec, given the connection's base URL.
func specCandidates(baseURL string) []string {
	trimmed := strings.TrimRight(baseURL, "/")
	seen := map[string]struct{}{}
	var out []string
	add := func(u string) {
		if u == "" {
			return
		}
		if _, dup := seen[u]; dup {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	// 1. The URL itself — it may already be a spec document.
	add(trimmed)

	// 2. Well-known paths appended to the full URL (handles APIs mounted under
	//    a path prefix, e.g. https://host/api → https://host/api/openapi.json).
	for _, p := range specCandidatePaths {
		add(trimmed + p)
	}

	// 3. Well-known paths on the bare origin (scheme://host[:port]).
	if u, err := url.Parse(trimmed); err == nil && u.Scheme != "" && u.Host != "" {
		origin := u.Scheme + "://" + u.Host
		for _, p := range specCandidatePaths {
			add(origin + p)
		}
	}
	return out
}

// fetchSpec GETs a candidate URL and returns its body when the response looks
// like a usable document (2xx, non-empty), plus the HTTP status code (0 when
// the request never got a response). Auth + Cloudflare Access headers are
// attached so specs behind auth are reachable.
func fetchSpec(ctx context.Context, client *http.Client, specURL string, auth AuthConfig) ([]byte, int, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
	if err != nil {
		return nil, 0, false
	}
	req.Header.Set("Accept", "application/json, application/yaml, text/yaml, */*")
	addAuthHeaders(req, auth)

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, false
	}
	// Cap the read so a stray non-spec endpoint streaming a large body can't
	// blow up memory. 8 MiB comfortably fits even large specs.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil || len(body) == 0 {
		return nil, resp.StatusCode, false
	}
	return body, resp.StatusCode, true
}

// parseSpec parses an OpenAPI v3 or Swagger v2 document (JSON or YAML) and
// extracts its endpoints. Returns nil when the body is not a spec or declares no
// paths. Both spec versions use the same top-level `paths` shape, so one parser
// covers both.
func parseSpec(body []byte) []discoveredEndpoint {
	doc := decodeSpecDoc(body)
	if doc == nil {
		return nil
	}
	// Only trust documents that self-identify as OpenAPI/Swagger, so an
	// arbitrary JSON endpoint that happens to have a "paths" key isn't mistaken
	// for a spec.
	if _, isV3 := doc["openapi"]; !isV3 {
		if _, isV2 := doc["swagger"]; !isV2 {
			return nil
		}
	}
	pathsRaw, ok := doc["paths"].(map[string]any)
	if !ok {
		return nil
	}

	var out []discoveredEndpoint
	for path, itemRaw := range pathsRaw {
		if !strings.HasPrefix(path, "/") {
			continue
		}
		item, ok := itemRaw.(map[string]any)
		if !ok {
			continue
		}
		var methods []string
		for key := range item {
			if verb, isVerb := httpVerbs[strings.ToLower(key)]; isVerb {
				methods = append(methods, verb)
			}
		}
		if len(methods) == 0 {
			continue
		}
		sort.Strings(methods)
		out = append(out, discoveredEndpoint{Path: path, Methods: methods, Summary: pathSummary(item)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// pathSummary picks the human-readable label for a path item: the path item's
// own `summary`, or the first non-empty operation `summary` in deterministic
// verb order. Descriptions are deliberately not used — they are long-form prose,
// while summaries are the one-line labels specs put on operations.
func pathSummary(item map[string]any) string {
	if s, ok := item["summary"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	verbs := make([]string, 0, len(item))
	for key := range item {
		if _, isVerb := httpVerbs[strings.ToLower(key)]; isVerb {
			verbs = append(verbs, key)
		}
	}
	sort.Strings(verbs)
	for _, verb := range verbs {
		op, ok := item[verb].(map[string]any)
		if !ok {
			continue
		}
		if s, ok := op["summary"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// decodeSpecDoc decodes a spec body to a generic map, trying JSON first then
// YAML. YAML decoding normalizes to map[string]any (yaml.v3 yields
// map[string]interface{} for mappings with string keys, which is the shape
// parseSpec expects).
func decodeSpecDoc(body []byte) map[string]any {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err == nil && doc != nil {
		return doc
	}
	var y map[string]any
	if err := yaml.Unmarshal(body, &y); err == nil && y != nil {
		return normalizeYAML(y)
	}
	return nil
}

// normalizeYAML recursively converts the map/slice types yaml.v3 produces into
// the map[string]any / []any shapes parseSpec walks. yaml.v3 already uses
// map[string]any for string-keyed mappings, but nested values may need the same
// treatment, and non-string keys are coerced via fmt.
func normalizeYAML(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		// yaml.v3 can yield map[any]any in some paths; coerce keys to strings.
		if anyMap, ok := v.(map[any]any); ok {
			m = make(map[string]any, len(anyMap))
			for k, val := range anyMap {
				m[fmt.Sprintf("%v", k)] = val
			}
		} else {
			return nil
		}
	}
	for k, val := range m {
		switch child := val.(type) {
		case map[any]any:
			m[k] = normalizeYAML(child)
		case map[string]any:
			m[k] = normalizeYAML(child)
		}
	}
	return m
}
