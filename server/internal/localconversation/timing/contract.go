// Package timing writes the daemon's local conversation timing records. A
// record holds identifiers, timings, counts and closed-enum classifications
// only: never prompts, payloads, working directories, raw error text or
// addresses. The contract is shared with packages/core/local-conversation/timing.ts
// through testdata/contract.json.
package timing

import (
	"regexp"
	"strings"
)

// Version is the record schema this package writes.
const Version = 2

// Source values. A daemon writes its own records; mobilelink and a phone's
// reports write SourceMobile.
const (
	SourceDaemon = "daemon"
	SourceMobile = "mobile"
)

// Event values.
const (
	EventStart = "start"
	EventEnd   = "end"
	EventMark  = "mark"
)

// Send-path marks this daemon emits.
const (
	StageDaemonRequestReceived  = "daemon.request_received"
	StageQueueDispatch          = "queue.dispatch"
	StageTurnPersist            = "turn.persist"
	StageRunnerCreate           = "runner.create"
	StageRunnerLimiter          = "runner.limiter"
	StagePrepareWait            = "prepare.wait"
	StageProviderSpawn          = "provider.spawn"
	StageProviderHandshake      = "provider.handshake"
	StageProviderConfig         = "provider.config"
	StageContextBind            = "context.bind"
	StageModelResolve           = "model.resolve"
	StageThreadPrepare          = "thread.prepare"
	StageProviderTurnStart      = "provider.turn_start"
	StageAdapterSpawn           = "adapter.spawn"
	StageAdapterReady           = "adapter.ready"
	StageSDKLoad                = "sdk.load"
	StageQueryInit              = "query.init"
	StageProviderInputQueued    = "provider.input_queued"
	StageProviderFirstMessage   = "provider.first_message"
	StageProviderFirstReasoning = "provider.first_reasoning"
	StageProviderFirstText      = "provider.first_text"
	StageProviderFirstTool      = "provider.first_tool"
	StageBatcherFirstOutput     = "batcher.first_output_commit"
	StageHubFirstOutputPublish  = "hub.first_output_publish"
	StageWSFirstOutputWrite     = "ws.first_output_write"
	StageTurnFinished           = "turn.finished"
	StageSpareClaim             = "spare.claim"
	StageMCPSet                 = "mcp.set"
	StageFocusChanged           = "focus.changed"
	StagePreheatRequested       = "preheat.requested"
	StagePreheatCancelled       = "preheat.cancelled"
	StagePreheatStarted         = "preheat.started"
	StagePreheatFinished        = "preheat.finished"
	StagePreheatFailed          = "preheat.failed"
	StageSpareCreated           = "spare.created"
	StageSpareClaimed           = "spare.claimed"
	StageSpareRejected          = "spare.rejected"
	StageSpareDiscarded         = "spare.discarded"
	StageRunnerEvicted          = "runner.evicted"
	StageTimingDropped          = "timing.dropped"
	StageProviderFirstFrame     = "provider.first_frame"
	StageProviderExit           = "provider.exit"
	StageProviderSessionReady   = "provider.session_ready"
	StageTurnOutputSummary      = "turn.output_summary"

	StageLinkStart        = "link.start"
	StageLinkReady        = "link.ready"
	StageLinkRegistered   = "link.registered"
	StageLinkStop         = "link.stop"
	StageRegistryFetch    = "registry.fetch"
	StagePeerKnown        = "peer.known"
	StagePeerMDNSFound    = "peer.mdns_found"
	StagePeerConnect      = "peer.connect"
	StageRelayReserve     = "relay.reserve"
	StageRelayConnected   = "relay.connected"
	StageRelayRefused     = "relay.refused"
	StageHolepunchStart   = "holepunch.start"
	StageHolepunchSuccess = "holepunch.success"
	StageHolepunchFailed  = "holepunch.failed"
	StageConnectionDirect = "connection.direct"
	StageConnectionRelay  = "connection.relayed"
	StageConnectionLost   = "connection.lost"
	StageNetworkChanged   = "network.changed"

	StageTunnelConn          = "tunnel.conn"
	StageTunnelStreamOpen    = "tunnel.stream_open"
	StageProxyRequest        = "proxy.request"
	StageProxyFirstByte      = "proxy.first_byte"
	StageMigrateStart        = "migrate.start"
	StageMigrateStreamClosed = "migrate.stream_closed"
	StageMigrateReconnected  = "migrate.reconnected"
	StageMigrateResubscribed = "migrate.resubscribed"

	StageLinkRequestSent      = "link.request_sent"
	StageLinkRequestReceived  = "link.request_received"
	StageLinkResponseSent     = "link.response_sent"
	StageLinkResponseReceived = "link.response_received"
	StageLinkEventSent        = "link.event_sent"
	StageSnapshotServerRead   = "snapshot.server_read"
)

// Role values: which side of a hole punch or a proxied request a record is from.
const (
	RoleInitiator = "initiator"
	RoleReceiver  = "receiver"
	RoleExecutor  = "executor"
)

// AddrType values. A record never holds the address itself.
const (
	AddrLoopback = "loopback"
	AddrLAN      = "lan"
	AddrPublic   = "public"
	AddrRelay    = "relay"
	AddrUnknown  = "unknown"
)

// reportable is what a phone may report: its own client marks and its own link.
// Anything about the turn itself is the executor's to say.
var reportable = setOf(
	"client.send", "client.pending_shown", "client.request", "client.first_event_received", "client.first_event_applied", "client.first_commit", "client.terminal_applied",
	StageLinkStart, StageLinkReady, StageLinkRegistered, StageLinkStop, StageRegistryFetch, StagePeerKnown,
	StagePeerConnect, StageRelayReserve, StageRelayConnected, StageRelayRefused, StageHolepunchStart, StageHolepunchSuccess, StageHolepunchFailed,
	StageConnectionDirect, StageConnectionRelay, StageConnectionLost, StageNetworkChanged,
	StageTunnelConn, StageTunnelStreamOpen, StageProxyRequest, StageProxyFirstByte,
	"ws.hello", "ws.open", "ws.subscribe", "ws.subscribed", "ws.catch_up", "ws.closed", "ws.retry", "ws.resubscribed",
	StageMigrateStart, StageMigrateStreamClosed, StageMigrateReconnected,
	StageLinkRequestSent, StageLinkResponseReceived, "link.event_received",
	"snapshot.request", "snapshot.first_byte", "snapshot.received", "snapshot.parsed", "snapshot.rendered",
	StageTimingDropped,
)

// Reportable reports whether a phone may report stage.
func Reportable(stage string) bool { return reportable[stage] }

// Origin values: where a request came from.
const (
	OriginLocal   = "local"
	OriginP2P     = "p2p"
	OriginUnknown = "unknown"
)

// ClientKind values.
const (
	ClientDesktop = "desktop"
	ClientMobile  = "mobile"
	ClientUnknown = "unknown"
)

// Transport values: what carries a direct connection.
const (
	TransportQUIC    = "quic"
	TransportTCP     = "tcp"
	TransportRelay   = "relay"
	TransportUnknown = "unknown"
)

// Connection values: the stream's own connection, never device-level state.
const (
	ConnectionLoopback = "loopback"
	ConnectionDirect   = "direct"
	ConnectionRelayed  = "relayed"
	ConnectionUnknown  = "unknown"
)

// PrepareKind values.
const (
	PrepareHot       = "hot"
	PreparePreheated = "preheated"
	PrepareSpare     = "spare"
	PrepareCold      = "cold"
)

// TurnKind values. Background and native turns stay out of p50/p90 summaries.
const (
	TurnUser       = "user"
	TurnQueued     = "queued"
	TurnSteer      = "steer"
	TurnBackground = "background"
	TurnNative     = "native"
	TurnAgent      = "agent"
)

// Reason values.
const (
	ReasonStart            = "start"
	ReasonResume           = "resume"
	ReasonSkipped          = "skipped"
	ReasonLive             = "live"
	ReasonCatchUp          = "catch_up"
	ReasonConcurrencyLimit = "concurrency_limit"

	ReasonFocused        = "focused"
	ReasonUnfocused      = "unfocused"
	ReasonConnectionLost = "connection_lost"

	ReasonNotFocused = "not_focused"
	ReasonDraining   = "draining"
	ReasonDisabled   = "disabled"
	ReasonNoSession  = "no_session"
	ReasonArchived   = "archived"

	ReasonClaimRefused       = "claim_refused"
	ReasonOptionNotApplied   = "option_not_applied"
	ReasonSettingsNotApplied = "settings_not_applied"
	ReasonMCPFailed          = "mcp_failed"
	ReasonBindFailed         = "bind_failed"
	ReasonUnsupported        = "unsupported"

	ReasonMaxAge             = "max_age"
	ReasonConfigChanged      = "config_changed"
	ReasonCLIChanged         = "cli_changed"
	ReasonEnvironmentChanged = "environment_changed"
	ReasonNoClients          = "no_clients"
	ReasonExited             = "exited"
	ReasonStopped            = "stopped"

	ReasonIdlePreheated = "idle_preheated"
	ReasonIdleSent      = "idle_sent"
	ReasonOverLimit     = "over_limit"

	// Where model.resolve got its answer. "resolving" and "failed" read from the
	// record's event and outcome.
	ReasonExplicit      = "explicit"
	ReasonConfig        = "config"
	ReasonCatalog       = "catalog"
	ReasonCatalogCached = "catalog_cached"
	ReasonNativeDefault = "native_default"

	ReasonBackground = "background"
	ReasonSlow       = "slow"
	ReasonLateDirect = "late_direct"

	// Why a device connection closed: this device's decision, or anything else.
	ReasonPathChanged      = "path_changed"
	ReasonProbeFailed      = "probe_failed"
	ReasonConnectionClosed = "connection_closed"

	// Why a relay refused; its own text can name addresses.
	ReasonReservationRefused = "reservation_refused"
	ReasonResourceLimit      = "resource_limit"
	ReasonPermissionDenied   = "permission_denied"
	ReasonConnectionFailed   = "connection_failed"

	// Why a WebSocket closed.
	ReasonWSNormal           = "ws_normal"
	ReasonWSGoingAway        = "ws_going_away"
	ReasonWSAbnormal         = "ws_abnormal"
	ReasonWSProtocol         = "ws_protocol"
	ReasonWSHandshakeTimeout = "ws_handshake_timeout"
)

var (
	stages = setOf(
		"gui.submit", "gui.first_output_applied", "gui.terminal_applied", "renderer.request", "renderer.first_output_received", "main.request",
		"queue.context", "turn.execute", "turn.result", "attachments.resolve",
		"context.prepare", "context.credentials", "context.mcp", "provider.lookup",
		"provider.sdk_ready", "provider.refresh", "provider.input_written", "provider.input_yielded",
		"provider.pool_admission", "provider.pool_wait", "provider.pool_create", "provider.pool_reused", "provider.bridge",
		"provider.thread_wait", "provider.thread_ready", "provider.thread_unload",
		"provider.first_stdout", "provider.first_stderr", "provider.first_event",
		"provider.first_output", "provider.tool", "provider.result", "provider.failure", "provider.retry",
		"title.generate", "title.persist", "user.question", "images.save", "context.dispose", "images.recover", "turn.save",

		"client.send", "client.pending_shown", "client.request",
		StageDaemonRequestReceived, StageQueueDispatch, StageTurnPersist,
		StageRunnerCreate, StageRunnerLimiter, StagePrepareWait,
		StageBatcherFirstOutput, StageHubFirstOutputPublish, StageWSFirstOutputWrite,
		"client.first_event_received", "client.first_event_applied", "client.first_commit",
		StageTurnFinished, "client.terminal_applied",
		StageProviderSpawn, StageProviderHandshake, StageProviderConfig, StageContextBind, StageModelResolve, StageThreadPrepare,
		StageProviderTurnStart, "provider.first_frame", StageProviderFirstReasoning, StageProviderFirstText, StageProviderFirstTool, "provider.exit",
		StageAdapterSpawn, StageAdapterReady, StageSDKLoad, StageQueryInit, StageSpareClaim, StageMCPSet,
		StageProviderInputQueued, StageProviderFirstMessage, "provider.session_ready", "provider.first_thinking",
		StageFocusChanged, StagePreheatRequested, StagePreheatCancelled, StagePreheatStarted, StagePreheatFinished, StagePreheatFailed,
		StageSpareCreated, StageSpareClaimed, StageSpareRejected, StageSpareDiscarded, StageRunnerEvicted,
		"link.start", "link.ready", "link.registered", "link.stop",
		"registry.fetch", "peer.known", "peer.mdns_found",
		"peer.connect", "relay.reserve", "relay.connected", "relay.refused",
		"holepunch.start", "holepunch.success", "holepunch.failed",
		"connection.direct", "connection.relayed", "connection.lost", "network.changed",
		"tunnel.conn", "tunnel.stream_open", "proxy.request", "proxy.first_byte",
		"ws.ticket", "ws.hello", "ws.open", "ws.subscribe", "ws.subscribed", "ws.catch_up",
		"migrate.start", "migrate.stream_closed", "migrate.reconnected", "migrate.resubscribed",
		"ws.closed", "ws.retry", "ws.resubscribed",
		"link.request_sent", "link.request_received", "link.response_sent", "link.response_received", "link.event_sent", "link.event_received",
		"snapshot.request", "snapshot.server_read", "snapshot.first_byte", "snapshot.received", "snapshot.parsed", "snapshot.rendered",
		StageTurnOutputSummary,
		StageTimingDropped,
	)
	sources      = setOf(SourceDaemon, SourceMobile)
	roles        = setOf(RoleInitiator, RoleReceiver, RoleExecutor)
	addrTypes    = setOf(AddrLoopback, AddrLAN, AddrPublic, AddrRelay, AddrUnknown)
	origins      = setOf(OriginLocal, OriginP2P, OriginUnknown)
	clientKinds  = setOf(ClientDesktop, ClientMobile, ClientUnknown)
	transports   = setOf(TransportQUIC, TransportTCP, TransportRelay, TransportUnknown)
	connections  = setOf(ConnectionLoopback, ConnectionDirect, ConnectionRelayed, ConnectionUnknown)
	providers    = setOf("codex", "claude")
	prepareKinds = setOf(PrepareHot, PreparePreheated, PrepareSpare, PrepareCold)
	turnKinds    = setOf(TurnUser, TurnQueued, TurnSteer, TurnBackground, TurnNative, TurnAgent)
	reasons      = setOf(ReasonStart, ReasonResume, ReasonSkipped, ReasonLive, ReasonCatchUp, ReasonConcurrencyLimit,
		"provider_unavailable", "provider_error", "provider_exited", "outcome_unknown",
		"storage_limit", "compaction_failed", "interrupt_unconfirmed", "remote_failed",
		ReasonFocused, ReasonUnfocused, ReasonConnectionLost,
		ReasonNotFocused, ReasonDraining, ReasonDisabled, ReasonNoSession, ReasonArchived,
		ReasonClaimRefused, ReasonOptionNotApplied, ReasonSettingsNotApplied, ReasonMCPFailed, ReasonBindFailed, ReasonUnsupported,
		ReasonMaxAge, ReasonConfigChanged, ReasonCLIChanged, ReasonEnvironmentChanged, ReasonNoClients, ReasonExited, ReasonStopped,
		ReasonIdlePreheated, ReasonIdleSent, ReasonOverLimit,
		ReasonExplicit, ReasonConfig, ReasonCatalog, ReasonCatalogCached, ReasonNativeDefault,
		ReasonBackground, ReasonSlow, ReasonLateDirect, ReasonPathChanged, ReasonProbeFailed, ReasonConnectionClosed,
		ReasonReservationRefused, ReasonResourceLimit, ReasonPermissionDenied, ReasonConnectionFailed,
		ReasonWSNormal, ReasonWSGoingAway, ReasonWSAbnormal, ReasonWSProtocol, ReasonWSHandshakeTimeout)
	outcomes = setOf("completed", "failed", "cancelled")
	failures = setOf("rate_limit", "authentication", "transport", "cancelled", "other")
	events   = setOf(EventStart, EventEnd, EventMark, "wait")

	identifier = regexp.MustCompile(`^[\w:.-]{1,160}$`)
	atPattern  = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$`)

	rateLimited   = regexp.MustCompile(`(?i)(?:^|[^0-9a-z])429(?:[^0-9a-z]|$)|rate.?limit|insufficient|quota|余额不足`)
	unauthorized  = regexp.MustCompile(`(?i)(?:^|[^0-9a-z])40[13](?:[^0-9a-z]|$)|unauthorized|authentication|not signed in`)
	cancelledText = regexp.MustCompile(`(?i)abort|cancel`)
	transportText = regexp.MustCompile(`(?i)websocket|disconnect|ECONN|ETIMEDOUT|fetch failed|timeout|timed out|deadline exceeded`)
)

func setOf(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}

// Record is one line of daemon-timing.jsonl. Field names follow the
// camelCase of the existing timing records so one parser reads both.
type Record struct {
	Version        int    `json:"version"`
	Source         string `json:"source"`
	At             string `json:"at"`
	Stage          string `json:"stage"`
	Event          string `json:"event"`
	ElapsedMs      int64  `json:"elapsedMs"`
	ConversationID string `json:"conversationId,omitempty"`
	RequestID      string `json:"requestId,omitempty"`
	TurnID         string `json:"turnId,omitempty"`
	Provider       string `json:"provider,omitempty"`
	Background     bool   `json:"background,omitempty"`
	DurationMs     *int64 `json:"durationMs,omitempty"`
	Outcome        string `json:"outcome,omitempty"`
	Failure        string `json:"failure,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Origin         string `json:"origin,omitempty"`
	ClientKind     string `json:"clientKind,omitempty"`
	Connection     string `json:"connection,omitempty"`
	PrepareKind    string `json:"prepareKind,omitempty"`
	TurnKind       string `json:"turnKind,omitempty"`
	TurnIndex      *int64 `json:"turnIndex,omitempty"`
	Attempt        *int64 `json:"attempt,omitempty"`
	PrepareID      string `json:"prepareId,omitempty"`
	Count          *int64 `json:"count,omitempty"`
	// Bytes and MaxGapMs summarize traffic, one record per turn or snapshot.
	Bytes    *int64 `json:"bytes,omitempty"`
	MaxGapMs *int64 `json:"maxGapMs,omitempty"`
	// Peer is the other device, hashed at export.
	Peer      string `json:"peer,omitempty"`
	Role      string `json:"role,omitempty"`
	AddrType  string `json:"addrType,omitempty"`
	Transport string `json:"transport,omitempty"`
	Reused    bool   `json:"reused,omitempty"`
	// Reporter and ReceivedAt are stamped by the executor on a phone's report:
	// who sent it, on the executor's clock, because the phone's own can be wrong.
	Reporter   string `json:"reporter,omitempty"`
	ReceivedAt string `json:"receivedAt,omitempty"`
}

// Sanitize returns r with unknown closed-enum values dropped, or false when
// r cannot be a valid record: an unknown stage or an identifier that could
// carry anything but an opaque id.
func Sanitize(r Record) (Record, bool) {
	if r.Version != Version || !sources[r.Source] || !stages[r.Stage] || !events[r.Event] || !atPattern.MatchString(r.At) || r.ElapsedMs < 0 {
		return Record{}, false
	}
	for _, id := range []string{r.ConversationID, r.RequestID, r.TurnID} {
		if id != "" && !identifier.MatchString(id) {
			return Record{}, false
		}
	}
	r.Provider = closed(providers, r.Provider)
	r.Outcome = closed(outcomes, r.Outcome)
	r.Failure = closed(failures, r.Failure)
	r.Reason = closed(reasons, r.Reason)
	r.Origin = closed(origins, r.Origin)
	r.ClientKind = closed(clientKinds, r.ClientKind)
	r.Connection = closed(connections, r.Connection)
	r.PrepareKind = closed(prepareKinds, r.PrepareKind)
	r.TurnKind = closed(turnKinds, r.TurnKind)
	if r.PrepareID != "" && !identifier.MatchString(r.PrepareID) {
		r.PrepareID = ""
	}
	r.DurationMs = nonNegative(r.DurationMs)
	r.TurnIndex = nonNegative(r.TurnIndex)
	r.Attempt = nonNegative(r.Attempt)
	r.Count = nonNegative(r.Count)
	r.Bytes = nonNegative(r.Bytes)
	r.MaxGapMs = nonNegative(r.MaxGapMs)
	r.Role = closed(roles, r.Role)
	r.AddrType = closed(addrTypes, r.AddrType)
	r.Transport = closed(transports, r.Transport)
	if r.Peer != "" && !identifier.MatchString(r.Peer) {
		r.Peer = ""
	}
	if r.Reporter != "" && !identifier.MatchString(r.Reporter) {
		r.Reporter = ""
	}
	if r.ReceivedAt != "" && !atPattern.MatchString(r.ReceivedAt) {
		r.ReceivedAt = ""
	}
	return r, true
}

func closed(set map[string]bool, value string) string {
	if set[value] {
		return value
	}
	return ""
}

func nonNegative(v *int64) *int64 {
	if v == nil || *v < 0 {
		return nil
	}
	return v
}

// Classify maps error text to a closed category without keeping the text,
// matching localTimingFailure on the TypeScript side.
func Classify(message string) string {
	switch {
	case rateLimited.MatchString(message):
		return "rate_limit"
	case unauthorized.MatchString(message):
		return "authentication"
	case cancelledText.MatchString(message):
		return "cancelled"
	case transportText.MatchString(message):
		return "transport"
	}
	return "other"
}

// IsModelOutput reports whether a committed event carries model output, as
// opposed to the user message and lifecycle events every turn commits first.
func IsModelOutput(eventType, kind string) bool {
	if !strings.HasPrefix(eventType, "item.") {
		return false
	}
	switch kind {
	case "", "error", "background_task":
		return false
	}
	return true
}
