package daemon

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/cursorusage"
)

const (
	cursorCostReconcileTimeout   = 45 * time.Second
	cursorCostReconcileQueueSize = 64
)

type cursorCostJob struct {
	taskID string
	start  time.Time
	end    time.Time
	usage  []TaskUsageEntry
}

// cursorCostReconciler runs Dashboard cost matching off the task-completion
// path so HTTP lag / retries never hold a task slot or delay the terminal
// callback. Successful matches use the cost-only endpoint (no Prometheus
// CaptureTaskUsage side effects).
type cursorCostReconciler struct {
	client *Client
	log    *slog.Logger
	enrich *cursorusage.Enricher

	once sync.Once
	jobs chan cursorCostJob
}

func newCursorCostReconciler(client *Client, log *slog.Logger) *cursorCostReconciler {
	return &cursorCostReconciler{
		client: client,
		log:    log,
		enrich: &cursorusage.Enricher{Logger: log},
		jobs:   make(chan cursorCostJob, cursorCostReconcileQueueSize),
	}
}

func (r *cursorCostReconciler) start(ctx context.Context) {
	if r == nil {
		return
	}
	r.once.Do(func() {
		go r.loop(ctx)
	})
}

func (r *cursorCostReconciler) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-r.jobs:
			r.runJob(ctx, job)
		}
	}
}

func (r *cursorCostReconciler) enqueue(job cursorCostJob) {
	if r == nil || !cursorusage.Enabled() || len(job.usage) == 0 || job.taskID == "" {
		return
	}
	if !needsCursorCostEnrichment(job.usage) {
		return
	}
	select {
	case r.jobs <- job:
	default:
		if r.log != nil {
			r.log.Debug("cursor cost reconcile queue full; leaving estimate in place",
				"task_id", job.taskID,
			)
		}
	}
}

func (r *cursorCostReconciler) runJob(parent context.Context, job cursorCostJob) {
	base := parent
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(base), cursorCostReconcileTimeout)
	defer cancel()

	usage := toCursorUsage(job.usage)
	enriched := r.enrich.EnrichUsageCosts(ctx, job.taskID, job.start, job.end, usage)
	corrections, accountKey := costCorrectionsFromUsage(enriched)
	if len(corrections) == 0 || accountKey == "" {
		return
	}

	if err := r.client.ReportCursorUsageCost(ctx, job.taskID, accountKey, corrections); err != nil {
		if r.log != nil {
			r.log.Warn("cursor cost reconcile report failed", "task_id", job.taskID, "error", err)
		}
	}
}

func needsCursorCostEnrichment(entries []TaskUsageEntry) bool {
	for _, e := range entries {
		if e.CostUSDTicksPresent {
			continue
		}
		if e.InputTokens > 0 || e.OutputTokens > 0 || e.CacheReadTokens > 0 || e.CacheWriteTokens > 0 {
			return true
		}
	}
	return false
}

func toCursorUsage(entries []TaskUsageEntry) []cursorusage.TaskUsage {
	usage := make([]cursorusage.TaskUsage, len(entries))
	for i, e := range entries {
		usage[i] = cursorusage.TaskUsage{
			Model:            e.Model,
			InputTokens:      e.InputTokens,
			OutputTokens:     e.OutputTokens,
			CacheReadTokens:  e.CacheReadTokens,
			CacheWriteTokens: e.CacheWriteTokens,
			CostUSDTicks:     e.CostUSDTicks,
			HasCostUSDTicks:  e.CostUSDTicksPresent,
		}
	}
	return usage
}

func costCorrectionsFromUsage(usage []cursorusage.TaskUsage) ([]CursorUsageCostCorrection, string) {
	var corrections []CursorUsageCostCorrection
	accountKey := ""
	for _, u := range usage {
		if !u.HasCostUSDTicks || len(u.OccurrenceKeys) == 0 {
			continue
		}
		if accountKey == "" {
			accountKey = u.AccountKey
		}
		corrections = append(corrections, CursorUsageCostCorrection{
			Model:          u.Model,
			CostUSDTicks:   u.CostUSDTicks,
			OccurrenceKeys: append([]string(nil), u.OccurrenceKeys...),
		})
	}
	return corrections, accountKey
}
