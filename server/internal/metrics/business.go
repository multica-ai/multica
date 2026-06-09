package metrics

import (
	"sync"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
	"github.com/prometheus/client_golang/prometheus"
)

var taskDurationBuckets = []float64{1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1200, 3600, 7200}

type activeTaskLabels struct {
	source      string
	runtimeMode string
}

type BusinessMetrics struct {
	taskEnqueued     *prometheus.CounterVec
	taskDispatched   *prometheus.CounterVec
	taskStarted      *prometheus.CounterVec
	taskTerminal     *prometheus.CounterVec
	taskFailed       *prometheus.CounterVec
	taskQueueWait    *prometheus.HistogramVec
	taskRunSeconds   *prometheus.HistogramVec
	taskTotalSeconds *prometheus.HistogramVec
	taskInProgress   *prometheus.GaugeVec
	taskIterations   *prometheus.HistogramVec

	llmTokens         *prometheus.CounterVec
	llmCostUSD        *prometheus.CounterVec
	llmUnpricedTokens *prometheus.CounterVec
	llmRequests       *prometheus.CounterVec

	taskQueuedExpired *prometheus.CounterVec
	taskLeaseExpired  *prometheus.CounterVec

	activeMu    sync.Mutex
	activeTasks map[string]activeTaskLabels

	// PR3 funnel / community / commercial counters. See business_events.go
	// for the field-level docs and labels.
	events *businessEventMetrics
}

func NewBusinessMetrics() *BusinessMetrics {
	validateBusinessMetricLabels()
	m := &BusinessMetrics{
		taskEnqueued: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "agent_task",
			Name:      "enqueued_total",
			Help:      "Total agent tasks enqueued.",
		}, metricLabels("multica_agent_task_enqueued_total")),
		taskDispatched: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "agent_task",
			Name:      "dispatched_total",
			Help:      "Total agent tasks dispatched to a runtime.",
		}, metricLabels("multica_agent_task_dispatched_total")),
		taskStarted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "agent_task",
			Name:      "started_total",
			Help:      "Total agent tasks that reached running state.",
		}, metricLabels("multica_agent_task_started_total")),
		taskTerminal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "agent_task",
			Name:      "terminal_total",
			Help:      "Total agent tasks that reached a terminal state.",
		}, metricLabels("multica_agent_task_terminal_total")),
		taskFailed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "agent_task",
			Name:      "failed_total",
			Help:      "Total failed agent tasks by canonical failure reason.",
		}, metricLabels("multica_agent_task_failed_total")),
		taskQueueWait: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "multica",
			Subsystem: "agent_task",
			Name:      "queue_wait_seconds",
			Help:      "Time agent tasks spent queued before dispatch.",
			Buckets:   taskDurationBuckets,
		}, metricLabels("multica_agent_task_queue_wait_seconds")),
		taskRunSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "multica",
			Subsystem: "agent_task",
			Name:      "run_seconds",
			Help:      "Time agent tasks spent running before a terminal state.",
			Buckets:   taskDurationBuckets,
		}, metricLabels("multica_agent_task_run_seconds")),
		taskTotalSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "multica",
			Subsystem: "agent_task",
			Name:      "total_seconds",
			Help:      "Total time from agent task creation to terminal state.",
			Buckets:   taskDurationBuckets,
		}, metricLabels("multica_agent_task_total_seconds")),
		taskInProgress: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "agent_task",
			Name:      "in_progress",
			Help:      "Current agent tasks dispatched by this process and not yet terminal.",
		}, metricLabels("multica_agent_task_in_progress")),
		taskIterations: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "multica",
			Subsystem: "agent_task",
			Name:      "iteration_count",
			Help:      "Retry attempt count observed when an agent task reaches a terminal state.",
			Buckets:   []float64{1, 2, 3, 4, 5, 10},
		}, metricLabels("multica_agent_task_iteration_count")),
		llmTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "llm",
			Name:      "tokens_total",
			Help:      "Total priced LLM tokens by provider, model, token type, runtime mode, and task source.",
		}, metricLabels("multica_llm_tokens_total")),
		llmCostUSD: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "llm",
			Name:      "cost_usd_total",
			Help:      "Total estimated priced LLM token cost in USD.",
		}, metricLabels("multica_llm_cost_usd_total")),
		llmUnpricedTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "llm",
			Name:      "unpriced_tokens_total",
			Help:      "Total LLM tokens for model aliases without a fixed TSR price.",
		}, metricLabels("multica_llm_unpriced_tokens_total")),
		llmRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "llm",
			Name:      "request_total",
			Help:      "Total task usage reports by normalized LLM provider and model.",
		}, metricLabels("multica_llm_request_total")),
		taskQueuedExpired: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "task",
			Name:      "queued_expired_total",
			Help:      "Total queued tasks expired by the scheduler.",
		}, metricLabels("multica_task_queued_expired_total")),
		taskLeaseExpired: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "task",
			Name:      "lease_expired_total",
			Help:      "Total dispatched or running task leases expired by the scheduler.",
		}, metricLabels("multica_task_lease_expired_total")),
		activeTasks: map[string]activeTaskLabels{},
		events:      newBusinessEventMetrics(),
	}
	m.prewarmFailureReasons()
	return m
}

func (m *BusinessMetrics) Collectors() []prometheus.Collector {
	return append([]prometheus.Collector{
		m.taskEnqueued,
		m.taskDispatched,
		m.taskStarted,
		m.taskTerminal,
		m.taskFailed,
		m.taskQueueWait,
		m.taskRunSeconds,
		m.taskTotalSeconds,
		m.taskInProgress,
		m.taskIterations,
		m.llmTokens,
		m.llmCostUSD,
		m.llmUnpricedTokens,
		m.llmRequests,
		m.taskQueuedExpired,
		m.taskLeaseExpired,
	}, m.events.collectors()...)
}

func (m *BusinessMetrics) RecordTaskEnqueued(source, runtimeMode string) {
	if m == nil {
		return
	}
	m.taskEnqueued.WithLabelValues(NormalizeTaskSource(source), NormalizeRuntimeMode(runtimeMode)).Inc()
}

func (m *BusinessMetrics) RecordTaskDispatched(taskID, source, runtimeMode string, queueWaitSeconds float64) {
	if m == nil {
		return
	}
	source = NormalizeTaskSource(source)
	runtimeMode = NormalizeRuntimeMode(runtimeMode)
	m.taskDispatched.WithLabelValues(source, runtimeMode).Inc()
	if queueWaitSeconds >= 0 {
		m.taskQueueWait.WithLabelValues(source, runtimeMode).Observe(queueWaitSeconds)
	}
	m.markTaskInProgress(taskID, source, runtimeMode)
}

func (m *BusinessMetrics) RecordTaskStarted(source, runtimeMode, provider string) {
	if m == nil {
		return
	}
	m.taskStarted.WithLabelValues(
		NormalizeTaskSource(source),
		NormalizeRuntimeMode(runtimeMode),
		NormalizeRuntimeProvider(provider),
	).Inc()
}

func (m *BusinessMetrics) RecordTaskTerminal(taskID, source, runtimeMode, terminalStatus string, runSeconds, totalSeconds float64, attempt int32) {
	if m == nil {
		return
	}
	source = NormalizeTaskSource(source)
	runtimeMode = NormalizeRuntimeMode(runtimeMode)
	terminalStatus = NormalizeTerminalStatus(terminalStatus)
	m.taskTerminal.WithLabelValues(source, runtimeMode, terminalStatus).Inc()
	if runSeconds >= 0 {
		m.taskRunSeconds.WithLabelValues(source, runtimeMode, terminalStatus).Observe(runSeconds)
	}
	if totalSeconds >= 0 {
		m.taskTotalSeconds.WithLabelValues(source, runtimeMode, terminalStatus).Observe(totalSeconds)
	}
	if attempt < 1 {
		attempt = 1
	}
	m.taskIterations.WithLabelValues(source, terminalStatus).Observe(float64(attempt))
	m.clearTaskInProgress(taskID)
}

func (m *BusinessMetrics) RecordTaskFailed(source, runtimeMode, failureReason string) {
	if m == nil {
		return
	}
	m.taskFailed.WithLabelValues(
		NormalizeTaskSource(source),
		NormalizeRuntimeMode(runtimeMode),
		NormalizeFailureReason(failureReason),
	).Inc()
}

func (m *BusinessMetrics) RecordTaskQueuedExpired(source, runtimeMode string) {
	if m == nil {
		return
	}
	m.taskQueuedExpired.WithLabelValues(NormalizeTaskSource(source), NormalizeRuntimeMode(runtimeMode)).Inc()
}

func (m *BusinessMetrics) RecordTaskLeaseExpired(source string) {
	if m == nil {
		return
	}
	m.taskLeaseExpired.WithLabelValues(NormalizeTaskSource(source)).Inc()
}

func (m *BusinessMetrics) RecordLLMUsage(source, runtimeMode, rawProvider, modelAlias string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int64) {
	if m == nil {
		return
	}
	source = NormalizeTaskSource(source)
	runtimeMode = NormalizeRuntimeMode(runtimeMode)
	price, priced := PriceForModelAlias(modelAlias)
	if !priced {
		provider := NormalizeRuntimeProvider(rawProvider)
		alias := NormalizeModelAlias(modelAlias)
		m.recordUnpricedTokens(provider, alias, "input", inputTokens)
		m.recordUnpricedTokens(provider, alias, "output", outputTokens)
		m.recordUnpricedTokens(provider, alias, "cache_read", cacheReadTokens)
		m.recordUnpricedTokens(provider, alias, "cache_write", cacheWriteTokens)
		m.llmRequests.WithLabelValues(provider, "unknown", runtimeMode).Inc()
		return
	}

	m.recordPricedTokens(price.Provider, price.Model, "input", runtimeMode, source, inputTokens, tokenCostUSD(inputTokens, price.InputPerM))
	m.recordPricedTokens(price.Provider, price.Model, "output", runtimeMode, source, outputTokens, tokenCostUSD(outputTokens, price.OutputPerM))
	m.recordPricedTokens(price.Provider, price.Model, "cache_read", runtimeMode, source, cacheReadTokens, tokenCostUSD(cacheReadTokens, price.CacheReadPerM))
	m.recordPricedTokens(price.Provider, price.Model, "cache_write", runtimeMode, source, cacheWriteTokens, tokenCostUSD(cacheWriteTokens, price.CacheWritePerM))
	m.llmRequests.WithLabelValues(price.Provider, price.Model, runtimeMode).Inc()
}

func (m *BusinessMetrics) recordPricedTokens(provider, model, tokenType, runtimeMode, source string, tokens int64, cost float64) {
	if tokens <= 0 {
		return
	}
	tokenType = NormalizeTokenType(tokenType)
	m.llmTokens.WithLabelValues(provider, model, tokenType, runtimeMode, source).Add(float64(tokens))
	if cost > 0 {
		m.llmCostUSD.WithLabelValues(provider, model, tokenType, runtimeMode, source).Add(cost)
	}
}

func (m *BusinessMetrics) recordUnpricedTokens(provider, modelAlias, tokenType string, tokens int64) {
	if tokens <= 0 {
		return
	}
	m.llmUnpricedTokens.WithLabelValues(provider, modelAlias, NormalizeTokenType(tokenType)).Add(float64(tokens))
}

func (m *BusinessMetrics) markTaskInProgress(taskID, source, runtimeMode string) {
	if taskID == "" {
		m.taskInProgress.WithLabelValues(source, runtimeMode).Inc()
		return
	}
	m.activeMu.Lock()
	defer m.activeMu.Unlock()
	if _, ok := m.activeTasks[taskID]; ok {
		return
	}
	m.activeTasks[taskID] = activeTaskLabels{source: source, runtimeMode: runtimeMode}
	m.taskInProgress.WithLabelValues(source, runtimeMode).Inc()
}

func (m *BusinessMetrics) clearTaskInProgress(taskID string) {
	if taskID == "" {
		return
	}
	m.activeMu.Lock()
	labels, ok := m.activeTasks[taskID]
	if ok {
		delete(m.activeTasks, taskID)
	}
	m.activeMu.Unlock()
	if ok {
		m.taskInProgress.WithLabelValues(labels.source, labels.runtimeMode).Dec()
	}
}

func (m *BusinessMetrics) prewarmFailureReasons() {
	for _, source := range []string{"issue", "chat", "autopilot", "autopilot_issue", "quick_create", "other"} {
		for _, runtimeMode := range []string{"local", "cloud", "unknown"} {
			for _, reason := range taskfailure.AllReasons() {
				m.taskFailed.WithLabelValues(source, runtimeMode, reason.String()).Add(0)
			}
		}
	}
}
