package metrics

import "github.com/prometheus/client_golang/prometheus"

// WecomMetrics is the production sink behind the WeCom adapter's Metrics
// interface (server/internal/integrations/wecom/metrics.go).
//
// The adapter is built to degrade quietly. A dial that fails, a handshake the
// server refuses, an ingest queue the read loop has to wait on — each of them
// yields the connection back to the Supervisor, which backs off and tries
// again, and none of them changes anything an operator can see. A bot that has
// been down since Tuesday and a bot nobody happened to message today produce
// the same silence.
//
// The two connection counters are deliberately separate. A dial or a read that
// fails is infrastructure and usually recovers on its own; a handshake the
// server refuses on its merits is a wrong secret or a deleted bot, and it will
// repeat identically on every backoff until a person fixes the installation.
// Summed into one number the operator cannot tell "wait" from "rotate the
// credential".
//
// No installation_id label anywhere. It is the same class of unbounded
// identifier as workspace_id and session_id, which forbiddenMetricLabels
// rejects outright; per-installation attribution is in the structured logs,
// which carry it at every one of these call sites.
type WecomMetrics struct {
	ConnectFailures      prometheus.Counter
	AuthFailures         prometheus.Counter
	CallbacksQueued      prometheus.Counter
	CallbackQueueBlocked prometheus.Counter
}

func NewWecomMetrics() *WecomMetrics {
	counter := func(name, help string) prometheus.Counter {
		return prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica", Subsystem: "wecom", Name: name, Help: help,
		})
	}
	return &WecomMetrics{
		ConnectFailures: counter("connect_failures_total",
			"Long-connection dials and handshakes that did not complete for an infrastructure reason. Excludes credential rejections, which are counted apart."),
		AuthFailures: counter("auth_failures_total",
			"aibot_subscribe answered with a non-zero errcode. The bot stays down until somebody fixes the installation."),
		CallbacksQueued: counter("inbound_callbacks_total",
			"Inbound callbacks handed to the ingest worker. The baseline every other inbound number is read against."),
		CallbackQueueBlocked: counter("inbound_queue_blocked_total",
			"Times the read loop had to wait on a full ingest queue. Backpressure by design; a rising rate means the engine is behind and the socket is about to stop being drained."),
	}
}

func (m *WecomMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.ConnectFailures, m.AuthFailures,
		m.CallbacksQueued, m.CallbackQueueBlocked,
	}
}

// ---- the adapter's Metrics interface ----

func (m *WecomMetrics) RecordConnectFailure()       { m.ConnectFailures.Inc() }
func (m *WecomMetrics) RecordAuthFailure()          { m.AuthFailures.Inc() }
func (m *WecomMetrics) RecordCallbackQueued()       { m.CallbacksQueued.Inc() }
func (m *WecomMetrics) RecordCallbackQueueBlocked() { m.CallbackQueueBlocked.Inc() }
