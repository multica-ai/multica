// Package agenttmpl loads and serves the curated agent templates that power
// the "Create Agent from template" flow. Templates are static JSON files
// embedded at build time (see loader.go); they pair a hand-written
// instructions block with a list of skill references (GitHub URLs) that get
// materialised into the workspace when a user picks the template.
//
// Templates are intentionally repo-only: their content is part of the product
// (the "curated best-practice combos") and changes go through normal PR
// review. No runtime mutation, no admin UI. If/when that changes, swap the
// in-memory map for a DB-backed Registry without touching callers.
package agenttmpl

// Template is the structured representation of an `agent template` JSON file
// loaded from server/internal/agenttmpl/templates/<slug>.json.
type Template struct {
	// Slug uniquely identifies a template within the catalog. Must equal the
	// JSON file's basename so URLs like /api/agent-templates/{slug} resolve
	// deterministically. Allowed characters: lowercase letters, digits, "-".
	Slug string `json:"slug"`

	// Name is the human-readable title shown in the picker grid.
	Name string `json:"name"`

	// Description is a one-line summary for the picker card.
	Description string `json:"description"`

	// Category groups templates in the picker UI ("Engineering" / "Writing" /
	// "Building" / ...). Empty allowed (renders in an "Other" group); when set
	// the picker shows a section header.
	Category string `json:"category,omitempty"`

	// Icon is a lucide-react icon name (e.g. "Search", "Palette"). Rendered
	// in the picker card + detail header as the visual differentiator. Empty
	// falls back to a generic "FileText" icon on the frontend.
	Icon string `json:"icon,omitempty"`

	// Accent picks the semantic color token used to tint the icon badge:
	// one of "info" / "success" / "warning" / "primary" / "secondary".
	// Empty falls back to "muted" on the frontend. Hardcoded color values
	// (text-red-500, bg-blue-100, …) are explicitly NOT allowed — accent
	// must be a Multica design-system token name (see CLAUDE.md).
	Accent string `json:"accent,omitempty"`

	// Instructions is the verbatim text written into the created agent's
	// `agent.instructions` column. Keep it plain markdown — the runtime
	// receives it as-is.
	Instructions string `json:"instructions"`

	// Skills lists the skill references that should be materialised into the
	// workspace when the template is picked. Order is preserved in responses
	// so the UI can show skills in a stable sequence.
	Skills []TemplateSkillRef `json:"skills"`
}

// TemplateSkillRef points to one skill that should be imported when the
// template is materialised. SourceURL is the only fetched field; CachedName
// and CachedDescription let the picker render the skill name without making
// an HTTP round-trip per template per page load.
type TemplateSkillRef struct {
	// SourceURL is the upstream skill location. Resolved at materialisation
	// time via handler/skill.go:detectImportSource (skills.sh, github.com, …)
	// so any URL the existing skill importer already accepts works here.
	SourceURL string `json:"source_url"`

	// CachedName mirrors the upstream SKILL.md frontmatter `name` field at
	// the time the template was authored. Used for picker rendering only —
	// the actual skill row uses whatever the fetched frontmatter says.
	CachedName string `json:"cached_name"`

	// CachedDescription mirrors the upstream frontmatter `description`.
	// Same role as CachedName.
	CachedDescription string `json:"cached_description"`
}
