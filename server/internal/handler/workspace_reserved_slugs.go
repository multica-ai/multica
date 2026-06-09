package handler

import (
	_ "embed"
	"encoding/json"
)

// reservedSlugs are workspace slugs that would collide with frontend top-level
// routes, platform features, or web standards. The frontend URL shape is
// /{workspaceSlug}/... so any slug that matches a top-level route or a
// system-significant name is rejected at workspace creation time.
//
// The list is loaded from reserved_slugs.json (embedded at build time), which
// is the single source of truth shared with the TypeScript side. Edit only
// the JSON; packages/core/paths/reserved-slugs.ts is regenerated from it by
// `pnpm generate:reserved-slugs` and CI fails on any drift.

//go:embed reserved_slugs.json
var reservedSlugsJSON []byte

var reservedSlugs = loadReservedSlugs()

type reservedSlugsFile struct {
	Groups []struct {
		Slugs []string `json:"slugs"`
	} `json:"groups"`
}

func loadReservedSlugs() map[string]bool {
	var data reservedSlugsFile
	if err := json.Unmarshal(reservedSlugsJSON, &data); err != nil {
		// reserved_slugs.json is checked into the repo and embedded into the
		// binary; a parse failure is a programming error caught at the very
		// first request that touches workspace creation, which is too late.
		// Panic at init so the binary refuses to start instead.
		panic("handler: parse reserved_slugs.json: " + err.Error())
	}
	out := make(map[string]bool)
	for _, g := range data.Groups {
		for _, slug := range g.Slugs {
			out[slug] = true
		}
	}
	return out
}

func isReservedSlug(slug string) bool {
	return reservedSlugs[slug]
}
