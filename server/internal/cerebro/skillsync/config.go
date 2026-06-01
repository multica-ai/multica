// Package skillsync mirrors approved Multica skills (the Postgres source of
// truth) into the firtal-skills GitHub archive every night (FIR-2656).
//
// Multica is the truth for skills; firtal-skills is the version archive. The
// skill table always holds the merged/approved content — open change requests
// live in skill_change_request and never touch skill.content — so reading the
// skill table directly yields exactly "the approved versions, no open drafts".
// The worker writes each skill to <domain>/<slug>/SKILL.md (+ supporting
// files), commits only when something changed, and pushes. The repo's git
// history is the version archive; the existing regenerate-catalog CI rebuilds
// catalog.json from the pushed frontmatter.
package skillsync

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// archiveDomains is the fixed set of top-level domain folders firtal-skills'
// catalog generator scans. A skill's target folder is its frontmatter
// `domain:` when it names one of these; otherwise it lands in DefaultDomain.
// Keep this in sync with scripts/gen_catalog.py in firtal-skills.
var archiveDomains = map[string]bool{
	"data":         true,
	"sales":        true,
	"marketing":    true,
	"customer":     true,
	"product":      true,
	"organisation": true,
	"engineering":  true,
	"design":       true,
	"workflows":    true,
}

// Config is the env-driven configuration for the nightly sync worker. The
// worker is disabled (Enabled=false) unless both Repo and Token are set, so a
// self-host install that never configures the archive simply never runs it.
type Config struct {
	Enabled bool

	// Repo is the https git URL of the archive, e.g.
	// https://github.com/firtal-group/firtal-skills.git
	Repo string
	// Token is a GitHub token with write access to Repo. Injected into the
	// remote URL as x-access-token; never logged.
	Token string
	// Branch is the branch to push to. Default "main".
	Branch string

	// Interval is the sync cadence. Default 24h (nightly).
	Interval time.Duration
	// WorkDir is the persistent checkout location reused across ticks.
	WorkDir string

	// DefaultDomain is where a skill lands when its frontmatter carries no
	// recognised domain and it is not already present in the archive.
	DefaultDomain string

	// AuthorName / AuthorEmail stamp the sync commits.
	AuthorName  string
	AuthorEmail string
}

// LoadConfig reads CEREBRO_SKILLS_SYNC_* from the environment. Returns
// Config{Enabled:false} when the repo or token is unset.
func LoadConfig() Config {
	repo := strings.TrimSpace(os.Getenv("CEREBRO_SKILLS_SYNC_REPO"))
	token := strings.TrimSpace(os.Getenv("CEREBRO_SKILLS_SYNC_TOKEN"))
	if repo == "" || token == "" {
		return Config{Enabled: false}
	}

	cfg := Config{
		Enabled:       true,
		Repo:          repo,
		Token:         token,
		Branch:        firstNonEmpty(os.Getenv("CEREBRO_SKILLS_SYNC_BRANCH"), "main"),
		Interval:      24 * time.Hour,
		WorkDir:       firstNonEmpty(os.Getenv("CEREBRO_SKILLS_SYNC_WORKDIR"), filepath.Join(os.TempDir(), "cerebro-skills-sync")),
		DefaultDomain: firstNonEmpty(os.Getenv("CEREBRO_SKILLS_SYNC_DEFAULT_DOMAIN"), "engineering"),
		AuthorName:    firstNonEmpty(os.Getenv("CEREBRO_SKILLS_SYNC_AUTHOR_NAME"), "Multica Skill Sync"),
		AuthorEmail:   firstNonEmpty(os.Getenv("CEREBRO_SKILLS_SYNC_AUTHOR_EMAIL"), "skills-sync@firtal.com"),
	}
	if v := strings.TrimSpace(os.Getenv("CEREBRO_SKILLS_SYNC_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Interval = d
		}
	}
	if !archiveDomains[cfg.DefaultDomain] {
		cfg.DefaultDomain = "engineering"
	}
	return cfg
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
