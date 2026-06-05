package agenttmpl

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

// Embed the whole templates/ directory (not templates/*.json) so the build
// still compiles when the catalog is empty: a *.json glob with zero matches is
// a compile error, whereas the directory always contains templates/README.md.
// loadFromFS only reads *.json, so the README is ignored at load time.
//
//go:embed templates
var templateFS embed.FS

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Registry is the in-memory store of loaded templates. It's read-only after
// construction — the only mutator is Load(), called once at server startup.
// Concurrent reads after that are safe without locking.
type Registry struct {
	bySlug map[string]Template
	order  []string // slugs in deterministic load order, used by List()
}

// Load parses every *.json file under templates/ and returns the Registry.
// An empty catalog (no *.json files) is valid and yields an empty Registry.
// Any malformed template (bad JSON, missing required fields, slug/filename
// mismatch) aborts startup — we'd rather fail loudly at boot than serve a
// half-broken picker.
func Load() (*Registry, error) {
	return loadFromFS(templateFS, "templates")
}

// LoadFromFS builds a Registry from an arbitrary filesystem, applying the same
// parsing and validation as Load. It exists so packages outside agenttmpl
// (notably handler tests) can stand up a Registry from an in-memory fstest.MapFS
// instead of the embedded catalog, exercising the create-from-template flow
// against controlled fixtures.
func LoadFromFS(fsys fs.FS, dir string) (*Registry, error) {
	return loadFromFS(fsys, dir)
}

func loadFromFS(fsys fs.FS, dir string) (*Registry, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("agenttmpl: read templates dir: %w", err)
	}

	reg := &Registry{bySlug: make(map[string]Template)}

	// Sort filenames so List() output is deterministic regardless of FS order.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		path := dir + "/" + name
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, fmt.Errorf("agenttmpl: read %s: %w", path, err)
		}

		var t Template
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("agenttmpl: parse %s: %w", path, err)
		}

		if err := validate(t, name); err != nil {
			return nil, fmt.Errorf("agenttmpl: %s: %w", path, err)
		}

		if _, dup := reg.bySlug[t.Slug]; dup {
			return nil, fmt.Errorf("agenttmpl: duplicate slug %q (file %s)", t.Slug, path)
		}

		reg.bySlug[t.Slug] = t
		reg.order = append(reg.order, t.Slug)
	}

	return reg, nil
}

// validate enforces the invariants that the rest of the handler / UI assume.
// Cheap to run at boot — every check pays for itself the first time someone
// adds a malformed template in a PR.
func validate(t Template, filename string) error {
	if t.Slug == "" {
		return fmt.Errorf("missing slug")
	}
	if !slugPattern.MatchString(t.Slug) {
		return fmt.Errorf("slug %q must be lowercase kebab-case (a-z, 0-9, -)", t.Slug)
	}
	// Slug must equal the filename basename so URL routing matches file
	// layout. Catches typos and lets `git mv` rename templates safely.
	if filename != t.Slug+".json" {
		return fmt.Errorf("slug %q does not match filename %q", t.Slug, filename)
	}
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("missing name")
	}
	if strings.TrimSpace(t.Instructions) == "" {
		return fmt.Errorf("missing instructions")
	}
	// 0-skill templates are legitimate — most starter templates are
	// prompt-only (instructions alone, no skill fan-out). See
	// docs/agent-quick-create-plan.md and the picker UI's "Prompt only"
	// rendering for zero-length skill arrays.
	for i, s := range t.Skills {
		if strings.TrimSpace(s.SourceURL) == "" {
			return fmt.Errorf("skill[%d]: missing source_url", i)
		}
	}
	return nil
}

// List returns all templates in deterministic load order.
func (r *Registry) List() []Template {
	out := make([]Template, 0, len(r.order))
	for _, slug := range r.order {
		out = append(out, r.bySlug[slug])
	}
	return out
}

// Get returns the template with the given slug, or false if not found.
func (r *Registry) Get(slug string) (Template, bool) {
	t, ok := r.bySlug[slug]
	return t, ok
}
