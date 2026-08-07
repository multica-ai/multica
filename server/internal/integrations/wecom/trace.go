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
// aibot_subscribe body, which traceOut never descends into.

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

// traceOut records a frame on its way to WeCom. Called from wsSender.write,
// the one place that writes the socket, so nothing can be sent without
// appearing here.
//
// It reads named fields rather than dumping the frame: the aibot_subscribe
// body carries the smart-bot secret, and a wholesale dump would put it in the
// log. Add a field here only after checking what every cmd puts under it.
func traceOut(log *slog.Logger, frame map[string]any) {
	if !tracingOn() || log == nil {
		return
	}
	attrs := []any{"dir", "out"}
	if cmd, ok := frame["cmd"].(string); ok {
		attrs = append(attrs, "cmd", cmd)
	}
	if h, ok := frame["headers"].(frameHeaders); ok && h.ReqID != "" {
		attrs = append(attrs, "req_id", h.ReqID)
	}
	if body, ok := frame["body"].(map[string]any); ok {
		if v, ok := body["chatid"].(string); ok {
			attrs = append(attrs, "chatid", v)
		}
		if v, ok := body["chat_type"].(int); ok {
			attrs = append(attrs, "chat_type", v)
		}
		if v, ok := body["msgtype"].(string); ok {
			attrs = append(attrs, "msgtype", v)
		}
		if md, ok := body["markdown"].(map[string]string); ok {
			attrs = append(attrs, "len", len(md["content"]), "text", tracePreview(md["content"]))
		}
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
