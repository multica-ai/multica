package metrics

import "github.com/prometheus/client_golang/prometheus"

// WecomMetrics is the production sink behind the WeCom adapter's Metrics
// interface (server/internal/integrations/wecom/metrics.go).
//
// The adapter is built to degrade quietly. A dial that fails and a handshake
// the server refuses both yield the connection back to the Supervisor, which
// backs off and tries again; an ingest queue the read loop has to wait on
// yields nothing and simply stops draining the socket until the worker
// catches up. None of the three changes anything an operator can see. A bot
// that has been down since Tuesday and a bot nobody happened to message today
// produce the same silence.
//
// The two connection counters are deliberately separate. A dial or a read that
// fails is infrastructure and usually recovers on its own; a handshake the
// server refuses on its merits is a wrong secret or a deleted bot, and it will
// repeat identically on every backoff until a person fixes the installation.
// Summed into one number the operator cannot tell "wait" from "rotate the
// credential".
//
// Which side of that line an ack falls on is classifySubscribeAck's call, in
// the adapter, and this package only counts the verdict. An ack it cannot
// verify — a throttle, a platform-side failure — is a connect failure, on the
// "wait" side, because rotating a credential that was fine costs a second
// outage.
//
// No installation_id label anywhere. It is the same class of unbounded
// identifier as workspace_id and session_id, which forbiddenMetricLabels
// rejects outright. Attribution falls to the structured logs and is uneven
// there: the two connection failures reach the Supervisor as a returned error
// and are logged with installation_id, while a blocked ingest queue is
// counted and nothing else — the counter says some bot is behind, not which.
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
			"Long-connection attempts that failed for a reason nobody has to act on: the socket never came up, or the server answered the handshake with a code that only means it could not verify the bot (a throttle, a platform-side failure). Excludes credential rejections, which are counted apart."),
		AuthFailures: counter("auth_failures_total",
			"Long-connection handshakes the server refused on the credentials themselves (WeCom errcode 40001 / 40013). The bot stays down until somebody fixes the installation."),
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
