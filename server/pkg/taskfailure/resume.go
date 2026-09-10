package taskfailure

import (
	"regexp"
	"strings"
)

// UnresumableHistory reports whether an agent error means the conversation
// history itself can no longer be sent to the provider: a message already
// baked into the transcript carries empty content, so every resume of that
// session replays the same body and reproduces the same rejection.
//
// This predicate is deliberately provider-agnostic, and that is the whole
// point. The original detector paired "400" with "invalid_request_error"
// (see classifyPoisonedError in internal/daemon), which is the Anthropic wire
// shape. The identical defect is reported by other providers with neither
// token present:
//
//	Invalid request: the message at position 37 with role 'assistant' must not be empty   (GH #6066)
//	provider.api_error: 400 the message at position 43 with role 'assistant' must not be empty  (GH #5760)
//	messages.37: all messages must have non-empty content ...                             (Anthropic)
//	messages[43].content: content must not be empty
//
// Keying on a status code or a provider name means missing the next backend
// that words it differently — Multica supports 17 of them and holds only an
// opaque session id, so it cannot know which CLIs write a truncated
// transcript. What all of these DO state is the two things that define the
// defect: that some content is empty, and which message in the history it
// belongs to.
//
// Both signals are required, and that is what keeps the predicate narrow. A
// tool reporting "commit message must not be empty" has no message locator
// and does not match; a diff mentioning "messages[3]" without an emptiness
// complaint does not match either. Erring toward NOT matching is the safe
// direction: a miss leaves today's behaviour (the task fails and the user
// retries), while a false positive would discard a healthy session pointer
// and lose conversation context.
func UnresumableHistory(errText string) bool {
	if errText == "" {
		return false
	}
	return emptyContentRe.MatchString(errText) && historyMessageLocatorRe.MatchString(errText)
}

// AuthMethodUnresolved reports whether an agent error is the provider
// SDK refusing to resolve its own credentials — no api_key, no auth_token, and
// no explicitly-omitted auth header. On a RESUMED session this is
// deterministic rather than transient: the credential-bearing provider
// identity lives in the session state the runtime rebuilt, so replaying the
// same session reproduces the same error forever. A fresh session re-resolves
// the provider from current config and succeeds (GH #6777).
//
// Deliberately the exact provider phrase and nothing looser. Every other
// authentication-shaped error — an expired token, a revoked key, a 401 — is
// about the credential itself and is NOT cured by a new session, so widening
// this would discard healthy conversation pointers on failures a retry cannot
// fix. Classify leaves this text as agent_error.unknown (resume-safe), which
// is why the guard has to key on the text rather than the reason.
//
// This is the single source of truth for the phrase. Keep it in sync with the
// GetLastTaskSession / GetLastChatTaskSession resume queries (pkg/db/queries),
// which apply the same guard server-side so rows written by a daemon too old
// to carry the in-turn retry are still excluded from resume.
func AuthMethodUnresolved(errText string) bool {
	if errText == "" {
		return false
	}
	return strings.Contains(strings.ToLower(errText), authMethodUnresolvedPhrase)
}

// authMethodUnresolvedPhrase is the lowercase provider phrase
// AuthMethodUnresolved matches. It appears verbatim in the runtime's error
// however the failure reached us — session/resume, session/set_model or
// session/prompt — because every ACP adapter wraps the underlying message
// with %v rather than replacing it.
const authMethodUnresolvedPhrase = "could not resolve authentication method"

// emptyContentRe matches the provider's complaint that a content field is
// empty, in the wordings observed across providers.
var emptyContentRe = regexp.MustCompile(`(?i)must not be empty|must be non-?empty|must have non-?empty|non-?empty content|cannot be empty|should not be empty`)

// historyMessageLocatorRe matches the part of the error that points at a
// message inside the conversation history — a role name, an index, or a
// position. Without one of these an emptiness complaint is about some other
// field entirely and says nothing about the transcript.
//
// Keep in sync with the equivalent regex in the GetLastTaskSession /
// GetLastChatTaskSession resume queries (pkg/db/queries), which apply the same
// text guard server-side for rows an older daemon classified as
// agent_error.unknown.
var historyMessageLocatorRe = regexp.MustCompile(`(?i)role[^a-z0-9]{0,2}assistant|assistant message|message at position|messages\.[0-9]|messages\[[0-9]`)
