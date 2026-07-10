package modelregistry

// The in-process store: consumers on hot paths (cost computation on every
// task, Prometheus metrics, context-window fullness) never hit the database —
// they read the atomically-swapped table below. The store is loaded from the
// DB at startup (LoadFromDB), re-published on every merge/rollback in this
// process, and refreshed on a timer so other replicas converge within one
// interval.

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/pkg/pricing"
)

type tableState struct {
	snap    Snapshot
	version string
}

var current atomic.Pointer[tableState]

// Publish makes a snapshot the process-wide current table and pushes the
// cents-denominated view into pkg/pricing so ComputeCents / Snapshot (the
// /api/cerebro/pricing contract) read the same data.
func Publish(snap Snapshot, version string) {
	if snap.Models == nil {
		snap.Models = map[string]ModelEntry{}
	}
	current.Store(&tableState{snap: snap, version: version})

	cents := make(map[string]pricing.Pricing, len(snap.Models))
	for id, e := range snap.Models {
		cents[id] = pricing.Pricing{
			InputCentsPerMtok:      e.InputUSDPerMtok * 100,
			OutputCentsPerMtok:     e.OutputUSDPerMtok * 100,
			CacheReadCentsPerMtok:  e.CacheReadUSDPerMtok * 100,
			CacheWriteCentsPerMtok: e.CacheWriteUSDPerMtok * 100,
		}
	}
	pricing.SetTable(cents, snap.FallbackModel, "registry-"+version)
}

// Current returns the published snapshot and its version. ok is false before
// the first successful load.
func Current() (Snapshot, string, bool) {
	st := current.Load()
	if st == nil {
		return Snapshot{}, "", false
	}
	return st.snap, st.version, true
}

// LoadFromDB reads the singleton registry row and publishes it.
func LoadFromDB(ctx context.Context, q *cerebrodb.Queries) error {
	reg, err := q.GetModelRegistry(ctx)
	if err != nil {
		return err
	}
	Publish(DecodeSnapshot(reg.Snapshot), reg.CurrentVersion)
	return nil
}

// StartRefresher reloads the table on an interval so replicas that did not
// serve the approving request converge. Call once from server startup.
func StartRefresher(ctx context.Context, q *cerebrodb.Queries, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := LoadFromDB(ctx, q); err != nil {
					slog.Warn("modelregistry: periodic reload failed", "error", err)
				}
			}
		}
	}()
}

// --- Lookup ---

// dateSuffixPattern matches a trailing dated snapshot or `-latest` tag, so
// `claude-haiku-4-5-20251001` resolves to the `claude-haiku-4-5` row.
var dateSuffixPattern = regexp.MustCompile(`-(20\d{2}-\d{2}-\d{2}|20\d{6}|latest)$`)

// aliasRules map observed model strings that are NOT registry ids onto their
// canonical registry row — ported from the old internal/metrics alias table
// (dotted Anthropic ids from Copilot/Cursor, provider-prefixed ids from
// openclaw/opencode, hyphen-variant OpenAI SKUs). Matching logic is code, not
// registry data: these express identity ("claude-opus-4.7 IS claude-opus-4-7"),
// not prices.
var aliasRules = []struct {
	re *regexp.Regexp
	id string
}{
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]5$|^gpt-5-5$`), "gpt-5.5"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]4($|-2026-03-05|-xhigh)`), "gpt-5.4"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]4-mini($|[^a-z0-9])`), "gpt-5.4-mini"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]3-codex$`), "gpt-5.3-codex"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]2-codex$`), "gpt-5.2-codex"},
	{regexp.MustCompile(`claude-fable-5`), "claude-fable-5"},
	{regexp.MustCompile(`claude-opus-4[-.]8`), "claude-opus-4-8"},
	{regexp.MustCompile(`claude-opus-4[-.]7`), "claude-opus-4-7"},
	{regexp.MustCompile(`claude-opus-4[-.]6`), "claude-opus-4-6"},
	{regexp.MustCompile(`claude-opus-4[-.]5`), "claude-opus-4-5"},
	{regexp.MustCompile(`claude-opus-4[-.]1`), "claude-opus-4-1"},
	{regexp.MustCompile(`claude-sonnet-5`), "claude-sonnet-5"},
	{regexp.MustCompile(`claude-sonnet-4[-.]6|claude-4[-.]6-sonnet`), "claude-sonnet-4-6"},
	{regexp.MustCompile(`claude-sonnet-4[-.]5|claude-4[-.]5-sonnet`), "claude-sonnet-4-5"},
	{regexp.MustCompile(`claude-haiku-4[-.]5`), "claude-haiku-4-5"},
	{regexp.MustCompile(`claude-haiku-3[-.]5`), "claude-haiku-3-5"},
	{regexp.MustCompile(`deepseek-v4-pro`), "deepseek-v4-pro"},
	{regexp.MustCompile(`deepseek-v4-flash`), "deepseek-v4-flash"},
	{regexp.MustCompile(`minimax-m2[.]7.*highspeed|highspeed.*minimax-m2[.]7`), "minimax-m2.7-highspeed"},
	{regexp.MustCompile(`minimax-m2[.]7`), "minimax-m2.7"},
	{regexp.MustCompile(`gemini-3-flash`), "gemini-3-flash"},
	{regexp.MustCompile(`gemini-3[.]1-pro`), "gemini-3.1-pro"},
	{regexp.MustCompile(`gemini-2[.]5-pro`), "gemini-2.5-pro"},
	{regexp.MustCompile(`gemini-2[.]5-flash($|[^-])`), "gemini-2.5-flash"},
}

// Lookup resolves a raw model string (as reported by runtimes: mixed casing,
// provider prefixes, dated snapshots, dotted variants) to its canonical
// registry id and entry.
func Lookup(model string) (string, ModelEntry, bool) {
	st := current.Load()
	if st == nil {
		return "", ModelEntry{}, false
	}
	key := strings.ToLower(strings.TrimSpace(model))
	if key == "" {
		return "", ModelEntry{}, false
	}
	if e, ok := st.snap.Models[key]; ok {
		return key, e, true
	}
	// Strip a `<provider>/` or `<provider>:` routing prefix.
	if i := strings.LastIndexAny(key, "/:"); i >= 0 && i+1 < len(key) {
		trimmed := key[i+1:]
		if e, ok := st.snap.Models[trimmed]; ok {
			return trimmed, e, true
		}
		key = trimmed
	}
	// Strip a trailing dated-snapshot / -latest tag.
	if stripped := dateSuffixPattern.ReplaceAllString(key, ""); stripped != key {
		if e, ok := st.snap.Models[stripped]; ok {
			return stripped, e, true
		}
	}
	// Alias rules for spellings that are not simple suffix/prefix variants.
	for _, rule := range aliasRules {
		if rule.re.MatchString(key) {
			if e, ok := st.snap.Models[rule.id]; ok {
				return rule.id, e, true
			}
		}
	}
	return "", ModelEntry{}, false
}

// ContextWindow returns the curated context window for a model. ok is false
// when the model is unknown or its window is not curated (0) — callers apply
// their own conservative default.
func ContextWindow(model string) (int64, bool) {
	_, e, ok := Lookup(model)
	if !ok || e.ContextWindow <= 0 {
		return 0, false
	}
	return e.ContextWindow, true
}
