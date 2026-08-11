package cursorusage

import (
	"context"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

const (
	envCursorDashboardUsage = "MULTICA_CURSOR_DASHBOARD_USAGE"

	// CostUSDTicksPerUSD matches agent.CostUSDTicksPerUSD / metrics scale.
	CostUSDTicksPerUSD = agent.CostUSDTicksPerUSD

	// Dashboard rows can lag a few seconds behind the local task finish.
	defaultWindowPad = 15 * time.Second
	// Allow small token drift between CLI aggregates and Dashboard rows.
	defaultTokenTolerance = int64(2)
)

// placeholderModels are CLI fallbacks that do not name a real Dashboard model.
// Matching then requires a globally unique token candidate across models.
var placeholderModels = map[string]struct{}{
	"":        {},
	"cursor":  {},
	"auto":    {},
	"default": {},
}

// modelAliases maps normalized CLI / Dashboard labels onto a canonical id.
// Only exact (post-alias) equality is accepted — never substring contains.
var modelAliases = map[string]string{
	"composer":   "composer-1",
	"composer-1": "composer-1",
}

// TaskUsage is the per-model usage slice we may enrich with authoritative cost.
type TaskUsage struct {
	Model            string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSDTicks     int64
	// HasCostUSDTicks marks CostUSDTicks as provider-authoritative, including
	// an authoritative $0 (included-in-plan). When false the value is ignored.
	HasCostUSDTicks bool
	// OccurrenceKeys are opaque digests of the Dashboard event occurrences
	// claimed atomically with the server-side cost correction.
	OccurrenceKeys []string
	// AccountKey is the opaque digest of the Cursor account id used for
	// shared server-side claims (never the raw user_… id).
	AccountKey string
}

// Enricher resolves Cursor Dashboard costs for finished cursor-agent tasks.
type Enricher struct {
	Client *Client
	// ReadSessionToken defaults to ReadLocalSessionToken.
	ReadSessionToken func() (string, error)
	// Sleep allows tests to skip real delays while waiting for Dashboard lag.
	Sleep func(context.Context, time.Duration) error
	// Attempts controls how many times we refetch events when matching fails.
	Attempts int
	// RetryWait is the delay between attempts.
	RetryWait time.Duration
	Logger    *slog.Logger
}

func (e *Enricher) logger() *slog.Logger {
	if e != nil && e.Logger != nil {
		return e.Logger
	}
	return slog.Default()
}

func (e *Enricher) readSession() (string, error) {
	if e != nil && e.ReadSessionToken != nil {
		return e.ReadSessionToken()
	}
	return ReadLocalSessionToken()
}

func (e *Enricher) sleep(ctx context.Context, d time.Duration) error {
	if e != nil && e.Sleep != nil {
		return e.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (e *Enricher) attempts() int {
	if e != nil && e.Attempts > 0 {
		return e.Attempts
	}
	return 3
}

func (e *Enricher) retryWait() time.Duration {
	if e != nil && e.RetryWait > 0 {
		return e.RetryWait
	}
	return 2 * time.Second
}

// Enabled reports whether the daemon may read Cursor's local session and use
// the undocumented Dashboard API. This integration is opt-in.
func Enabled() bool {
	return strings.TrimSpace(os.Getenv(envCursorDashboardUsage)) == "1"
}

// EnrichUsageCosts fills HasCostUSDTicks/CostUSDTicks for usage rows that lack
// a provider cost by matching Dashboard events inside the task window.
// Best-effort: failures leave rows without HasCostUSDTicks so the static rate
// table remains the fallback.
func (e *Enricher) EnrichUsageCosts(ctx context.Context, taskID string, start, end time.Time, usage []TaskUsage) []TaskUsage {
	if len(usage) == 0 || strings.TrimSpace(taskID) == "" {
		return usage
	}
	needs := false
	for _, u := range usage {
		if !u.HasCostUSDTicks && hasTokens(u) {
			needs = true
			break
		}
	}
	if !needs {
		return usage
	}

	sessionToken, err := e.readSession()
	if err != nil {
		e.logger().Debug("cursor dashboard auth unavailable", "error", err)
		return usage
	}
	client := e.Client
	if client == nil {
		client = &Client{}
	}

	rawAccountKey := AccountKeyFromSessionToken(sessionToken)
	if rawAccountKey == "" {
		e.logger().Debug("cursor dashboard session token missing account key")
		return usage
	}
	accountKey := OpaqueClaimKey(rawAccountKey)
	if accountKey == "" {
		return usage
	}

	windowStart := start.Add(-defaultWindowPad)
	windowEnd := end.Add(defaultWindowPad)
	// Do not evaluate a window that is still open. Dashboard rows are
	// eventually consistent, and accepting a unique candidate while later
	// in-window rows can still arrive can permanently attribute another Cursor
	// request to this task.
	if wait := time.Until(windowEnd); wait > 0 {
		e.logger().Debug("cursor dashboard cost reconcile: waiting for task window to close",
			"task_id", taskID,
			"wait", wait,
		)
		if err := e.sleep(ctx, wait); err != nil {
			return usage
		}
	}

	out := append([]TaskUsage(nil), usage...)
	// A unique match in one Dashboard snapshot is not enough: the task's own
	// event may still be indexing while an unrelated account event is already
	// visible. Require the exact cost + occurrence set in two consecutive
	// complete snapshots before committing it to the result.
	pendingSignatures := make([]string, len(out))
	for attempt := 1; attempt <= e.attempts(); attempt++ {
		events, err := client.FetchFilteredUsageEvents(ctx, sessionToken, windowStart, windowEnd)
		if err != nil {
			e.logger().Debug("cursor dashboard usage fetch failed", "error", err, "attempt", attempt)
			return out
		}
		proposed := append([]TaskUsage(nil), out...)
		applyMatches(accountKey, proposed, events, windowStart, windowEnd)
		matched := commitStableMatches(out, proposed, pendingSignatures)
		pending := pendingTokenCostRows(out)
		if pending == 0 {
			if matched > 0 {
				e.logger().Info("cursor dashboard cost reconciled",
					"task_id", taskID,
					"matched_models", matched,
					"events", len(events),
					"attempt", attempt,
				)
			}
			return out
		}
		if matched > 0 {
			e.logger().Info("cursor dashboard cost partially reconciled; retrying remaining models",
				"task_id", taskID,
				"matched_models", matched,
				"pending_models", pending,
				"events", len(events),
				"attempt", attempt,
			)
		}
		if attempt == e.attempts() {
			e.logger().Debug("cursor dashboard cost reconcile: unmatched models remain",
				"task_id", taskID,
				"events", len(events),
				"pending_models", pending,
			)
			return out
		}
		if err := e.sleep(ctx, e.retryWait()); err != nil {
			return out
		}
	}
	return out
}

func commitStableMatches(current, proposed []TaskUsage, pendingSignatures []string) int {
	matched := 0
	for i := range current {
		if current[i].HasCostUSDTicks || !hasTokens(current[i]) {
			pendingSignatures[i] = ""
			continue
		}
		if i >= len(proposed) || !proposed[i].HasCostUSDTicks || len(proposed[i].OccurrenceKeys) == 0 {
			pendingSignatures[i] = ""
			continue
		}
		sig := stableMatchSignature(proposed[i])
		if pendingSignatures[i] != sig {
			pendingSignatures[i] = sig
			continue
		}
		current[i] = proposed[i]
		pendingSignatures[i] = ""
		matched++
	}
	return matched
}

func stableMatchSignature(u TaskUsage) string {
	return strconv.FormatInt(u.CostUSDTicks, 10) + "\x00" + strings.Join(u.OccurrenceKeys, "\x00")
}

func hasTokens(u TaskUsage) bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0
}

func pendingTokenCostRows(usage []TaskUsage) int {
	n := 0
	for _, u := range usage {
		if !u.HasCostUSDTicks && hasTokens(u) {
			n++
		}
	}
	return n
}

func assignedOccurrenceKeys(usage []TaskUsage) []string {
	var keys []string
	for _, u := range usage {
		keys = append(keys, u.OccurrenceKeys...)
	}
	return keys
}

func applyMatches(accountKey string, usage []TaskUsage, events []UsageEvent, start, end time.Time) int {
	available := assignOccurrenceIndexes(filterEventsInWindow(events, start, end))
	// Prior attempts may already have matched occurrences for another model.
	available = removeKeys(available, assignedOccurrenceKeys(usage))
	matched := 0
	for i := range usage {
		if usage[i].HasCostUSDTicks || !hasTokens(usage[i]) {
			continue
		}
		cost, keys, ok := matchUsage(usage[i], available)
		if !ok {
			continue
		}
		usage[i].CostUSDTicks = cost
		usage[i].HasCostUSDTicks = true
		usage[i].OccurrenceKeys = append([]string(nil), keys...)
		usage[i].AccountKey = accountKey
		available = removeKeys(available, keys)
		matched++
	}
	return matched
}

func filterEventsInWindow(events []UsageEvent, start, end time.Time) []UsageEvent {
	out := make([]UsageEvent, 0, len(events))
	for _, e := range events {
		if e.Timestamp.Before(start) || e.Timestamp.After(end) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func removeKeys(events []UsageEvent, keys []string) []UsageEvent {
	drop := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		drop[k] = struct{}{}
	}
	out := make([]UsageEvent, 0, len(events))
	for _, e := range events {
		if _, ok := drop[e.OccurrenceKey()]; ok {
			continue
		}
		out = append(out, e)
	}
	return out
}

// matchUsage finds a high-confidence event set for one usage row.
// Fail closed on ambiguity (multiple model groups, missing chargedCents, etc.).
func matchUsage(u TaskUsage, events []UsageEvent) (costTicks int64, keys []string, ok bool) {
	wantModel := canonicalizeModel(u.Model)
	if isPlaceholderModel(wantModel) {
		return matchUniqueAcrossModels(u, events)
	}

	candidates := make([]UsageEvent, 0, len(events))
	for _, e := range events {
		if canonicalizeModel(e.Model) == wantModel {
			candidates = append(candidates, e)
		}
	}
	return matchTokenSet(u, candidates)
}

func matchUniqueAcrossModels(u TaskUsage, events []UsageEvent) (int64, []string, bool) {
	byModel := map[string][]UsageEvent{}
	for _, e := range events {
		key := canonicalizeModel(e.Model)
		if key == "" || isPlaceholderModel(key) {
			continue
		}
		byModel[key] = append(byModel[key], e)
	}

	type hit struct {
		cost int64
		keys []string
	}
	var hits []hit
	for _, group := range byModel {
		cost, keys, ok := matchTokenSet(u, group)
		if ok {
			hits = append(hits, hit{cost: cost, keys: keys})
		}
	}
	if len(hits) != 1 {
		return 0, nil, false
	}
	return hits[0].cost, hits[0].keys, true
}

func matchTokenSet(u TaskUsage, candidates []UsageEvent) (int64, []string, bool) {
	if len(candidates) == 0 {
		return 0, nil, false
	}
	// Contiguous windows only (chronological Dashboard order). Collect every
	// compatible window and succeed only when exactly one distinct key-set
	// matches — otherwise two identical composer-1 rows could each match and
	// attribution would depend on worker/API order.
	type hit struct {
		cost int64
		keys []string
	}
	hitsBySig := map[string]hit{}
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j <= len(candidates); j++ {
			cost, keys, ok := tokensMatchCost(u, candidates[i:j])
			if !ok {
				continue
			}
			sig := strings.Join(keys, "\x00")
			hitsBySig[sig] = hit{cost: cost, keys: keys}
		}
	}
	if len(hitsBySig) != 1 {
		return 0, nil, false
	}
	for _, h := range hitsBySig {
		return h.cost, h.keys, true
	}
	return 0, nil, false
}

func tokensMatchCost(u TaskUsage, events []UsageEvent) (int64, []string, bool) {
	var in, out, cacheRead, cacheWrite int64
	var cents float64
	keys := make([]string, 0, len(events))
	for _, e := range events {
		if !e.HasChargedCents {
			// CodexBar refuses to publish a total when any event omits chargedCents.
			return 0, nil, false
		}
		in += e.InputTokens
		out += e.OutputTokens
		cacheRead += e.CacheReadTokens
		cacheWrite += e.CacheWriteTokens
		cents += e.ChargedCents
		keys = append(keys, e.OccurrenceKey())
	}
	if !within(in, u.InputTokens, defaultTokenTolerance) ||
		!within(out, u.OutputTokens, defaultTokenTolerance) ||
		!within(cacheRead, u.CacheReadTokens, defaultTokenTolerance) ||
		!within(cacheWrite, u.CacheWriteTokens, defaultTokenTolerance) {
		return 0, nil, false
	}
	if cents < 0 {
		return 0, nil, false
	}
	ticks := int64(math.Round(cents / 100 * float64(CostUSDTicksPerUSD)))
	if ticks < 0 {
		return 0, nil, false
	}
	return ticks, keys, true
}

func within(a, b, tol int64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func normalizeModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	m = strings.TrimPrefix(m, "cursor/")
	m = strings.TrimPrefix(m, "agent/")
	return m
}

func canonicalizeModel(model string) string {
	m := normalizeModel(model)
	if alias, ok := modelAliases[m]; ok {
		return alias
	}
	return m
}

func isPlaceholderModel(model string) bool {
	_, ok := placeholderModels[normalizeModel(model)]
	return ok
}

// CentsToUSDTicks converts Cursor chargedCents into Multica cost ticks.
// A present zero remains zero (authoritative included-in-plan spend).
func CentsToUSDTicks(cents float64) int64 {
	if cents < 0 {
		return 0
	}
	return int64(math.Round(cents / 100 * float64(CostUSDTicksPerUSD)))
}
