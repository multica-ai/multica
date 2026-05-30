package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed oauth-constants.json
var oauthConstantsJSON []byte

// OAuthConstants is the subset of oauth-constants.json that the broker reads
// at runtime. The companion extractor (server/cmd/extract-oauth-constants/)
// writes the canonical JSON here; the claude-version-watcher CI workflow
// re-runs the extractor whenever Claude Code publishes a new version.
type OAuthConstants struct {
	Endpoint      string `json:"endpoint"`
	ClientID      string `json:"client_id"`
	VersionHeader string `json:"version_header"`
	Scopes        string `json:"scopes"`
	ClaudeVersion string `json:"claude_version"`
	ExtractedAt   string `json:"extracted_at"`
}

// Constants is populated at init() from the embedded JSON and is read-only
// for the lifetime of the process. A malformed or incomplete file panics
// at startup — better to fail loudly than send a malformed refresh request.
var Constants OAuthConstants

// rawOAuthFile mirrors the on-disk shape (top-level fields + _meta block).
// We flatten it into Constants at init.
type rawOAuthFile struct {
	OAuthConstants
	Meta struct {
		ClaudeVersion string `json:"claude_version"`
		ExtractedAt   string `json:"extracted_at"`
	} `json:"_meta"`
}

func init() {
	var raw rawOAuthFile
	if err := json.Unmarshal(oauthConstantsJSON, &raw); err != nil {
		panic(fmt.Sprintf("malformed oauth-constants.json at build time: %v", err))
	}
	Constants = raw.OAuthConstants
	// Provenance fields live under _meta in the file; copy them up so callers
	// don't have to know about the file layout.
	if Constants.ClaudeVersion == "" {
		Constants.ClaudeVersion = raw.Meta.ClaudeVersion
	}
	if Constants.ExtractedAt == "" {
		Constants.ExtractedAt = raw.Meta.ExtractedAt
	}
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
