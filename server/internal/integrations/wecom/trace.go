package wecom

// trace.go — an opt-in record of every frame this adapter reads and writes.
//
// Why it exists: nothing else in the package logs a frame. Failures log, and
// only failures — a bad envelope, a non-zero server ack. A run that goes
// wrong QUIETLY leaves no trace on the server at all: the reply addressed to
// the room instead of the person, the command that was silently dropped, the
// receipt that went out twice. Verifying those today needs someone to
// describe what they saw on a phone, which is slow, lossy, and impossible for
// anything about ordering or timing.
//
// With MULTICA_WECOM_TRACE=1 the server records enough to check a real-device
// session afterwards: which way the frame went, what chat it was addressed
// to, whether that chat is a room or a person, and what the server said back.
//
// Why not slog.Debug, which the siblings use for their per-frame lines
// (slack_channel.go:162, lark/ws_connector.go:339): logger.parseLevel
// defaults LOG_LEVEL to *debug*, so a Debug call is on in every deployment
// that has not set LOG_LEVEL. Message text must not be logged by default, so
// the switch has to be its own, and default to off.
//
// Off is the right state outside a test session. What this records includes a
// bounded prefix of message text, because "which of the copy strings was
// that" cannot be answered from lengths alone — so it is message content, in
// a log, and it should be turned on deliberately and turned off after. The
// prefix is capped rather than full, the cap counts runes so a Chinese
// message is not cut mid-character, bearer tokens are redacted out of it, and
// nothing here reads a credential field: the bot secret rides in the
// aibot_subscribe body, which traceOutFields never descends into.

import (
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"
)

// tracing is set once at boot and read on every frame.
var tracing atomic.Bool

// SetTrace turns frame tracing on or off. Called from the server wiring with
// MULTICA_WECOM_TRACE; returns what it set so the caller can log it.
func SetTrace(on bool) bool {
	tracing.Store(on)
	return on
}

// tracingOn reports whether frames are being recorded.
func tracingOn() bool { return tracing.Load() }

// tracePreviewRunes bounds what of a message body reaches the log. Long
// enough to tell one copy string from another and to see which language it is
// in; short enough that a transcript is not reconstructable from the log.
const tracePreviewRunes = 120

// tracePreview returns a bounded, single-line prefix of s with any bearer
// token redacted. Newlines become spaces so one frame stays one log line, and
// the cut is on a rune boundary.
//
// The redaction is not optional. OutboundReplier.sendBindingPrompt builds
// "👋 请先绑定你的 Multica 账号，才能与我对话：\n" + appURL + "/wecom/bind?token=" +
// a 43-character token, and with a normal MULTICA_APP_URL the token's last
// character lands at rune 107-112 — inside the cap. Without this, turning
// tracing on for a debugging session would log live binding credentials in
// full. A binding token is a bearer credential (RedeemAndBind checks only
// that the redeemer belongs to the token's workspace, and the bind page
// redeems on load as whoever is signed in), so whoever could read the log
// could bind that sender's WeCom identity to their own Multica account before
// the user clicked their own link — the same hijack replier.go:150 already
// guards against by refusing to post the link into a room.
//
// It matches on the query parameter rather than on the bind path, because
// this is a package-level function with no access to the configured path
// (OutboundReplier owns it, and BindingPath is configurable). A "token=" in a
// URL is the shape worth hiding wherever it appears.
func tracePreview(s string) string {
	s = redactBearerTokens(s)
	out := make([]rune, 0, tracePreviewRunes)
	cut := false
	for _, r := range s {
		if len(out) == tracePreviewRunes {
			cut = true
			break
		}
		if r == '\n' || r == '\r' {
			r = ' '
		}
		out = append(out, r)
	}
	if cut {
		return string(out) + "…"
	}
	return string(out)
}

// An outbound frame is recorded twice, as an attempt and an outcome sharing a
// seq: "dir=out" when it is about to go on the wire, "dir=out.done" with
// ok=true/false once SetWriteDeadline and WriteMessage have had their say.
//
// One line would not do. The trace exists to answer "did this frame actually
// reach the wire?", and a single line written before the write cannot answer
// it — a rejected frame and a delivered one look identical. A single line
// written after the write answers it only for writes that return: a write
// that hangs or a process killed mid-write leaves nothing at all, which is
// the silent failure the switch exists to remove. Two lines let a reader pair
// them by seq and see an attempt with no outcome for what it is.
//
// The pair is also why the seq exists rather than pairing on req_id alone: a
// pong echoes the server's req_id, which may be empty or repeated, so req_id
// is not a key. seq is assigned under the writer mutex and never goes on the
// wire; it is the frame's position in the wire order.

// Stages of one write, named on a failed outcome so a rejected frame is
// distinguishable from one whose deadline could not even be set.
const (
	traceStageDeadline = "set_write_deadline"
	traceStageWrite    = "write_message"
)

// outTrace is what tracing keeps about one outbound frame between extracting
// its fields and emitting its two lines. A nil outTrace means this frame is
// not being traced; both lines are governed by that single decision, so the
// log can never hold an attempt whose outcome was suppressed by the switch
// flipping halfway through a write.
type outTrace struct {
	attrs []any
	cmd   string
	reqID string
}

// traceOutFields extracts what tracing records about a frame on its way to
// WeCom. wsSender.write calls it BEFORE taking the writer mutex: this is the
// expensive half — a regexp redaction pass and a rune-by-rune cut over the
// message body — and none of it needs to be serialized with the socket. Only
// the emit does, and that is traceOutAttempt's job.
//
// It reads named fields rather than dumping the frame: the aibot_subscribe
// body carries the smart-bot secret, and a wholesale dump would put it in the
// log. Add a field here only after checking what every cmd puts under it.
func traceOutFields(log *slog.Logger, frame map[string]any) *outTrace {
	if !tracingOn() || log == nil {
		return nil
	}
	t := &outTrace{}
	if cmd, ok := frame["cmd"].(string); ok {
		t.cmd = cmd
		t.attrs = append(t.attrs, "cmd", cmd)
	}
	if h, ok := frame["headers"].(frameHeaders); ok && h.ReqID != "" {
		t.reqID = h.ReqID
		t.attrs = append(t.attrs, "req_id", h.ReqID)
	}
	if body, ok := frame["body"].(map[string]any); ok {
		if v, ok := body["chatid"].(string); ok {
			t.attrs = append(t.attrs, "chatid", v)
		}
		if v, ok := body["chat_type"].(int); ok {
			t.attrs = append(t.attrs, "chat_type", v)
		}
		if v, ok := body["msgtype"].(string); ok {
			t.attrs = append(t.attrs, "msgtype", v)
		}
		if md, ok := body["markdown"].(map[string]string); ok {
			t.attrs = append(t.attrs, "len", len(md["content"]), "text", tracePreview(md["content"]))
		}
	}
	return t
}

// traceOutAttempt records a frame at the moment it is about to be written.
// wsSender.write calls it under the writer mutex, which is the point at which
// concurrent senders — the ping loop, agent replies, inbox pushes — become
// ordered. So the order of these lines is the order the frames reach
// WriteMessage, by construction rather than by correlation.
func traceOutAttempt(log *slog.Logger, seq uint64, t *outTrace) {
	if t == nil {
		return
	}
	attrs := make([]any, 0, len(t.attrs)+4)
	attrs = append(attrs, "dir", "out", "seq", seq)
	attrs = append(attrs, t.attrs...)
	log.Info("wecom trace", attrs...)
}

// traceOutResult records what became of the attempt carrying the same seq.
// Also under the writer mutex, so an attempt and its outcome are never split
// by another goroutine's frame.
//
// The error text goes through tracePreview: it is the socket's words, not
// ours, and everything else this file writes is bounded and redacted.
func traceOutResult(log *slog.Logger, seq uint64, t *outTrace, stage string, err error) {
	if t == nil {
		return
	}
	attrs := make([]any, 0, 12)
	attrs = append(attrs, "dir", "out.done", "seq", seq)
	if t.cmd != "" {
		attrs = append(attrs, "cmd", t.cmd)
	}
	if t.reqID != "" {
		attrs = append(attrs, "req_id", t.reqID)
	}
	if err != nil {
		attrs = append(attrs, "ok", false, "stage", stage, "error", tracePreview(err.Error()))
	} else {
		attrs = append(attrs, "ok", true)
	}
	log.Info("wecom trace", attrs...)
}

// traceIn records a frame arriving from WeCom, including the server's verdict
// on something we sent — an errcode here is how a silent failure becomes
// visible. dispatchFrame only warns on a non-zero errcode for the anonymous
// ack case, so without this a rejected aibot_send_msg is the only rejection
// that shows up at all.
func traceIn(log *slog.Logger, env frameEnvelope) {
	if !tracingOn() || log == nil {
		return
	}
	log.Info("wecom trace",
		"dir", "in",
		"cmd", env.Cmd,
		"req_id", env.Headers.ReqID,
		"errcode", env.ErrCode,
		"errmsg", tracePreview(env.ErrMsg),
	)
}

// traceInbound records a decoded user message: what the adapter believes
// about who sent it and where it landed, which is exactly the pair that gets
// confused (the room's id in one field, the person's in the other). traceIn
// alone cannot show it — the callback body is still raw JSON at that point.
func traceInbound(log *slog.Logger, mc aibotMsgCallback, text string) {
	if !tracingOn() || log == nil {
		return
	}
	log.Info("wecom trace",
		"dir", "in.msg",
		"msg_id", mc.MsgID,
		"chatid", mc.ChatID,
		"chat_type", mc.ChatType,
		"sender", mc.From.UserID,
		"msgtype", mc.MsgType,
		"len", len(text),
		"text", tracePreview(text),
	)
}

// tokenParamPattern finds a token carried as a query parameter, whatever path
// it sits on. Deliberately broad: an access_token or a code is no safer in a
// log than a binding token.
var tokenParamPattern = regexp.MustCompile(`(?i)\b((?:binding_token|access_token|token|code)=)([^\s&"'<>]+)`)

// redactBearerTokens replaces the value of any token-shaped query parameter
// with a fixed marker, keeping enough of the line to be worth logging.
func redactBearerTokens(s string) string {
	if !strings.Contains(s, "=") {
		return s
	}
	return tokenParamPattern.ReplaceAllString(s, "${1}[redacted]")
}
