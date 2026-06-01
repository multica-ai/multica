package skillsync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// StartWorker launches the FIR-2656 nightly skill-archive sync in a goroutine
// and returns immediately. It is disabled (and logs why) when the archive is
// not configured (CEREBRO_SKILLS_SYNC_REPO / _TOKEN unset) or when no git
// binary is available, so a self-host install without the archive never runs
// it.
func StartWorker(ctx context.Context, cerebro *cerebrodb.Queries, upstream *db.Queries) {
	cfg := LoadConfig()
	if !cfg.Enabled {
		slog.Info("skill archive sync disabled: CEREBRO_SKILLS_SYNC_REPO/_TOKEN unset")
		return
	}
	if !gitAvailable() {
		slog.Error("skill archive sync disabled: git not found on PATH")
		return
	}
	w := &worker{cfg: cfg, cerebro: cerebro, upstream: upstream, repo: &repo{cfg: cfg}}
	slog.Info("skill archive sync started",
		"branch", cfg.Branch, "interval", cfg.Interval.String(), "default_domain", cfg.DefaultDomain)
	go w.run(ctx)
}

type worker struct {
	cfg      Config
	cerebro  *cerebrodb.Queries
	upstream *db.Queries
	repo     *repo
}

func (w *worker) run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	w.tick(ctx) // run once on startup, then every interval
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick refreshes the archive checkout, materialises every approved skill, and
// commits + pushes only when something changed.
func (w *worker) tick(ctx context.Context) {
	dir, err := w.repo.ensureCheckout(ctx)
	if err != nil {
		slog.Warn("skill archive sync: checkout failed", "error", err)
		return
	}
	existing, err := existingSlugDomains(dir)
	if err != nil {
		slog.Warn("skill archive sync: scanning existing skills failed", "error", err)
		return
	}
	skills, err := w.cerebro.ListSkillsForArchive(ctx)
	if err != nil {
		slog.Warn("skill archive sync: listing skills failed", "error", err)
		return
	}

	written := 0
	seen := make(map[string]string) // slug -> domain written this tick
	for _, s := range skills {
		slug := skillSlug(s.Name)
		domain := archiveDomain(s.Content, existing[slug], w.cfg.DefaultDomain)

		// Two workspaces can hold a same-named skill; the archive is keyed by
		// slug, so the first one wins and the rest are skipped with a warning
		// rather than silently clobbering each other.
		if prev, ok := seen[slug]; ok {
			slog.Warn("skill archive sync: duplicate slug skipped",
				"slug", slug, "kept_domain", prev, "skill_id", uuidString(s.ID), "workspace_id", uuidString(s.WorkspaceID))
			continue
		}

		files, err := w.upstream.ListSkillFiles(ctx, s.ID)
		if err != nil {
			slog.Warn("skill archive sync: listing files failed", "slug", slug, "error", err)
			continue
		}
		fileMap := make(map[string]string, len(files)+1)
		fileMap["SKILL.md"] = ensureFrontmatter(s.Content, slug, s.Description)
		for _, f := range files {
			fileMap[f.Path] = f.Content
		}
		if err := writeSkillFiles(dir, domain, slug, fileMap); err != nil {
			slog.Warn("skill archive sync: writing skill failed", "slug", slug, "domain", domain, "error", err)
			continue
		}
		seen[slug] = domain
		written++
	}

	msg := fmt.Sprintf("chore(sync): nightly skill archive sync (%d skills)", written)
	pushed, err := w.repo.commitAndPush(ctx, dir, msg)
	if err != nil {
		slog.Warn("skill archive sync: commit/push failed", "error", err)
		return
	}
	if pushed {
		slog.Info("skill archive sync: pushed", "skills", written)
	} else {
		slog.Info("skill archive sync: no changes", "skills", written)
	}
}

// uuidString renders a pgtype.UUID for logging.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", u.Bytes[0:4], u.Bytes[4:6], u.Bytes[6:8], u.Bytes[8:10], u.Bytes[10:16])
}
