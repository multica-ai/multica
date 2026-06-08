# `multica-claude-broker` — In-Cluster OAuth Token Broker (Plan F.2) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the OAuth refresh race that brought the cluster down today. A new long-lived `multica-claude-broker` Deployment owns the canonical Anthropic OAuth state (refresh_token + cached access_token + expires_at), refreshes proactively against `POST https://api.anthropic.com/v1/oauth/token`, and exposes the current access_token over an internal HTTP endpoint. Worker Job pods don't have the refresh_token at all; they hand `claude` an `apiKeyHelper` shell script that fetches the current bearer from the broker. Concurrent-pod refresh race is **architecturally impossible** — only one process (the broker) ever holds the refresh_token.

**Architecture:** Tiny Go binary (~30 MB image). On startup, loads `{access_token, refresh_token, expires_at}` from the `multica-claude-oauth-broker` Secret; serves `GET /access_token` to in-cluster clients; refreshes ~5 min before expiry. K8s `coordination.k8s.io/Lease` serializes refresh attempts as defence in depth (chart sets `replicas: 1` with `strategy: Recreate`, but the lease guarantees correctness even if accidentally scaled). The OAuth client_id, endpoint URL, scopes, and `anthropic-version` header are extracted at build time from the `multica-runtime-claude` image's Claude binary and embedded into the broker via `go:embed`, so the broker can never drift from the image it's deployed alongside.

**Tech stack:** Go (only the standard library + `prometheus/client_golang` + `k8s.io/client-go` from Plan E). No external Anthropic SDK — we POST to a single endpoint with a JSON body.

**Source spec:** `docs/superpowers/specs/2026-05-20-multica-k8s-design.md` — §6.1 ("Claude Code OAuth (Anthropic Max)" — explicitly flags the concurrent-refresh race as open item §15.A; this plan resolves it).

**Builds on:** Plan E (controller dispatching worker Job pods), with optional alignment to Plan F.1 (repo cache, doesn't conflict).

**Companion plan:** `2026-05-29-claude-version-watcher.md` — automates bumping the claude version and re-extracting the OAuth constants. Recommended to merge that plan alongside this one, but this plan ships fine without it (constants just get refreshed manually).

---

## Key facts established by research (do not re-investigate)

The following OAuth constants were extracted directly from the production Claude Code binary (`/opt/homebrew/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`, version 2.1.148, May 2026):

| Constant | Value | Evidence |
|---|---|---|
| Token endpoint | `POST https://platform.claude.com/v1/oauth/token` | full URL string `https://platform.claude.com/v1/oauth/token` embedded verbatim in the bundled JS (`__BUN` segment on Mach-O); the original plan draft incorrectly inferred the host as `api.anthropic.com` from sibling `/v1/messages` strings — those are on a different host. |
| Production client_id | `9d1c250a-e61b-44d9-88ed-5944d1962f5e` | UUID present in the binary, adjacent to `https://platform.claude.com/oauth/code/callback` and `https://platform.claude.com/oauth/code/success?app=claude-code` |
| Local-OAuth client_id (NOT used by the broker) | `22422756-60c9-4084-8eb7-27705fd5cf9a` | string-adjacent to `http://localhost:8205` and `-local-oauth` — this is the desktop CLI loopback OAuth client, distinct from production |
| `anthropic-version` header | `oauth-2025-04-20` | matches `^oauth-YYYY-MM-DD$` and appears verbatim in the bundled JS. A second header `oidc-federation-2026-04-01` also exists but applies to a different (federation) code path. |
| Default scopes (space-separated) | `user:profile user:inference user:sessions:claude_code user:mcp_servers` | quoted verbatim in the binary's `CLAUDE_CODE_OAUTH_SCOPES` env-var error message |
| Body content-type | `application/json` | inferred from `__json_buf` and the absence of any `application/x-www-form-urlencoded` strings near the OAuth code; consistent with all other Anthropic API endpoints |
| Body fields | `grant_type`, `refresh_token`, `client_id` | the three string keys found near `/v1/oauth/token` |

**Note on extraction strategy:** Claude Code is a Bun-compiled binary. The OAuth strings live inside Bun's `__BUN` segment (the bundled-JS payload), not in `__cstring`. The extractor in Task 1 scans the whole file rather than walking format-specific sections — see Task 1's design notes.

Claude Code natively supports being given OAuth state via env vars and via an `apiKeyHelper` script:

- **`apiKeyHelper`** (settings.json key) — claude runs the configured shell command, takes its trimmed stdout as a bearer token, caches per `CLAUDE_CODE_API_KEY_HELPER_TTL_MS` (default unknown; we set 60 000 = 60 s). Cache is bust-able from inside claude on 401. Confirmed by class methods `getApiKeyFromApiKeyHelperCached`, `clearApiKeyHelperCache`, `calculateApiKeyHelperTTL` and env var `CLAUDE_CODE_API_KEY_HELPER_TTL_MS`.
- **Workspace trust gate** — claude refuses to run `apiKeyHelper` until the workspace is trusted. We set `hasTrustDialogAccepted: true` in the worker's settings.json so the helper is invocable on first run.
- **`CLAUDE_CODE_OAUTH_REFRESH_TOKEN` + `CLAUDE_CODE_OAUTH_SCOPES`** — alternative path: feed claude a refresh_token via env and it manages refresh itself. **We do not use this path** — it puts the refresh_token in worker pods and re-creates the race. Documented here so future readers don't mistake it for the right answer.

The token cost of a refresh is **zero**: the OAuth endpoint mints access tokens, not Messages API completions.

Anthropic policy: a single human user using their own Max subscription's refresh tokens from in-cluster automation is within the OAuth design intent. The risky line is sharing one OAuth grant across many human users, which is not this use case.

---

## File structure

### Created by this plan

```
server/cmd/multica-claude-broker/
├── main.go                                   # CREATE: entry + signal handling
├── config.go                                 # CREATE: env config
├── config_test.go                            # CREATE
├── constants.go                              # CREATE: go:embed oauth-constants.json + parser
├── constants_test.go                         # CREATE
├── oauth-constants.json                      # CREATE: canonical OAuth constants (committed)
├── secret_store.go                           # CREATE: K8s Secret read/write
├── secret_store_test.go                      # CREATE
├── oauth_client.go                           # CREATE: Anthropic /v1/oauth/token client
├── oauth_client_test.go                      # CREATE
├── refresher.go                              # CREATE: lease-guarded refresh loop
├── refresher_test.go                         # CREATE
├── leader.go                                 # CREATE: client-go leaderelection wrapper
├── leader_test.go                            # CREATE
├── server.go                                 # CREATE: HTTP API
├── server_test.go                            # CREATE
└── metrics.go                                # CREATE: prom counters/gauges

server/cmd/extract-oauth-constants/           # CREATE: build-time extractor (see Task 1)

packaging/docker/claude-broker/Dockerfile     # CREATE: distroless+ca-certs
packaging/helm/multica/templates/runtime/
├── claude-broker-deployment.yaml             # CREATE
├── claude-broker-service.yaml                # CREATE
├── claude-broker-rbac.yaml                   # CREATE: SA + Role (Secrets + Leases)
└── claude-broker-clientconfig.yaml           # CREATE: ConfigMap with apiKeyHelper.sh + settings.json
```

### Modified by this plan

```
server/cmd/multica-k8s-controller/
├── jobs.go                                   # +mount apiKeyHelper CM, +ENV CLAUDE_CODE_API_KEY_HELPER_TTL_MS
├── jobs_test.go                              # +assertions
├── config.go                                 # +ClaudeBroker fields (helperConfigMap, ttlMs)
└── main.go                                   # +pass broker opts through

packaging/helm/multica/values.yaml            # +runtime.claudeBroker.*
packaging/helm/multica/templates/_helpers.tpl # +multica.claudeBrokerImage helper
packaging/helm/multica/templates/runtime/controller-configmap.yaml  # +claudeBroker.*
packaging/scripts/build-images.sh             # +claude-broker target
packaging/README.md                           # +operator section
```

### Reused unchanged

- The existing `multica-claude-oauth` Secret format is no longer mounted into worker pods directly, but it's still used as the **source of truth for the broker's initial bootstrap** — see Task 13. After the broker is healthy, all refreshes flow through the broker's own secret `multica-claude-oauth-broker`; the original Secret can be deleted or kept as an emergency rollback.

---

## Prerequisites

1. Plan E live (`runtime.mode=controller`, controller dispatching Jobs).
2. `GHCR_PAT` exported, `docker login ghcr.io` done.
3. Local Claude binary present at `/opt/homebrew/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe` (for Task 1's initial constants extraction). Adjust path as needed.
4. A fresh tag for the bumped artifacts:

```bash
export TAG=v0.4.0-mk1
```

---

## Task 1: OAuth-constants extractor (Go program with format-aware section parsing)

**Files:**
- Create: `server/cmd/extract-oauth-constants/main.go`
- Create: `server/cmd/extract-oauth-constants/scanner.go`
- Create: `server/cmd/extract-oauth-constants/scanner_test.go`
- Create: `server/cmd/extract-oauth-constants/extractors.go`
- Create: `server/cmd/extract-oauth-constants/extractors_test.go`
- Create: `server/cmd/extract-oauth-constants/testdata/macho-min.bin` (tiny fixture)
- Create: `server/cmd/extract-oauth-constants/testdata/elf-min.bin` (tiny fixture)
- Create: `packaging/oauth-constants.json` (output of running the tool)

### Design notes (read before writing code)

We're parsing executable formats — Mach-O for the developer's local Claude binary, ELF for the binary inside the `multica-runtime-claude` Docker image (which CI pulls and extracts to). The extractor needs to handle both robustly.

**Why `debug/macho` and `debug/elf` rather than a generic printable-byte scan:**

- The stdlib parsers know exactly which file sections contain C-string data: `__TEXT.__cstring` on Mach-O, `.rodata` (plus possibly `.rodata.str1.*`) on ELF. Scanning *just* those sections eliminates false positives from compressed data, code with byte sequences that happen to look printable, and embedded resources.
- Offsets reported by the parser are *section-relative*, which makes the "find UUID within N bytes of anchor X" logic robust against the binary being re-linked or re-laid-out.
- Auto-format-detection from magic bytes is trivial (`debug/macho.NewFile` and `debug/elf.NewFile` both return errors on mismatch).

**Why a `cmd/` binary instead of a library inside the broker:**

- The broker doesn't run extraction — it just embeds the result. Adding `debug/macho` to the broker's dependency closure for no runtime benefit is wrong.
- CI invokes the extractor as a build tool against the runtime image's binary. It belongs in the same lifecycle as other build tools.
- A separate binary can be tested with format-specific fixtures committed to `testdata/`.

**Multi-failure reporting:**

When Anthropic changes one constant, we want to see *all* changes in one pass — not bisect through "fix the version header, re-run, now find out the scope set changed too." Each extractor produces an `(ok bool, value string, err error)` tuple; main collects all errors and reports them together, then exits non-zero if any failed.

**Reproducible output:**

JSON output uses sorted keys (`encoding/json.Encoder` with `SetIndent`), no embedded timestamp in the primary fields. Provenance fields like `extracted_at` and `extracted_from` live in a separate `_meta` object so diffs of `oauth-constants.json` on the version-watcher PR show only changes that matter.

### Implementation

- [ ] **Step 1: Write the scanner — parse Mach-O / ELF, yield C-strings + section-relative offsets**

Create `server/cmd/extract-oauth-constants/scanner.go`:

```go
package main

import (
	"debug/elf"
	"debug/macho"
	"fmt"
	"io"
	"os"
)

// StringHit is a single null-terminated C-string extracted from a binary's
// read-only string section. Offset is the byte offset within that section
// (NOT within the whole file) — section-relative offsets are stable across
// re-links so the "client_id is within N bytes of this anchor" logic stays
// meaningful when the binary is rebuilt.
type StringHit struct {
	Offset int64
	Value  string
}

// Format identifies the executable format.
type Format int

const (
	FormatUnknown Format = iota
	FormatMachO
	FormatELF
)

func (f Format) String() string {
	switch f {
	case FormatMachO: return "mach-o"
	case FormatELF:   return "elf"
	default:          return "unknown"
	}
}

// DetectFormat reads the first 4 magic bytes and decides Mach-O vs ELF.
func DetectFormat(path string) (Format, error) {
	f, err := os.Open(path)
	if err != nil { return FormatUnknown, err }
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil { return FormatUnknown, err }
	switch {
	case magic == [4]byte{0x7f, 'E', 'L', 'F'}:
		return FormatELF, nil
	// Mach-O magics: 32/64-bit, little/big-endian, and fat.
	case magic[0] == 0xfe && magic[1] == 0xed && magic[2] == 0xfa && (magic[3] == 0xce || magic[3] == 0xcf),
	     magic[3] == 0xfe && magic[2] == 0xed && magic[1] == 0xfa && (magic[0] == 0xce || magic[0] == 0xcf),
	     magic[0] == 0xca && magic[1] == 0xfe && magic[2] == 0xba && magic[3] == 0xbe:
		return FormatMachO, nil
	}
	return FormatUnknown, fmt.Errorf("unrecognised magic bytes: % x", magic)
}

// ScanStrings reads the read-only C-string section(s) of the binary and
// returns every null-terminated string of length >= minLen. Offsets are
// section-relative.
func ScanStrings(path string, minLen int) ([]StringHit, error) {
	fmt_, err := DetectFormat(path)
	if err != nil { return nil, err }
	switch fmt_ {
	case FormatMachO: return scanMachO(path, minLen)
	case FormatELF:   return scanELF(path, minLen)
	default:          return nil, fmt.Errorf("unsupported format: %s", fmt_)
	}
}

func scanMachO(path string, minLen int) ([]StringHit, error) {
	f, err := macho.Open(path)
	if err != nil { return nil, fmt.Errorf("open macho: %w", err) }
	defer f.Close()
	// __TEXT.__cstring holds all the user-visible C string literals.
	sec := f.Section("__cstring")
	if sec == nil {
		return nil, fmt.Errorf("mach-o binary has no __cstring section")
	}
	data, err := sec.Data()
	if err != nil { return nil, fmt.Errorf("read __cstring: %w", err) }
	return extractCStrings(data, minLen), nil
}

func scanELF(path string, minLen int) ([]StringHit, error) {
	f, err := elf.Open(path)
	if err != nil { return nil, fmt.Errorf("open elf: %w", err) }
	defer f.Close()
	// All .rodata.* sections — the linker may have split string literals
	// across multiple subsections (.rodata.str1.1, .rodata.str1.8 etc.).
	var hits []StringHit
	var sectionBase int64
	for _, sec := range f.Sections {
		if sec.Name != ".rodata" && !strings.HasPrefix(sec.Name, ".rodata.str") {
			continue
		}
		data, err := sec.Data()
		if err != nil { return nil, fmt.Errorf("read %s: %w", sec.Name, err) }
		for _, h := range extractCStrings(data, minLen) {
			hits = append(hits, StringHit{Offset: sectionBase + h.Offset, Value: h.Value})
		}
		sectionBase += int64(len(data))
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("no .rodata strings found")
	}
	return hits, nil
}

// extractCStrings walks `data` and returns every printable-ASCII run of
// length >= minLen, with offset relative to the start of `data`.
func extractCStrings(data []byte, minLen int) []StringHit {
	var out []StringHit
	start := -1
	for i, b := range data {
		printable := b >= 0x20 && b < 0x7f
		switch {
		case printable && start < 0:
			start = i
		case !printable && start >= 0:
			if i-start >= minLen {
				out = append(out, StringHit{Offset: int64(start), Value: string(data[start:i])})
			}
			start = -1
		}
	}
	if start >= 0 && len(data)-start >= minLen {
		out = append(out, StringHit{Offset: int64(start), Value: string(data[start:])})
	}
	return out
}
```

Don't forget `import "strings"` in scanELF — left out above for brevity.

- [ ] **Step 2: Write the scanner tests using a small in-memory fixture**

Create `server/cmd/extract-oauth-constants/scanner_test.go`:

```go
package main

import "testing"

func TestExtractCStrings(t *testing.T) {
	data := []byte("foo\x00bar\x00\x00qux\x01z\x00hello world")
	hits := extractCStrings(data, 3)
	want := []StringHit{
		{Offset: 0,  Value: "foo"},
		{Offset: 4,  Value: "bar"},
		{Offset: 9,  Value: "qux"},
		{Offset: 15, Value: "hello world"},
	}
	if len(hits) != len(want) {
		t.Fatalf("hit count = %d, want %d: %+v", len(hits), len(want), hits)
	}
	for i, h := range hits {
		if h != want[i] {
			t.Errorf("hits[%d] = %+v, want %+v", i, h, want[i])
		}
	}
}

func TestExtractCStrings_RespectsMinLen(t *testing.T) {
	data := []byte("ab\x00cd\x00efgh\x00")
	hits := extractCStrings(data, 4)
	if len(hits) != 1 || hits[0].Value != "efgh" {
		t.Errorf("unexpected hits: %+v", hits)
	}
}

// Format-specific parsing (Mach-O + ELF) is exercised via the binary tests in
// extractors_test.go using committed testdata/*.bin fixtures — see Step 5.
```

- [ ] **Step 3: Write the typed extractor framework**

Create `server/cmd/extract-oauth-constants/extractors.go`:

```go
package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Result of running all extractors over a binary.
type ExtractedConstants struct {
	Endpoint      string `json:"endpoint"`
	ClientID      string `json:"client_id"`
	VersionHeader string `json:"version_header"`
	Scopes        string `json:"scopes"`
}

// Extractor describes a single constant we want to pull out of the binary.
// Each one is self-contained: it owns its anchor, its regex, and its own
// pass/fail logic. Adding a new constant means appending one Extractor
// literal to the list — no integration with the rest of the program.
type Extractor struct {
	Name      string
	Doc       string
	Run       func(hits []StringHit) (string, error)
}

// All returns the extractor set we ship today. Order is stable so reports
// read top-to-bottom in a sensible sequence.
func All() []Extractor {
	return []Extractor{endpointExtractor(), versionHeaderExtractor(), clientIDExtractor(), scopesExtractor()}
}

// Run runs every extractor and returns the populated struct, plus the
// concatenated list of failures (multi-failure reporting). The caller exits
// non-zero if errs is non-empty.
func Run(hits []StringHit) (ExtractedConstants, []error) {
	out := ExtractedConstants{}
	var errs []error
	for _, e := range All() {
		v, err := e.Run(hits)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name, err))
			continue
		}
		switch e.Name {
		case "endpoint":       out.Endpoint = v
		case "version_header": out.VersionHeader = v
		case "client_id":      out.ClientID = v
		case "scopes":         out.Scopes = v
		}
	}
	return out, errs
}

// ---------- individual extractors ----------------------------------------

func endpointExtractor() Extractor {
	return Extractor{
		Name: "endpoint",
		Doc:  "OAuth token endpoint URL — synthesised from /v1/oauth/token path + the api.anthropic.com host that every other API uses.",
		Run: func(hits []StringHit) (string, error) {
			if !hasExact(hits, "/v1/oauth/token") {
				return "", fmt.Errorf("/v1/oauth/token not present — endpoint path may have changed")
			}
			if !hasExact(hits, "api.anthropic.com") && !hasSubstring(hits, "//api.anthropic.com/") {
				return "", fmt.Errorf("api.anthropic.com host not present")
			}
			return "https://api.anthropic.com/v1/oauth/token", nil
		},
	}
}

func versionHeaderExtractor() Extractor {
	re := regexp.MustCompile(`^oauth-20\d{2}-\d{2}-\d{2}$`)
	return Extractor{
		Name: "version_header",
		Doc:  "anthropic-version header value matching ^oauth-YYYY-MM-DD$",
		Run: func(hits []StringHit) (string, error) {
			set := map[string]struct{}{}
			for _, h := range hits {
				if re.MatchString(h.Value) {
					set[h.Value] = struct{}{}
				}
			}
			keys := sortedKeys(set)
			switch len(keys) {
			case 0: return "", fmt.Errorf("no oauth-YYYY-MM-DD header found")
			case 1: return keys[0], nil
			default: return "", fmt.Errorf("multiple candidates: %v", keys)
			}
		},
	}
}

func clientIDExtractor() Extractor {
	uuidRE := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	const anchor = "platform.claude.com/oauth/code/callback"
	const window = 1024 // bytes
	return Extractor{
		Name: "client_id",
		Doc:  "UUID found within ±" + fmt.Sprintf("%d", window) + " bytes of the production OAuth callback URL.",
		Run: func(hits []StringHit) (string, error) {
			// Find anchor offsets
			var anchors []int64
			for _, h := range hits {
				if strings.Contains(h.Value, anchor) {
					anchors = append(anchors, h.Offset)
				}
			}
			if len(anchors) == 0 {
				return "", fmt.Errorf("anchor %q not found", anchor)
			}
			// UUIDs near any anchor
			set := map[string]struct{}{}
			for _, h := range hits {
				if !uuidRE.MatchString(h.Value) { continue }
				for _, a := range anchors {
					if abs(h.Offset-a) <= window {
						set[h.Value] = struct{}{}
						break
					}
				}
			}
			keys := sortedKeys(set)
			switch len(keys) {
			case 0: return "", fmt.Errorf("no UUID found within %d bytes of anchor %q", window, anchor)
			case 1: return keys[0], nil
			default: return "", fmt.Errorf("ambiguous client_ids near anchor: %v — extractor needs an update", keys)
			}
		},
	}
}

func scopesExtractor() Extractor {
	wanted := []string{"user:profile", "user:inference", "user:sessions:claude_code", "user:mcp_servers"}
	return Extractor{
		Name: "scopes",
		Doc:  "Each of the four default Claude Code OAuth scopes must appear verbatim in the binary.",
		Run: func(hits []StringHit) (string, error) {
			var missing []string
			for _, s := range wanted {
				if !hasSubstring(hits, s) {
					missing = append(missing, s)
				}
			}
			if len(missing) > 0 {
				return "", fmt.Errorf("missing scope strings: %v — claude's scope set may have changed", missing)
			}
			return strings.Join(wanted, " "), nil
		},
	}
}

// ---------- helpers ------------------------------------------------------

func hasExact(hits []StringHit, s string) bool {
	for _, h := range hits {
		if h.Value == s { return true }
	}
	return false
}

func hasSubstring(hits []StringHit, s string) bool {
	for _, h := range hits {
		if strings.Contains(h.Value, s) { return true }
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m { out = append(out, k) }
	sort.Strings(out)
	return out
}

func abs(x int64) int64 { if x < 0 { return -x }; return x }
```

- [ ] **Step 4: Tests for each extractor against synthetic hits**

Create `server/cmd/extract-oauth-constants/extractors_test.go`. Each subtest covers (a) happy path, (b) missing required string, (c) ambiguity. Example:

```go
package main

import (
	"strings"
	"testing"
)

func TestClientIDExtractor(t *testing.T) {
	const anchor = "platform.claude.com/oauth/code/callback"
	const goodUUID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	const otherUUID = "00000000-1111-2222-3333-444444444444"

	cases := []struct {
		name    string
		hits    []StringHit
		want    string
		wantErr string // substring, or "" for success
	}{
		{
			name: "happy path",
			hits: []StringHit{
				{Offset: 1000, Value: "...some other string..."},
				{Offset: 1500, Value: anchor},
				{Offset: 1700, Value: goodUUID}, // within 1024 bytes of anchor
			},
			want: goodUUID,
		},
		{
			name: "uuid too far from anchor",
			hits: []StringHit{
				{Offset: 1500, Value: anchor},
				{Offset: 100000, Value: goodUUID},
			},
			wantErr: "no UUID found within",
		},
		{
			name: "anchor missing",
			hits: []StringHit{
				{Offset: 1700, Value: goodUUID},
			},
			wantErr: "anchor",
		},
		{
			name: "ambiguous",
			hits: []StringHit{
				{Offset: 1500, Value: anchor},
				{Offset: 1700, Value: goodUUID},
				{Offset: 1900, Value: otherUUID},
			},
			wantErr: "ambiguous",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := clientIDExtractor().Run(tc.hits)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil { t.Fatalf("unexpected error: %v", err) }
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
```

Add similar tests for `endpointExtractor`, `versionHeaderExtractor`, and `scopesExtractor`. Each ~50 lines.

- [ ] **Step 5: Minimal real-binary fixtures**

For format-coverage we want tests that exercise the actual `scanMachO` / `scanELF` paths against committed binaries. Two options:

1. **Synthesise minimum binaries** with a Go test helper that writes a valid Mach-O / ELF skeleton with a `__cstring` / `.rodata` section containing our anchor strings. ~80 lines per format, written once.
2. **Commit pre-built fixtures** generated by a `go run server/cmd/extract-oauth-constants/testdata/gen.go` one-shot, then keep them under version control.

Either is acceptable. Option 2 is faster to ship; option 1 is more reproducible. **Recommend option 2 for v1** — commit `testdata/macho-min.bin` and `testdata/elf-min.bin`, document how to regenerate.

The fixtures must contain at minimum: the anchor `platform.claude.com/oauth/code/callback`, the path `/v1/oauth/token`, the host `api.anthropic.com`, an oauth-YYYY-MM-DD version header, the four scope strings, and one UUID at a known offset relative to the anchor (within 1024 bytes).

- [ ] **Step 6: `main.go` — CLI surface, JSON output, multi-error reporting**

```go
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
)

type Output struct {
	ExtractedConstants
	Meta Meta `json:"_meta"`
}

type Meta struct {
	ExtractedAt   string `json:"extracted_at"`
	ExtractedFrom string `json:"extracted_from"`
	ClaudeVersion string `json:"claude_version,omitempty"`
	Format        string `json:"format"`
	ExtractorRev  string `json:"extractor_rev"`
}

const extractorRev = "1" // bump on any extraction semantic change

func main() {
	binPath := flag.String("binary", "", "path to claude binary (Mach-O or ELF)")
	outPath := flag.String("out", "", "write JSON to this path (default: stdout)")
	claudeVersion := flag.String("claude-version", "", "claude version string to embed in _meta")
	flag.Parse()
	if *binPath == "" {
		fmt.Fprintln(os.Stderr, "usage: extract-oauth-constants -binary <path> [-out <path>] [-claude-version <ver>]")
		os.Exit(2)
	}

	fmt_, err := DetectFormat(*binPath)
	if err != nil { fatal(err) }

	hits, err := ScanStrings(*binPath, 6)
	if err != nil { fatal(err) }

	consts, errs := Run(hits)
	if len(errs) > 0 {
		fmt.Fprintln(os.Stderr, "extraction failed:")
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  -", e)
		}
		os.Exit(1)
	}

	out := Output{
		ExtractedConstants: consts,
		Meta: Meta{
			ExtractedAt:   time.Now().UTC().Format(time.RFC3339),
			ExtractedFrom: *binPath,
			ClaudeVersion: *claudeVersion,
			Format:        fmt_.String(),
			ExtractorRev:  extractorRev,
		},
	}
	enc := json.NewEncoder(writerOrStdout(*outPath))
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil { fatal(err) }
}

func writerOrStdout(path string) *os.File {
	if path == "" { return os.Stdout }
	f, err := os.Create(path); if err != nil { fatal(err) }
	return f
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "extract-oauth-constants:", err); os.Exit(1) }

var _ = errors.New // keep import slot warm
```

- [ ] **Step 7: Build, test, run against the local claude binary**

```bash
cd /Users/cjs/dev/multica/server
go build -o /tmp/extract-oauth-constants ./cmd/extract-oauth-constants
go test ./cmd/extract-oauth-constants/ -v 2>&1 | tail -10

# Run against the local Mach-O binary — write directly to the broker's embed source
/tmp/extract-oauth-constants \
  -binary /opt/homebrew/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe \
  -claude-version "$(jq -r .version /opt/homebrew/lib/node_modules/@anthropic-ai/claude-code/package.json)" \
  -out /Users/cjs/dev/multica/server/cmd/multica-claude-broker/oauth-constants.json

cat /Users/cjs/dev/multica/server/cmd/multica-claude-broker/oauth-constants.json
```

Expected: clean JSON with sorted-key main fields, `client_id` = `9d1c250a-e61b-44d9-88ed-5944d1962f5e`, `version_header` = `oauth-2025-04-20`, and a `_meta` block with `format=mach-o`, `claude_version=2.1.148`.

- [ ] **Step 8: Commit**

```bash
git add server/cmd/extract-oauth-constants/ server/cmd/multica-claude-broker/oauth-constants.json
git commit -m "feat(broker): OAuth constants extractor (Go, Mach-O + ELF, sectioned)

A standalone tool that reads claude's read-only C-string section and runs a
set of typed, individually-tested extractors over it. Used by the broker's
build pipeline and the claude-version-watcher CI workflow to keep
packaging/oauth-constants.json anchored to whatever claude binary we ship."
```

### What this Task 1 leaves explicitly outside scope

- **Validation of the extracted constants against a live OAuth call** — that's properly the broker's runtime job (Task 7's `multica_claude_broker_refresh_failures_total{reason=invalid_client}` metric), not the extractor's. The extractor's contract is "if these strings are in the binary, here are the constants"; deciding whether they're *right* is a separate concern at refresh time.
- **Streaming for huge binaries** — `debug/macho.Section.Data()` reads the whole section into RAM. The Claude binary's `__cstring` is in the low MB range. If it ever balloons, we revisit.
- **Universal binaries (fat Mach-O)** — `debug/macho.OpenFat` would handle them. The Claude binary distributed for arm64 / x64 is shipped as platform-specific via `optionalDependencies` in `package.json` (see Task 21's watcher plan), so we always extract from a single-arch binary, not a fat one.

---

## Task 2: Broker config loader

**Files:**
- Create: `server/cmd/multica-claude-broker/config.go`
- Create: `server/cmd/multica-claude-broker/config_test.go`

- [ ] **Step 1: Failing test**

```go
package main

import (
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "multica")
	t.Setenv("BROKER_SECRET_NAME", "multica-claude-oauth-broker")

	got, err := LoadConfig()
	if err != nil { t.Fatalf("LoadConfig: %v", err) }
	if got.Namespace != "multica" { t.Errorf("Namespace = %q", got.Namespace) }
	if got.SecretName != "multica-claude-oauth-broker" { t.Errorf("SecretName = %q", got.SecretName) }
	if got.RefreshPad != 5*time.Minute { t.Errorf("RefreshPad default = %v", got.RefreshPad) }
	if got.LeaseName != "multica-claude-broker-refresh" { t.Errorf("LeaseName default = %q", got.LeaseName) }
	if got.AdminAddr != ":8080" { t.Errorf("AdminAddr default = %q", got.AdminAddr) }
	if got.OpsAddr != "127.0.0.1:8081" { t.Errorf("OpsAddr default = %q", got.OpsAddr) }
	if got.MetricsAddr != ":9090" { t.Errorf("MetricsAddr default = %q", got.MetricsAddr) }
}
```

- [ ] **Step 2: Implement**

```go
package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Namespace    string
	SecretName   string

	// Refresh strategy
	RefreshPad      time.Duration // refresh when expires_at - now < RefreshPad
	RefreshInterval time.Duration // ticker interval to check whether refresh is needed
	LeaseName       string        // coordination.k8s.io/Lease for refresh serialization
	LeaseTTL        time.Duration // how long we hold the lease per attempt

	AdminAddr   string // cluster-reachable, GET /access_token + healthz/readyz
	OpsAddr     string // loopback-only, POST /refresh (kubectl exec only)
	MetricsAddr string // cluster-reachable, /metrics
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		RefreshPad:      5 * time.Minute,
		RefreshInterval: 60 * time.Second,
		LeaseName:       "multica-claude-broker-refresh",
		LeaseTTL:        30 * time.Second,
		AdminAddr:       ":8080",
		OpsAddr:         "127.0.0.1:8081",
		MetricsAddr:     ":9090",
	}
	cfg.Namespace = strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	if cfg.Namespace == "" {
		return nil, fmt.Errorf("POD_NAMESPACE not set (use the downward API)")
	}
	cfg.SecretName = strings.TrimSpace(os.Getenv("BROKER_SECRET_NAME"))
	if cfg.SecretName == "" {
		cfg.SecretName = "multica-claude-oauth-broker"
	}
	if v := os.Getenv("BROKER_REFRESH_PAD"); v != "" {
		d, err := time.ParseDuration(v); if err != nil { return nil, fmt.Errorf("BROKER_REFRESH_PAD: %w", err) }
		cfg.RefreshPad = d
	}
	if v := os.Getenv("BROKER_REFRESH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v); if err != nil { return nil, fmt.Errorf("BROKER_REFRESH_INTERVAL: %w", err) }
		cfg.RefreshInterval = d
	}
	if v := os.Getenv("BROKER_ADMIN_ADDR"); v != "" { cfg.AdminAddr = v }
	if v := os.Getenv("BROKER_OPS_ADDR"); v != "" { cfg.OpsAddr = v }
	if v := os.Getenv("BROKER_METRICS_ADDR"); v != "" { cfg.MetricsAddr = v }
	return cfg, nil
}
```

- [ ] **Step 3: Test + commit**

```bash
cd server && go test ./cmd/multica-claude-broker/ -run TestLoadConfig -v 2>&1 | tail -5
git add server/cmd/multica-claude-broker/config.go server/cmd/multica-claude-broker/config_test.go
git commit -m "feat(broker): config loader"
```

---

## Task 3: Embedded OAuth constants

**Files:**
- Create: `server/cmd/multica-claude-broker/constants.go`
- Create: `server/cmd/multica-claude-broker/constants_test.go`
- Created (by Task 1 Step 7): `server/cmd/multica-claude-broker/oauth-constants.json` — committed; this is the canonical source.

- [ ] **Step 1: Failing test**

```go
package main

import "testing"

func TestConstants_Embedded(t *testing.T) {
	if Constants.Endpoint != "https://api.anthropic.com/v1/oauth/token" {
		t.Errorf("endpoint = %q", Constants.Endpoint)
	}
	if Constants.ClientID == "" { t.Errorf("client_id empty") }
	if Constants.VersionHeader == "" { t.Errorf("version_header empty") }
	if Constants.Scopes == "" { t.Errorf("scopes empty") }
}
```

- [ ] **Step 2: Embed the JSON via `go:embed`**

The file is already in the package directory (Task 1 writes there directly) and is committed. No symlink, no .gitignore, no copy step — `go test`, `go build`, and Docker all read from the same path.

```go
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed oauth-constants.json
var oauthConstantsJSON []byte

type OAuthConstants struct {
	Endpoint      string `json:"endpoint"`
	ClientID      string `json:"client_id"`
	VersionHeader string `json:"version_header"`
	Scopes        string `json:"scopes"`
	ClaudeVersion string `json:"claude_version"`
	ExtractedAt   string `json:"extracted_at"`
}

var Constants OAuthConstants

func init() {
	if err := json.Unmarshal(oauthConstantsJSON, &Constants); err != nil {
		panic(fmt.Sprintf("malformed oauth-constants.json at build time: %v", err))
	}
	// Self-check: the four fields the broker reads at runtime must be non-empty,
	// otherwise the broker would silently send a malformed refresh request.
	for k, v := range map[string]string{
		"endpoint":       Constants.Endpoint,
		"client_id":      Constants.ClientID,
		"version_header": Constants.VersionHeader,
		"scopes":         Constants.Scopes,
	} {
		if v == "" {
			panic("oauth-constants.json missing required field: " + k)
		}
	}
}
```

- [ ] **Step 3: Test + commit**

```bash
cd server && go test ./cmd/multica-claude-broker/ -run TestConstants -v 2>&1 | tail -5
git add server/cmd/multica-claude-broker/constants.go server/cmd/multica-claude-broker/constants_test.go
git commit -m "feat(broker): embed OAuth constants via go:embed"
```

---

## Task 4: Anthropic OAuth client — with classified errors and bounded retry

**Files:**
- Create: `server/cmd/multica-claude-broker/oauth_client.go`
- Create: `server/cmd/multica-claude-broker/oauth_client_test.go`

### Design notes

OAuth refresh is a **non-idempotent operation with side effects on the server side**. Anthropic rotates the refresh_token on each successful use, so a partial-success on our side (we got a 200 but our body-read failed) leaves the server with a fresh refresh_token that we never observed. We must therefore:

1. **Never retry past the point of a successful HTTP response.** If `Client.Do` returned a non-nil response, we are committed — read the body completely, parse it, and either succeed or fail definitively. Retrying after a 200 risks burning a fresh refresh_token a second time and getting `invalid_grant`.
2. **Retry only pre-response failures** — DNS, connection refused, TLS handshake error, request timeout *before* the server has acknowledged. Even those need a budget so we don't loop on a sustained outage.
3. **Classify HTTP responses correctly:**
   - **4xx** are non-retryable. `invalid_grant` means the refresh_token is dead and no number of retries will help — surface the error.
   - **429** is retryable with respect to the `Retry-After` header, but treated as non-retryable for our purposes (we don't have throttling pressure — Anthropic's rate limit on /v1/oauth/token is far above our cadence; a 429 here is a signal something is wrong, not transient load).
   - **5xx** is retryable. Anthropic's OAuth endpoint occasionally returns 502/503 during deploys.
4. **Backoff with jitter.** Exponential with full jitter (the AWS Architecture Blog pattern) — `sleep = rand[0, base * 2^attempt]`. Avoids retry-storms when many client_ids hit the endpoint at once (not us today, but a cheap correctness improvement).
5. **Context-respecting.** All sleeps are select-on-ctx so SIGTERM during a backoff doesn't delay shutdown.
6. **No external library.** Stdlib + a small `math/rand/v2` for jitter. We avoid `go-retryablehttp` and friends because we need precise control over the retry-classification boundary (point 1 above).

### Implementation

- [ ] **Step 1: Failing tests for each classification + happy path**

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// happy-path: correct request shape + body, single 200 response.
func TestRefreshToken_PostsCorrectShape(t *testing.T) {
	var gotBody string
	var gotVersion, gotCT, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("anthropic-version")
		gotCT      = r.Header.Get("Content-Type")
		gotUA      = r.Header.Get("User-Agent")
		b, _ := io.ReadAll(r.Body); gotBody = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ACCESS_NEW",
			"refresh_token": "REFRESH_ROTATED",
			"expires_in":    3600,
			"token_type":    "Bearer",
			"scope":         "user:inference",
		})
	}))
	defer srv.Close()

	c := newClientForTest(srv.URL, "9d1c250a-test", "oauth-2025-04-20")
	out, err := c.Refresh(context.Background(), "REFRESH_OLD")
	if err != nil { t.Fatalf("Refresh: %v", err) }
	if out.AccessToken != "ACCESS_NEW" || out.RefreshToken != "REFRESH_ROTATED" {
		t.Errorf("unexpected output: %+v", out)
	}
	if gotVersion != "oauth-2025-04-20" { t.Errorf("version header = %q", gotVersion) }
	if !strings.HasPrefix(gotCT, "application/json") { t.Errorf("content-type = %q", gotCT) }
	if !strings.HasPrefix(gotUA, "multica-claude-broker/") { t.Errorf("user-agent = %q", gotUA) }
	for _, fragment := range []string{`"grant_type":"refresh_token"`, `"refresh_token":"REFRESH_OLD"`, `"client_id":"9d1c250a-test"`} {
		if !strings.Contains(gotBody, fragment) {
			t.Errorf("body missing %s; got: %s", fragment, gotBody)
		}
	}
}

// 4xx is non-retryable — server is called exactly once.
func TestRefreshToken_4xxIsTerminal(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL, "x", "x")
	_, err := c.Refresh(context.Background(), "stale")
	var perm *PermanentError
	if !errors.As(err, &perm) {
		t.Errorf("expected PermanentError, got %T: %v", err, err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server called %d times; expected exactly 1 (no retries on 4xx)", got)
	}
}

// 5xx is retried up to MaxAttempts then surfaces as transient.
func TestRefreshToken_5xxRetriesThenFails(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "upstream blew up", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL, "x", "x")
	c.MaxAttempts = 3
	c.BackoffBase = 1 * time.Millisecond // keep tests fast
	_, err := c.Refresh(context.Background(), "fresh")
	if err == nil { t.Fatal("expected error") }
	var transient *TransientError
	if !errors.As(err, &transient) {
		t.Errorf("expected TransientError, got %T: %v", err, err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("server called %d times; expected exactly MaxAttempts (3)", got)
	}
}

// 5xx then 200 succeeds without surfacing the transient error.
func TestRefreshToken_5xxThenSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, "transient", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token":"A","refresh_token":"R","expires_in":3600})
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL, "x", "x"); c.BackoffBase = 1 * time.Millisecond
	out, err := c.Refresh(context.Background(), "fresh")
	if err != nil { t.Fatalf("Refresh after one retry: %v", err) }
	if out.AccessToken != "A" { t.Errorf("got %+v", out) }
}

// Body-read failure after 200 must NOT retry — refresh_token already rotated server-side.
func TestRefreshToken_NoRetryAfter2xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Length", "999999") // lie to force a read error
		w.WriteHeader(200)
		_, _ = w.Write([]byte("{"))
		// Connection closes here; client sees premature EOF
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL, "x", "x"); c.MaxAttempts = 5; c.BackoffBase = 1 * time.Millisecond
	_, err := c.Refresh(context.Background(), "fresh")
	if err == nil { t.Fatal("expected error") }
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server called %d times; must be exactly 1 (don't retry after 2xx)", got)
	}
}

// Context cancellation during backoff returns immediately.
func TestRefreshToken_ContextCancelsBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "x", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL, "x", "x")
	c.MaxAttempts = 10; c.BackoffBase = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	start := time.Now()
	_, err := c.Refresh(ctx, "x")
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Errorf("context cancellation did not abort backoff (took %v)", elapsed)
	}
	if err == nil { t.Fatal("expected error") }
}

// helper -----------------------------------------------------------------

func newClientForTest(endpoint, clientID, version string) *OAuthClient {
	return &OAuthClient{
		Endpoint:      endpoint,
		ClientID:      clientID,
		VersionHeader: version,
		UserAgent:     "multica-claude-broker/test",
		HTTP:          &http.Client{Timeout: 5 * time.Second},
		MaxAttempts:   2,
		BackoffBase:   1 * time.Millisecond,
	}
}
```

- [ ] **Step 2: Implement**

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"time"
)

// RefreshResult is the parsed response from Anthropic's /v1/oauth/token.
type RefreshResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// PermanentError indicates a 4xx response or a malformed-response failure
// AFTER we received a 2xx. The caller MUST NOT retry — either the server
// rejected our request as invalid (no amount of retry helps), or the server
// already rotated our refresh_token but we lost the new one. Surface this
// and let the operator intervene.
type PermanentError struct {
	StatusCode int
	Body       string
	Wrapped    error
}

func (e *PermanentError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("permanent OAuth error: %v", e.Wrapped)
	}
	return fmt.Sprintf("permanent OAuth error: HTTP %d: %s", e.StatusCode, e.Body)
}
func (e *PermanentError) Unwrap() error { return e.Wrapped }

// TransientError indicates a pre-response failure (network, DNS, 5xx). The
// caller's retry loop already exhausted MaxAttempts before returning this;
// downstream code treats this as "broker may be unable to serve a fresh
// token right now, keep serving cached if non-expired."
type TransientError struct {
	Attempts int
	LastErr  error
}

func (e *TransientError) Error() string {
	return fmt.Sprintf("transient OAuth error after %d attempts: %v", e.Attempts, e.LastErr)
}
func (e *TransientError) Unwrap() error { return e.LastErr }

// OAuthClient is a single-purpose HTTP client for Anthropic's /v1/oauth/token.
// Fields are exported so tests can override Endpoint/ClientID/etc. without
// mutating package-globals.
type OAuthClient struct {
	Endpoint      string
	ClientID      string
	VersionHeader string
	UserAgent     string

	HTTP        *http.Client
	MaxAttempts int           // total tries including the first; default 4
	BackoffBase time.Duration // first-retry backoff is jittered in [0, base]; default 500ms
}

// DefaultOAuthClient returns the broker's runtime client, wired to the embedded constants.
func DefaultOAuthClient() *OAuthClient {
	return &OAuthClient{
		Endpoint:      Constants.Endpoint,
		ClientID:      Constants.ClientID,
		VersionHeader: Constants.VersionHeader,
		UserAgent:     "multica-claude-broker/" + Constants.ClaudeVersion,
		HTTP:          &http.Client{Timeout: 30 * time.Second},
		MaxAttempts:   4,
		BackoffBase:   500 * time.Millisecond,
	}
}

// Refresh exchanges a refresh_token for a fresh access_token (+ rotated
// refresh_token). It retries transient pre-response failures with
// exponential-with-full-jitter backoff, never retries past a 2xx, and never
// retries on a 4xx. The return error is either *PermanentError or
// *TransientError; callers use errors.As to distinguish.
func (c *OAuthClient) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	if refreshToken == "" {
		return nil, &PermanentError{Wrapped: errors.New("refresh_token is empty")}
	}
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     c.ClientID,
	})

	maxAttempts := c.MaxAttempts
	if maxAttempts < 1 { maxAttempts = 1 }

	var lastTransient error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if err := sleepWithJitter(ctx, c.BackoffBase, attempt-1); err != nil {
				return nil, &TransientError{Attempts: attempt - 1, LastErr: err}
			}
		}

		result, kind, err := c.doOnce(ctx, body)
		switch kind {
		case outcomeOK:
			return result, nil
		case outcomePermanent:
			// 4xx, malformed 2xx, etc. — do not retry.
			var perm *PermanentError
			if errors.As(err, &perm) { return nil, perm }
			return nil, &PermanentError{Wrapped: err}
		case outcomeTransient:
			lastTransient = err
			continue
		}
	}
	return nil, &TransientError{Attempts: maxAttempts, LastErr: lastTransient}
}

type outcomeKind int

const (
	outcomeOK outcomeKind = iota
	outcomePermanent
	outcomeTransient
)

// doOnce performs exactly one POST. The classification rules:
//   - http.Client.Do error before response  → transient
//   - 2xx response, body-read or parse error → permanent (we lost the token)
//   - 2xx response, well-formed              → ok
//   - 4xx                                    → permanent (server says we're broken)
//   - 5xx, 429                               → transient
func (c *OAuthClient) doOnce(ctx context.Context, body []byte) (*RefreshResult, outcomeKind, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil { return nil, outcomePermanent, fmt.Errorf("build request: %w", err) }
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", c.VersionHeader)
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil { return nil, outcomeTransient, fmt.Errorf("http do: %w", err) }
	defer resp.Body.Close()

	// From here on the server has accepted the request. Any error means we lost
	// a possibly-rotated refresh_token. Classify as permanent.
	raw, readErr := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if readErr != nil {
			return nil, outcomePermanent, fmt.Errorf("read 2xx body: %w (refresh_token may have been rotated server-side)", readErr)
		}
		var out RefreshResult
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, outcomePermanent, fmt.Errorf("decode 2xx body: %w (raw: %s)", err, string(raw))
		}
		if out.AccessToken == "" {
			return nil, outcomePermanent, fmt.Errorf("2xx response missing access_token (raw: %s)", string(raw))
		}
		return &out, outcomeOK, nil

	case resp.StatusCode == 429 || (resp.StatusCode >= 500 && resp.StatusCode < 600):
		return nil, outcomeTransient, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))

	default: // 4xx (and any unexpected non-2xx, non-5xx)
		return nil, outcomePermanent, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
}

// sleepWithJitter sleeps for a random duration in [0, base * 2^attempt],
// capped at 30s. Respects ctx cancellation.
func sleepWithJitter(ctx context.Context, base time.Duration, attempt int) error {
	const cap = 30 * time.Second
	exp := base << attempt
	if exp <= 0 || exp > cap { exp = cap }
	d := time.Duration(rand.Int64N(int64(exp)))
	t := time.NewTimer(d); defer t.Stop()
	select {
	case <-ctx.Done(): return ctx.Err()
	case <-t.C:        return nil
	}
}
```

- [ ] **Step 3: Test + commit**

```bash
go test ./cmd/multica-claude-broker/ -run TestRefreshToken -v 2>&1 | tail -20
git add server/cmd/multica-claude-broker/oauth_client.go server/cmd/multica-claude-broker/oauth_client_test.go
git commit -m "feat(broker): Anthropic OAuth client with classified retry

Distinguishes PermanentError (4xx, or any failure after a 2xx response
where the refresh_token may have been rotated server-side) from
TransientError (pre-response 5xx/network). Exponential-with-full-jitter
backoff capped at 30s, ctx-respecting. Never retries past a 2xx — that
class of error means we already lost the rotated refresh_token and
retrying would burn the next one too."
```

### Downstream impact

The `Refresher` in Task 6 inspects the error type via `errors.As`:
- `*PermanentError` → set `multica_claude_broker_refresh_failures_total{reason="permanent"}`, do NOT schedule a faster retry; the operator must intervene.
- `*TransientError` → set `…{reason="transient"}`, retry on the next refresher tick.
- The cached access_token (if still non-expired) is served either way.

---

## Task 5: K8s Secret store (read/write OAuth state)

**Files:**
- Create: `server/cmd/multica-claude-broker/secret_store.go`
- Create: `server/cmd/multica-claude-broker/secret_store_test.go`

The broker persists `{access_token, refresh_token, expires_at}` as three keys in the `multica-claude-oauth-broker` Secret. JSON-blob alternative was considered and rejected — three keys are easier to inspect with `kubectl get secret ... -o jsonpath=...`.

- [ ] **Step 1: Failing test using `fake.NewSimpleClientset`**

```go
package main

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSecretStore_LoadStore(t *testing.T) {
	now := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "multica-claude-oauth-broker", Namespace: "multica"},
		Data: map[string][]byte{
			"access_token":  []byte("ACCESS_A"),
			"refresh_token": []byte("REFRESH_A"),
			"expires_at":    []byte(now.Format(time.RFC3339)),
		},
	}
	k := fake.NewSimpleClientset(existing)
	store := NewSecretStore(k, "multica", "multica-claude-oauth-broker")

	state, err := store.Load(context.Background())
	if err != nil { t.Fatalf("Load: %v", err) }
	if state.AccessToken != "ACCESS_A" || state.RefreshToken != "REFRESH_A" {
		t.Errorf("Load returned wrong state: %+v", state)
	}
	if !state.ExpiresAt.Equal(now) {
		t.Errorf("ExpiresAt = %v, want %v", state.ExpiresAt, now)
	}

	newState := &TokenState{AccessToken: "ACCESS_B", RefreshToken: "REFRESH_B", ExpiresAt: now.Add(time.Hour)}
	if err := store.Store(context.Background(), newState); err != nil { t.Fatalf("Store: %v", err) }

	roundtrip, _ := store.Load(context.Background())
	if roundtrip.AccessToken != "ACCESS_B" || roundtrip.RefreshToken != "REFRESH_B" {
		t.Errorf("Store/Load roundtrip failed: %+v", roundtrip)
	}
}
```

- [ ] **Step 2: Implement**

```go
package main

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type TokenState struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

type SecretStore struct {
	k         kubernetes.Interface
	namespace string
	name      string
}

func NewSecretStore(k kubernetes.Interface, namespace, name string) *SecretStore {
	return &SecretStore{k: k, namespace: namespace, name: name}
}

func (s *SecretStore) Load(ctx context.Context) (*TokenState, error) {
	sec, err := s.k.CoreV1().Secrets(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("secret %s/%s not found — bootstrap required", s.namespace, s.name)
		}
		return nil, fmt.Errorf("load secret: %w", err)
	}
	state := &TokenState{
		AccessToken:  string(sec.Data["access_token"]),
		RefreshToken: string(sec.Data["refresh_token"]),
	}
	if rawExp, ok := sec.Data["expires_at"]; ok && len(rawExp) > 0 {
		t, err := time.Parse(time.RFC3339, string(rawExp))
		if err != nil {
			return nil, fmt.Errorf("parse expires_at %q: %w", rawExp, err)
		}
		state.ExpiresAt = t
	}
	if state.RefreshToken == "" {
		return nil, fmt.Errorf("secret %s/%s missing refresh_token", s.namespace, s.name)
	}
	return state, nil
}

func (s *SecretStore) Store(ctx context.Context, state *TokenState) error {
	patch := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: s.name, Namespace: s.namespace},
		Data: map[string][]byte{
			"access_token":  []byte(state.AccessToken),
			"refresh_token": []byte(state.RefreshToken),
			"expires_at":    []byte(state.ExpiresAt.Format(time.RFC3339)),
		},
	}
	_, err := s.k.CoreV1().Secrets(s.namespace).Update(ctx, patch, metav1.UpdateOptions{})
	if err != nil && errors.IsNotFound(err) {
		_, err = s.k.CoreV1().Secrets(s.namespace).Create(ctx, patch, metav1.CreateOptions{})
	}
	if err != nil {
		return fmt.Errorf("store secret: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Test + commit**

```bash
go test ./cmd/multica-claude-broker/ -run TestSecretStore -v 2>&1 | tail -10
git add server/cmd/multica-claude-broker/secret_store.go server/cmd/multica-claude-broker/secret_store_test.go
git commit -m "feat(broker): K8s Secret-backed token state store"
```

---

## Task 6: Refresher with leader election (`client-go/tools/leaderelection`)

**Files:**
- Create: `server/cmd/multica-claude-broker/refresher.go`
- Create: `server/cmd/multica-claude-broker/refresher_test.go`
- Create: `server/cmd/multica-claude-broker/leader.go`
- Create: `server/cmd/multica-claude-broker/leader_test.go`

### Design notes

The refresher is the only code path that ever calls Anthropic's OAuth endpoint, and refresh is non-idempotent (Anthropic rotates the refresh_token on each successful use). If two broker pods refresh concurrently, exactly one wins; the other's "fresh" refresh_token is already invalidated server-side but neither pod knows it — the loser persists its now-invalid token back to the Secret and silently breaks the next refresh.

We need a primitive that says **"only one of us calls Anthropic at a time."** That primitive is leader election. `client-go/tools/leaderelection` is the right tool for the job. It:

- Holds a `coordination.k8s.io/Lease` for us — semantics identical to what we'd write by hand, but tested by the kubernetes-authors against clock skew, partial network partitions, stale RenewTime, and the dozen other edge cases hand-rolled leases get wrong.
- Provides clean **`OnStartedLeading`** and **`OnStoppedLeading`** callbacks. Lost-leadership is a fault we handle (stop accepting refresh requests, fail-closed) rather than a state we have to poll for.
- Provides **`IsLeader()`** for synchronous "am I allowed to refresh right now?" checks from the HTTP server.
- Supports **`ReleaseOnCancel`** so SIGTERM cleanly transfers leadership to the replacement pod instead of waiting for the lease to time out.

The chart still sets `replicas: 1` + `strategy: Recreate` — the lease is **defence in depth**, not the primary mechanism. Anyone accidentally running `kubectl scale --replicas=3` doesn't break correctness, just observes one writer and two read-only replicas. (Read-only replicas can serve `GET /access_token` from the cached state without holding the lease; that's a nice side effect.)

### Architecture

Two pieces:

1. **`LeaderState`** (leader.go) wraps `leaderelection.LeaderElector`. Exposes:
   - `Run(ctx)` — blocks until ctx cancels; runs the election loop.
   - `IsLeader() bool` — snapshot of current leadership state; safe to call from any goroutine.
   - `OnLeaderChange(callback func(isLeader bool))` — optional, for state-machine resets.

2. **`Refresher`** (refresher.go) consumes a `LeaderState`. Its `RefreshIfNeeded` method:
   - If `IsLeader()` is false → return `(false, cached, ErrNotLeader)`. Caller (HTTP server) serves the cached access_token if it's still valid; if it's expired, returns 503 with a clear reason.
   - If leader, proceeds with: load state, check expiry, call `OAuthClient.Refresh`, persist new state on success.
   - The leader gate is checked **once at entry**. We don't need to hold the lease across the Anthropic call — the lease itself is a 30s TTL that's renewed every 10s by the elector goroutine; under any sustained leadership the gate stays true throughout.

### Implementation

- [ ] **Step 1: Failing tests**

The leader test uses `fake.NewSimpleClientset` with a `coordination.k8s.io/v1.Lease` object. We exercise three scenarios:

```go
package main

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

// New broker pod becomes leader if no existing lease.
func TestLeaderState_AcquiresWhenNoLease(t *testing.T) {
	k := fake.NewSimpleClientset()
	ls, _ := NewLeaderState(k, "multica", "multica-claude-broker-refresh", "podA")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { ls.Run(ctx); close(done) }()
	waitFor(t, 100*time.Millisecond, func() bool { return ls.IsLeader() })
	<-done
}

// Two LeaderStates can't both be leader at once.
func TestLeaderState_MutualExclusion(t *testing.T) {
	k := fake.NewSimpleClientset()
	a, _ := NewLeaderState(k, "multica", "x", "podA")
	b, _ := NewLeaderState(k, "multica", "x", "podB")
	// (Implementation snippet using parallel Run goroutines and asserting
	//  that at most one IsLeader() is true at any instant.)
}

// Cancelling the leader's ctx hands leadership back to the lease pool.
func TestLeaderState_OnStoppedLeading(t *testing.T) {
	var lost atomic.Bool
	k := fake.NewSimpleClientset()
	ls, _ := NewLeaderState(k, "multica", "x", "podA")
	ls.OnStoppedLeading = func() { lost.Store(true) }
	// (Run, wait for lead, cancel, assert lost.Load() within timeout.)
}
```

And the refresher tests exercise the leader-or-not branches:

```go
// Non-leader path: cached token served, no Anthropic call.
func TestRefreshIfNeeded_NotLeader(t *testing.T) {
	// Stub LeaderState returning IsLeader()=false; stub OAuthClient that
	// fatally errors if Refresh is called. Assert (refreshed=false,
	// state=cached, err=ErrNotLeader).
}

// Leader, token still fresh: no Anthropic call.
func TestRefreshIfNeeded_LeaderButFresh(t *testing.T) { ... }

// Leader, expired: Anthropic call, secret updated.
func TestRefreshIfNeeded_LeaderRefreshes(t *testing.T) { ... }

// Leader, permanent error: cached state preserved, error propagates.
func TestRefreshIfNeeded_PermanentErrorPreservesState(t *testing.T) { ... }
```

- [ ] **Step 2: Implement `leader.go`**

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// LeaderState wraps client-go's leaderelection.LeaderElector with a tidy
// public surface: Run, IsLeader, optional OnStartedLeading / OnStoppedLeading
// callbacks.
type LeaderState struct {
	elector *leaderelection.LeaderElector

	leader atomic.Bool

	mu                 sync.RWMutex
	OnStartedLeading   func()
	OnStoppedLeading   func()
}

// NewLeaderState configures an elector against a Lease named `name` in
// namespace `ns`, with this pod's identity. Lease/renew/retry durations are
// the kubernetes-author recommended defaults for control-plane components
// (Lease 30s, renew 20s, retry 4s).
func NewLeaderState(k kubernetes.Interface, ns, name, identity string) (*LeaderState, error) {
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Client:    k.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: identity},
	}
	ls := &LeaderState{}
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:            lock,
		LeaseDuration:   30 * time.Second,
		RenewDeadline:   20 * time.Second,
		RetryPeriod:     4 * time.Second,
		ReleaseOnCancel: true, // SIGTERM → tidy handoff
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(context.Context) {
				ls.leader.Store(true)
				ls.mu.RLock()
				cb := ls.OnStartedLeading
				ls.mu.RUnlock()
				if cb != nil { cb() }
			},
			OnStoppedLeading: func() {
				ls.leader.Store(false)
				ls.mu.RLock()
				cb := ls.OnStoppedLeading
				ls.mu.RUnlock()
				if cb != nil { cb() }
			},
		},
		Name: "multica-claude-broker",
	})
	if err != nil { return nil, fmt.Errorf("build elector: %w", err) }
	ls.elector = elector
	return ls, nil
}

// Run blocks until ctx is cancelled. The election loop renews the lease
// while we hold it and bids for it when we don't.
func (l *LeaderState) Run(ctx context.Context) { l.elector.Run(ctx) }

// IsLeader is safe to call from any goroutine.
func (l *LeaderState) IsLeader() bool { return l.leader.Load() }
```

- [ ] **Step 3: Implement `refresher.go`**

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNotLeader is returned when a refresh was requested on a broker pod
// that isn't currently the leader. HTTP callers fall back to serving the
// cached access_token (if still valid).
var ErrNotLeader = errors.New("not the leader; refresh skipped")

type Refresher struct {
	store       *SecretStore
	leader      *LeaderState
	oauth       *OAuthClient
	refreshPad  time.Duration
}

func NewRefresher(store *SecretStore, leader *LeaderState, oauth *OAuthClient, refreshPad time.Duration) *Refresher {
	return &Refresher{store: store, leader: leader, oauth: oauth, refreshPad: refreshPad}
}

// RefreshIfNeeded loads the current state and, if we're the leader and the
// access_token is within RefreshPad of expiry, calls Anthropic and persists
// the new state. Returns (refreshed, current_state, err).
//
// Errors:
//   - ErrNotLeader when this pod doesn't hold the refresh lease (caller
//     serves cached state if non-expired).
//   - *PermanentError (from oauth_client.go) on 4xx or post-2xx failures —
//     operator must intervene; cached state preserved.
//   - *TransientError on exhausted retries against 5xx/network — caller
//     keeps serving cached, next tick tries again.
func (r *Refresher) RefreshIfNeeded(ctx context.Context) (bool, *TokenState, error) {
	state, err := r.store.Load(ctx)
	if err != nil { return false, nil, fmt.Errorf("load: %w", err) }

	// Still fresh enough — leadership doesn't matter for this branch.
	if !state.ExpiresAt.IsZero() && time.Until(state.ExpiresAt) > r.refreshPad {
		return false, state, nil
	}

	if !r.leader.IsLeader() {
		return false, state, ErrNotLeader
	}

	res, err := r.oauth.Refresh(ctx, state.RefreshToken)
	if err != nil {
		// Surface to caller; preserve cached state. The leader/transient/
		// permanent classification is preserved via errors.As.
		return false, state, err
	}

	newState := &TokenState{
		AccessToken:  res.AccessToken,
		RefreshToken: res.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(res.ExpiresIn) * time.Second),
	}
	if newState.RefreshToken == "" {
		newState.RefreshToken = state.RefreshToken // Anthropic occasionally omits the rotated value
	}
	if err := r.store.Store(ctx, newState); err != nil {
		return false, state, fmt.Errorf("persist new state: %w", err)
	}
	return true, newState, nil
}
```

- [ ] **Step 4: Wire callbacks for observability**

In `main.go` (Task 8), pass callbacks into `LeaderState` that bump Prometheus gauges:

```go
ls.OnStartedLeading = func() {
    logger.Info("became leader")
    leaderStateGauge.Set(1)
}
ls.OnStoppedLeading = func() {
    logger.Warn("lost leadership")
    leaderStateGauge.Set(0)
}
```

The `multica_claude_broker_leader{pod="..."}` gauge gives operators "which pod is the writer" at a glance. The HTTP server's `/healthz` stays 200 regardless of leadership (the pod is still serving cached tokens), and `/readyz` returns 200 only if it has loaded state at least once — leadership is not a readiness condition.

- [ ] **Step 5: Test + commit**

```bash
go test ./cmd/multica-claude-broker/ -v 2>&1 | tail -20
git add server/cmd/multica-claude-broker/leader.go server/cmd/multica-claude-broker/leader_test.go server/cmd/multica-claude-broker/refresher.go server/cmd/multica-claude-broker/refresher_test.go
git commit -m "feat(broker): leader-elected refresher

Uses k8s.io/client-go/tools/leaderelection to coordinate refresh among
broker pods. The chart pins replicas: 1, so leader election is defence
in depth — but it eliminates a class of correctness bugs (clock skew,
stale RenewTime, lost RELEASE during partition) that hand-rolled lease
code routinely gets wrong.

ReleaseOnCancel=true so SIGTERM cleanly hands leadership to the
replacement pod; the chart's Recreate strategy means there's a brief
no-writer window during rolls, which is fine — refreshes simply queue
on the next leader's first tick."
```

### What this leaves explicitly out of scope

- **Multi-region failover.** A single Lease in a single cluster is the entire correctness story. If you ever run brokers in multiple regions sharing the same Anthropic Max grant, you need a cross-cluster lock (e.g., DynamoDB conditional write). Not v1.
- **Standby-pod refresh prefetch.** Read-only replicas (under accidental scale-up) can serve cached tokens but don't proactively refresh. If you want hot-standby, you'd add a "leader-elected mutator vs. read-only follower" split. v1 just blocks reads-of-stale and lets the next leader take over.
- **Lease metrics.** `leaderelection.LeaderElector` exposes hooks; we wire only the boolean gauge in v1. If lease-acquisition latency ever matters operationally, the package emits events we can subscribe to later.

---

## Task 7: HTTP server + metrics

**Files:**
- Create: `server/cmd/multica-claude-broker/server.go`
- Create: `server/cmd/multica-claude-broker/server_test.go`
- Create: `server/cmd/multica-claude-broker/metrics.go`

API (three listeners):

```
Admin listener (cluster-reachable, :8080):
  GET  /access_token   → 200 text/plain, body is the bearer token (no quoting, no newline)
                         triggers a synchronous refresh if expires_at - now < RefreshPad
                         returns 503 if no valid token is available
  GET  /healthz        → 200 always-OK once the broker has finished startup
  GET  /readyz         → 200 only after the first successful Load+(refresh-if-needed)

Ops listener (loopback only, 127.0.0.1:8081):
  POST /refresh        → 200 on success, 5xx with Anthropic's error otherwise.
                         Bound to 127.0.0.1 so it's only reachable via `kubectl exec`,
                         not from other pods in the cluster. Forcing a refresh is a
                         privileged op (consumes Anthropic rate, can burn the
                         refresh_token if abused) — keep it operator-only.

Metrics listener (cluster-reachable, :9090):
  GET  /metrics        → Prometheus
```

Metrics:

```
multica_claude_broker_refresh_total{outcome="ok|error|skipped"}
multica_claude_broker_refresh_duration_seconds  histogram
multica_claude_broker_access_token_expires_at_seconds  gauge (unix time)
multica_claude_broker_access_token_requests_total{outcome="ok|error|stale"}
multica_claude_broker_constants_claude_version{version="2.1.148"}  gauge=1
```

- [ ] **Step 1: Failing tests**

Cover three scenarios:
- (a) `GET /access_token` returns the cached token verbatim when fresh.
- (b) `GET /access_token` triggers refresh-if-needed and returns the new token.
- (c) `POST /refresh` forces a refresh even when the cached token is fresh.

- [ ] **Step 2: Implement `server.go`**

(Sketch — the full implementation is straightforward stdlib HTTP plus calls into the Refresher and SecretStore.)

```go
// Admin mux: in-cluster traffic (workers + Prom-style readiness probes).
// /refresh is deliberately NOT here — it's on a separate loopback-only listener.
func NewAdminMux(broker *Broker) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", broker.healthHandler)
	mux.HandleFunc("/readyz", broker.readyHandler)
	mux.HandleFunc("/access_token", broker.accessTokenHandler)
	return mux
}

// Ops mux: only bound to 127.0.0.1 in main.go. Forcing a refresh is operator-only
// (kubectl exec ... -- curl http://127.0.0.1:8081/refresh).
func NewOpsMux(broker *Broker) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/refresh", broker.refreshHandler)
	return mux
}
```

`Broker` is a small struct holding `*Refresher`, `*SecretStore`, a sync.RWMutex around a cached `*TokenState`, an atomic `ready` boolean (flipped true after the first successful Load), and a logger. The cache is updated whenever a refresh completes (both background ticker and forced via `POST /refresh`).

```go
// Reload loads state from the secret into the cache — used at startup and
// callable from main without holding the refresh lease. Distinguishes
// "broker is up but hasn't seen state yet" from "broker is up and ready."
func (b *Broker) Reload(ctx context.Context) error {
	state, err := b.store.Load(ctx)
	if err != nil { return err }
	b.mu.Lock()
	b.cached = state
	b.mu.Unlock()
	b.ready.Store(true)
	return nil
}

// RunRefreshLoop ticks every cfg.RefreshInterval. On each tick it calls
// RefreshIfNeeded; the leader gate inside the Refresher means non-leader
// pods (or pre-leader-election state) are silently no-ops.
func (b *Broker) RunRefreshLoop(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval); defer t.Stop()
	for {
		select {
		case <-ctx.Done(): return
		case <-t.C:
			refreshed, state, err := b.refresher.RefreshIfNeeded(ctx)
			if err != nil && !errors.Is(err, ErrNotLeader) {
				b.logger.Warn("refresh tick failed", "error", err)
				// Don't update cache on error — preserve last good state.
				continue
			}
			if refreshed {
				b.mu.Lock(); b.cached = state; b.mu.Unlock()
				b.logger.Info("refresh ok", "expires_at", state.ExpiresAt)
			}
		}
	}
}
```

- [ ] **Step 3: Implement `metrics.go`** with the five metrics above. On startup, `Constants.ClaudeVersion` populates the `…constants_claude_version` gauge.

- [ ] **Step 4: Test + commit**

```bash
go test ./cmd/multica-claude-broker/ -v 2>&1 | tail -10
git add server/cmd/multica-claude-broker/server.go server/cmd/multica-claude-broker/server_test.go server/cmd/multica-claude-broker/metrics.go
git commit -m "feat(broker): HTTP API + Prometheus metrics"
```

---

## Task 8: main.go

**Files:**
- Create: `server/cmd/multica-claude-broker/main.go`

```go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("broker exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := LoadConfig()
	if err != nil { return err }

	restCfg, err := rest.InClusterConfig()
	if err != nil { return err }
	k, err := kubernetes.NewForConfig(restCfg)
	if err != nil { return err }

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Wire the dependency chain — signatures match Tasks 5/6/7 exactly.
	identity := mustHostnameOrRandom()
	store := NewSecretStore(k, cfg.Namespace, cfg.SecretName)
	leader, err := NewLeaderState(k, cfg.Namespace, cfg.LeaseName, identity)
	if err != nil { return err }
	leader.OnStartedLeading = func() { logger.Info("became leader"); leaderStateGauge.Set(1) }
	leader.OnStoppedLeading = func() { logger.Warn("lost leadership");  leaderStateGauge.Set(0) }
	oauth := DefaultOAuthClient()
	refresher := NewRefresher(store, leader, oauth, cfg.RefreshPad)
	broker := NewBroker(refresher, store, logger)

	// Bootstrap ordering matters — see plan-review #2.
	//   1. Load cached state BEFORE leader election. Reload is leadership-
	//      independent; it lets us serve cached /access_token immediately.
	//      If the Secret is missing, fail-closed and exit — the broker can't
	//      function without bootstrap state.
	if err := broker.Reload(ctx); err != nil {
		return fmt.Errorf("initial reload: %w (run the bootstrap procedure)", err)
	}
	//   2. Start leader election in a goroutine. It runs until ctx cancels;
	//      the elector calls back into OnStartedLeading once we win.
	go leader.Run(ctx)
	//   3. Start the refresh ticker. RefreshIfNeeded gates on IsLeader(), so
	//      until election settles each tick is a no-op (returns ErrNotLeader),
	//      which RunRefreshLoop swallows. Once we become leader, the next tick
	//      refreshes if the cached token is within RefreshPad of expiry.
	go broker.RunRefreshLoop(ctx, cfg.RefreshInterval)

	// Three listeners — see Task 7. Admin and metrics on all interfaces; ops on loopback.
	adminSrv   := &http.Server{Addr: cfg.AdminAddr,   Handler: NewAdminMux(broker),   ReadHeaderTimeout: 5 * time.Second}
	opsSrv     := &http.Server{Addr: cfg.OpsAddr,     Handler: NewOpsMux(broker),     ReadHeaderTimeout: 5 * time.Second}
	metricsSrv := &http.Server{Addr: cfg.MetricsAddr, Handler: NewMetricsMux(),       ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = adminSrv.ListenAndServe() }()
	go func() { _ = opsSrv.ListenAndServe() }()
	go func() { _ = metricsSrv.ListenAndServe() }()
	logger.Info("broker up", "admin", cfg.AdminAddr, "ops", cfg.OpsAddr, "metrics", cfg.MetricsAddr)

	<-ctx.Done()
	logger.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second); defer cancel()
	_ = adminSrv.Shutdown(sctx); _ = opsSrv.Shutdown(sctx); _ = metricsSrv.Shutdown(sctx)
	return nil
}

func mustHostnameOrRandom() string {
	if h, _ := os.Hostname(); h != "" { return h }
	var b [8]byte; _, _ = rand.Read(b[:]); return "broker-" + hex.EncodeToString(b[:])
}
```

Note Task 2's `Config` now also has `OpsAddr` (default `127.0.0.1:8081`) and `LeaseName` (already defined). Update `config.go` to expose those.

- [ ] **Build + test + commit**

```bash
cd server
go build -o /tmp/multica-claude-broker ./cmd/multica-claude-broker
/tmp/multica-claude-broker || true  # cleanly errors on missing config
go test ./... 2>&1 | tail -10
git add server/cmd/multica-claude-broker/main.go
git commit -m "feat(broker): main — load, refresh loop, HTTP server"
```

---

## Task 9: Dockerfile + build-images.sh

**Files:**
- Create: `packaging/docker/claude-broker/Dockerfile`
- Modify: `packaging/scripts/build-images.sh`

Distroless. We need `ca-certificates` for TLS to api.anthropic.com (distroless `static:nonroot` ships them).

```dockerfile
ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download
COPY server/ ./server/
# oauth-constants.json lives at server/cmd/multica-claude-broker/oauth-constants.json
# and is committed; the COPY above brings it in.
ARG VERSION=dev
ARG COMMIT=unknown
RUN cd server && CGO_ENABLED=0 go build \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/multica-claude-broker ./cmd/multica-claude-broker

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/multica-claude-broker /usr/local/bin/multica-claude-broker
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/multica-claude-broker"]
```

Add to `build-images.sh` IMAGES map:

```bash
[claude-broker]="packaging/docker/claude-broker/Dockerfile"
```

- [ ] **Build + commit**

```bash
docker build --platform linux/amd64 -f packaging/docker/claude-broker/Dockerfile -t multica-claude-broker:test .
git add packaging/docker/claude-broker/Dockerfile packaging/scripts/build-images.sh
git commit -m "feat(packaging): multica-claude-broker image + build-images.sh entry"
```

---

## Task 10: Helm — values + image helper

**Files:**
- Modify: `packaging/helm/multica/values.yaml`
- Modify: `packaging/helm/multica/templates/_helpers.tpl`

```yaml
  claudeBroker:
    enabled: true
    replicaCount: 1
    image: { name: multica-claude-broker, tag: "" }
    refreshPad: 5m
    refreshInterval: 60s
    secretName: multica-claude-oauth-broker     # the broker's own state secret
    bootstrapFromSecret: multica-claude-oauth   # one-time copy from the existing tarball
    apiKeyHelperTTLMs: 60000                    # claude re-calls helper every 60s
    resources:
      requests: { cpu: 50m, memory: 64Mi }
      limits:   { cpu: 200m, memory: 128Mi }
```

```tpl
{{- define "multica.claudeBrokerImage" -}}
{{- $img := .Values.runtime.claudeBroker.image -}}
{{- $tag := default .Values.image.tag $img.tag -}}
{{- printf "%s/%s:%s" .Values.image.registry $img.name $tag -}}
{{- end }}
```

- [ ] **Render + commit**

```bash
helm template multica packaging/helm/multica/ \
  --set hostname=multica.chrissnell.com \
  --set image.registry=ghcr.io/chrissnell --set image.tag=v0.4.0-mk1 2>&1 | head -10
git add packaging/helm/multica/values.yaml packaging/helm/multica/templates/_helpers.tpl
git commit -m "feat(helm): runtime.claudeBroker.* values + image helper"
```

---

## Task 11: Helm — RBAC (Secret + Lease access)

**Files:**
- Create: `packaging/helm/multica/templates/runtime/claude-broker-rbac.yaml`

Namespaced Role with minimal permissions:

```yaml
{{- if and .Values.runtime.enabled (eq .Values.runtime.mode "controller") .Values.runtime.claudeBroker.enabled }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: multica-claude-broker
  namespace: {{ .Release.Namespace }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: multica-claude-broker
  namespace: {{ .Release.Namespace }}
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    resourceNames: ["{{ .Values.runtime.claudeBroker.secretName }}", "{{ .Values.runtime.claudeBroker.bootstrapFromSecret }}"]
    verbs: ["get", "update", "create"]
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "create", "update", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: multica-claude-broker
  namespace: {{ .Release.Namespace }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: multica-claude-broker
subjects:
  - kind: ServiceAccount
    name: multica-claude-broker
    namespace: {{ .Release.Namespace }}
{{- end }}
```

- [ ] **Commit**

```bash
git add packaging/helm/multica/templates/runtime/claude-broker-rbac.yaml
git commit -m "feat(helm): claude-broker RBAC"
```

---

## Task 12: Helm — Deployment + Service + NetworkPolicy

**Files:**
- Create: `packaging/helm/multica/templates/runtime/claude-broker-deployment.yaml`
- Create: `packaging/helm/multica/templates/runtime/claude-broker-service.yaml`
- Create: `packaging/helm/multica/templates/runtime/claude-broker-networkpolicy.yaml`

Deployment uses `replicas: 1` + `strategy: Recreate` to enforce single-writer to the Secret. Lease in Task 6 is defence in depth in case someone scales it accidentally. PodSecurity `restricted`-compatible.

(Template body follows the Plan E controller-deployment pattern — env from secrets+downward-API, restricted security context, readiness probe on `/readyz`.)

Service:

```yaml
{{- if and .Values.runtime.enabled (eq .Values.runtime.mode "controller") .Values.runtime.claudeBroker.enabled }}
apiVersion: v1
kind: Service
metadata:
  name: multica-claude-broker
  namespace: {{ .Release.Namespace }}
spec:
  type: ClusterIP
  selector:
    {{- include "multica.componentSelector" (dict "name" "claude-broker" "ctx" .) | nindent 4 }}
  ports:
    - { name: admin,   port: 8080, targetPort: admin }
    - { name: metrics, port: 9090, targetPort: metrics }
{{- end }}
```

NetworkPolicy (plan-review #3 fix). The broker serves bearer tokens over plain HTTP in-cluster; without a NetworkPolicy any pod in the namespace can `curl /access_token` and walk away with an Anthropic Max token. Restrict ingress to controller-managed worker pods + the Prom scraper:

```yaml
{{- if and .Values.runtime.enabled (eq .Values.runtime.mode "controller") .Values.runtime.claudeBroker.enabled .Values.runtime.claudeBroker.networkPolicy.enabled }}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: multica-claude-broker
  namespace: {{ .Release.Namespace }}
spec:
  podSelector:
    matchLabels:
      {{- include "multica.componentSelector" (dict "name" "claude-broker" "ctx" .) | nindent 6 }}
  policyTypes: ["Ingress"]
  ingress:
    # Admin port (:8080) — only worker Job pods managed by the controller.
    - from:
        - podSelector:
            matchLabels:
              app.kubernetes.io/managed-by: multica-k8s-controller
      ports:
        - { protocol: TCP, port: 8080 }
    # Metrics port (:9090) — Prom scraper namespace (configurable).
    {{- with .Values.runtime.claudeBroker.networkPolicy.metricsFrom }}
    - from:
        {{- toYaml . | nindent 8 }}
      ports:
        - { protocol: TCP, port: 9090 }
    {{- end }}
    # Note: ops port (:8081) is bound to 127.0.0.1 by the broker itself,
    # so no NetworkPolicy rule is needed — pod-network traffic can't reach it.
{{- end }}
```

Add to `values.yaml`:

```yaml
  claudeBroker:
    # ...existing fields...
    networkPolicy:
      enabled: true
      metricsFrom: []   # e.g. [{namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: monitoring}}}]
```

- [ ] **Render + commit**

```bash
helm template ... -s templates/runtime/claude-broker-deployment.yaml
helm template ... -s templates/runtime/claude-broker-service.yaml
helm template ... -s templates/runtime/claude-broker-networkpolicy.yaml
git add packaging/helm/multica/templates/runtime/claude-broker-deployment.yaml \
        packaging/helm/multica/templates/runtime/claude-broker-service.yaml \
        packaging/helm/multica/templates/runtime/claude-broker-networkpolicy.yaml \
        packaging/helm/multica/values.yaml
git commit -m "feat(helm): claude-broker Deployment + Service + NetworkPolicy"
```

---

## Task 13: Helm — client-config ConfigMap (`apiKeyHelper` script + settings.json)

**Files:**
- Create: `packaging/helm/multica/templates/runtime/claude-broker-clientconfig.yaml`

This ConfigMap is what worker Job pods mount instead of (and in place of) the old `multica-claude-oauth` Secret expansion. It contains:

- `apiKeyHelper.sh` — `#!/bin/sh\nexec curl -sf http://multica-claude-broker.multica.svc:8080/access_token`
- `settings.json` — `{ "apiKeyHelper": "/etc/claude-broker/apiKeyHelper.sh", "hasTrustDialogAccepted": true }`

```yaml
{{- if and .Values.runtime.enabled (eq .Values.runtime.mode "controller") .Values.runtime.claudeBroker.enabled }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: multica-claude-broker-client
  namespace: {{ .Release.Namespace }}
data:
  apiKeyHelper.sh: |
    #!/bin/sh
    exec curl -sf http://multica-claude-broker.{{ .Release.Namespace }}.svc:8080/access_token
  settings.json: |
    {
      "apiKeyHelper": "/etc/claude-broker/apiKeyHelper.sh",
      "hasTrustDialogAccepted": true
    }
{{- end }}
```

- [ ] **Commit**

```bash
git add packaging/helm/multica/templates/runtime/claude-broker-clientconfig.yaml
git commit -m "feat(helm): claude-broker client config (apiKeyHelper script + settings.json)"
```

---

## Task 14: Controller — wire apiKeyHelper into worker Jobs

**Files:**
- Modify: `server/cmd/multica-k8s-controller/jobs.go`
- Modify: `server/cmd/multica-k8s-controller/jobs_test.go`
- Modify: `server/cmd/multica-k8s-controller/config.go`
- Modify: `server/cmd/multica-k8s-controller/main.go`

When `claudeBroker.enabled`, the controller's `DispatchJob` changes the worker Job spec as follows:

1. **Skip** the existing `claude-oauth-secret` volume + `claude-auth` init container — the broker owns auth.
2. **Mount** the new `multica-claude-broker-client` ConfigMap at `/etc/claude-broker/`, with `apiKeyHelper.sh` exec-permissioned.
3. **Mount** the `settings.json` from the same ConfigMap at `/home/multica/.claude/settings.json` (subPath).
4. **Set env** `CLAUDE_CODE_API_KEY_HELPER_TTL_MS=<cfg>`. This caps the cache age inside claude so the broker can rotate tokens at most every minute.

- [ ] **Step 1: Add `ClaudeBroker` fields to Config**

```go
type ClaudeBrokerOptions struct {
	Enabled        bool
	ClientCMName   string // default "multica-claude-broker-client"
	HelperTTLMs    int    // default 60000
}
```

- [ ] **Step 2: Plumb through `DispatchJob`**

`DispatchJob` gains a `cb ClaudeBrokerOptions` parameter (or extend `JobOptions` if Plan F.1 has already merged that pattern). When `cb.Enabled`:

```go
// Replace claude-auth init container path
if !cb.Enabled {
    // existing claude-oauth path...
}
// Always-on additions for broker mode:
container.VolumeMounts = append(container.VolumeMounts,
    corev1.VolumeMount{Name: "claude-broker-client", MountPath: "/etc/claude-broker", ReadOnly: true},
    corev1.VolumeMount{Name: "claude-broker-settings", MountPath: "/home/multica/.claude/settings.json", SubPath: "settings.json", ReadOnly: true},
)
container.Env = append(container.Env, corev1.EnvVar{
    Name:  "CLAUDE_CODE_API_KEY_HELPER_TTL_MS",
    Value: strconv.Itoa(cb.HelperTTLMs),
})
volumes = append(volumes,
    corev1.Volume{Name: "claude-broker-client", VolumeSource: corev1.VolumeSource{
        ConfigMap: &corev1.ConfigMapVolumeSource{
            LocalObjectReference: corev1.LocalObjectReference{Name: cb.ClientCMName},
            DefaultMode: &execMode, // 0o755 so the .sh script is executable
        },
    }},
    // For the settings.json subPath mount we reuse the same CM
    corev1.Volume{Name: "claude-broker-settings", VolumeSource: corev1.VolumeSource{
        ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: cb.ClientCMName}},
    }},
)
```

- [ ] **Step 3: Tests**

`TestDispatchJob_WithClaudeBroker` asserts:
- The settings.json subPath mount is present.
- The `apiKeyHelper.sh` ConfigMap mount is present at `/etc/claude-broker`.
- `CLAUDE_CODE_API_KEY_HELPER_TTL_MS` env is set.
- The legacy `claude-oauth-secret` volume and `claude-auth` init container are **absent**.

- [ ] **Step 4: Wire from controller-configmap into Config**

Extend `runtime.yaml` (rendered by `controller-configmap.yaml`) with a `claudeBroker:` block; the controller's `LoadConfig` populates `ClaudeBrokerOptions`.

- [ ] **Step 5: Test + commit**

```bash
go test ./cmd/multica-k8s-controller/ -v 2>&1 | tail -10
git add server/cmd/multica-k8s-controller/ packaging/helm/multica/templates/runtime/controller-configmap.yaml
git commit -m "feat(controller): wire apiKeyHelper from claude-broker into worker Jobs"
```

---

## Task 15: Bootstrap procedure — one-time copy from `multica-claude-oauth` to `multica-claude-oauth-broker`

**Files:** none (operator-run procedure documented here)

The broker reads from `multica-claude-oauth-broker` (three keys: `access_token`, `refresh_token`, `expires_at`). The cluster currently has a `multica-claude-oauth` Secret containing `claude-auth.tgz` (a tarball of `.credentials.json + settings.json`). We need a one-time conversion.

- [ ] **Step 1: Extract the tarball locally**

```bash
kubectl -n multica get secret multica-claude-oauth -o jsonpath='{.data.claude-auth\.tgz}' \
  | base64 -d \
  | tar xzf - -C /tmp/

# yields /tmp/.claude/.credentials.json
```

- [ ] **Step 2: Parse the credentials.json and create the broker Secret**

```bash
# .credentials.json schema (verified by inspecting an active one):
#   { "claudeAiOauth": {
#       "accessToken": "...",
#       "refreshToken": "...",
#       "expiresAt": 1735000000000     // millis since epoch
#     } }
ACCESS=$(jq -r .claudeAiOauth.accessToken  /tmp/.claude/.credentials.json)
REFRESH=$(jq -r .claudeAiOauth.refreshToken /tmp/.claude/.credentials.json)
EXP_MS=$(jq -r .claudeAiOauth.expiresAt    /tmp/.claude/.credentials.json)
EXP_AT=$(date -u -r $((EXP_MS / 1000)) +%FT%TZ)

kubectl -n multica create secret generic multica-claude-oauth-broker \
  --from-literal=access_token="$ACCESS" \
  --from-literal=refresh_token="$REFRESH" \
  --from-literal=expires_at="$EXP_AT"

rm -rf /tmp/.claude
```

- [ ] **Step 3: Document in `packaging/README.md`** (covered in Task 18).

---

## Task 16: Push images + deploy

```bash
./packaging/scripts/build-images.sh --tag "$TAG" claude-broker controller
```

In `~/kube/apps/multica/values.yaml`:

```yaml
runtime:
  claudeBroker:
    enabled: true
```

Run the bootstrap from Task 15, then:

```bash
helm upgrade --install multica packaging/helm/multica/ -n multica -f ~/kube/apps/multica/values.yaml
kubectl -n multica rollout status deploy/multica-claude-broker --timeout=120s
kubectl -n multica rollout status deploy/multica-controller    --timeout=120s
```

- [ ] **Step 1: Verify broker is healthy**

```bash
kubectl -n multica logs deploy/multica-claude-broker --tail=20
kubectl -n multica exec deploy/multica-claude-broker -- /usr/local/bin/multica-claude-broker -h 2>&1 | head -5
# Direct curl from within the cluster:
kubectl -n multica run curl-test --rm -i --restart=Never --image=curlimages/curl -- \
  curl -sf http://multica-claude-broker:8080/healthz
```

- [ ] **Step 2: Verify `/access_token` returns a real token**

```bash
kubectl -n multica run curl-test --rm -i --restart=Never --image=curlimages/curl -- \
  sh -c 'curl -sf http://multica-claude-broker:8080/access_token | head -c 50; echo "..."'
```

Expected: prints the first 50 chars of an `sk-…` (or whatever Anthropic's bearer format is) token, then `…`. The full token never gets dumped to the transcript.

- [ ] **Step 3: Force-refresh via `POST /refresh` and confirm metric ticks**

```bash
kubectl -n multica run curl-test --rm -i --restart=Never --image=curlimages/curl -- \
  curl -sf -X POST http://multica-claude-broker:8080/refresh
kubectl -n multica port-forward deploy/multica-claude-broker 9090:9090 &
sleep 2
curl -s http://localhost:9090/metrics | grep multica_claude_broker_refresh_total
kill %1
```

Expected: `multica_claude_broker_refresh_total{outcome="ok"} >= 2` (initial + forced).

---

## Task 17: E2E — assign a task and confirm broker-backed auth

- [ ] **Step 1: Assign a real task in the web UI.**

- [ ] **Step 2: Watch the worker pod log for the `claude` invocation.**

```bash
kubectl -n multica logs -l app.kubernetes.io/managed-by=multica-k8s-controller -c runtask --follow
```

Expected: no `401 Invalid authentication credentials` line. The task completes with `claude finished … status=completed`. Compare to the failure baseline from Plan E debugging.

- [ ] **Step 3: Inspect the worker pod's settings.json**

```bash
POD=$(kubectl -n multica get pods -l app.kubernetes.io/managed-by=multica-k8s-controller --field-selector=status.phase=Running -o name | head -1)
kubectl -n multica exec "$POD" -c runtask -- cat /home/multica/.claude/settings.json
kubectl -n multica exec "$POD" -c runtask -- cat /etc/claude-broker/apiKeyHelper.sh
```

Expected: `apiKeyHelper` set to `/etc/claude-broker/apiKeyHelper.sh`; the script is the one-line `curl`.

- [ ] **Step 4: Confirm there is no `.credentials.json` in the pod's `.claude`** (the broker-mode pod doesn't need it).

```bash
kubectl -n multica exec "$POD" -c runtask -- ls /home/multica/.claude/
```

Expected: shows `settings.json` and any per-task state claude wrote, **not** `.credentials.json`.

---

## Task 18: Operator docs

**Files:**
- Modify: `packaging/README.md`

Add a section explaining:
- What the broker is (single source of truth for OAuth state, eliminates the concurrent-refresh race).
- The bootstrap procedure (Task 15) including the one-time conversion command.
- The new Secret `multica-claude-oauth-broker` and what's in it.
- How to force a refresh (`curl -X POST broker:8080/refresh`).
- How to rotate the underlying OAuth grant if Anthropic ever revokes it (run `claude /login` locally, re-run the bootstrap procedure, restart the broker — total downtime ~30s).
- How `oauth-constants.json` works and when it needs updating (see Plan F.2-watcher).
- The `multica_claude_broker_refresh_failures_total{reason="invalid_grant"}` alert recipe.

- [ ] **Commit**

```bash
git add packaging/README.md
git commit -m "docs(packaging): claude-broker operator guide"
```

---

## Task 19: Final regression

- [ ] `cd server && go vet ./... && go test ./...` clean.
- [ ] `helm lint packaging/helm/multica/` clean for both modes (`claudeBroker.enabled=true` and `=false`).
- [ ] Cluster state matches expected end state: `multica-claude-broker` Deployment, Service, RBAC, two ConfigMaps (server-side + client-side), `multica-claude-oauth-broker` Secret.
- [ ] Disable the broker (`runtime.claudeBroker.enabled=false`), helm upgrade, confirm worker pods fall back to the legacy `claude-auth-secret` expansion path. Defensive sanity that we didn't strand operators on the new path.

---

## What's next

- **`2026-05-29-claude-version-watcher.md`** — daily CI watch + auto-PR to bump the runtime image's claude version and re-extract `oauth-constants.json`. Recommended companion to this plan.
- **Tool cache + per-issue PVC GC (`2026-05-29-tool-cache-and-gc.md`)** — already drafted; orthogonal to the broker.
- **Repo cache (`2026-05-29-multica-repocache.md`)** — already drafted; orthogonal to the broker.
- **Failover broker pair** — out of scope for v1. If broker outages become a real problem, the natural next step is `replicas: 2` with leader-election via the same Lease the refresher already uses. Today, `replicas: 1` + `strategy: Recreate` is fine — broker downtime affects only the worker pods that start during the gap, and they fail in a way that's caught by the controller's failure sweep (Plan E Task 7).

## What this enables

- The cluster never silently goes dark from expired OAuth tokens. The broker either refreshes successfully (zero billed cost) or alerts via metrics.
- Operator's manual-refresh path is preserved as a fallback but rarely needed (only if Anthropic revokes the underlying grant).
- The OAuth refresh shape is anchored to the claude binary's own constants via a deterministic extractor — when claude updates, the watcher (next plan) opens a PR with the new constants and the broker rebuilds at the same tag as the runtime image. No drift possible at release time; runtime drift is observable via the `claude_version_info` gauge.
