package wecom

// strings.go — the copy the streaming bubble writes, in one place, selected
// per READER.
//
// IT CARRIES ONLY THE BUBBLE'S COPY. Everything else this adapter says — the
// offline and archived notices, the binding prompt, the inbox card's labels —
// is still the Chinese literal at its own call site, exactly as before. Those
// move here when they are translated; nothing about this file asks them to
// move first.
//
// Which pack a given bubble uses is decided by the DESTINATION, not by the
// installation (language.go): a 1:1 gets that person's Multica profile
// language, and a room — where there is no shared profile and no member list —
// gets the deployment's own language.
//
// Slack's adapter hardcodes English and Lark's hardcodes Chinese, so there is
// no house i18n mechanism to join. This is deliberately not one either: a
// struct of strings per locale. No catalogue files, no message ids, no plural
// rules. If wecom ever needs a third language with real formatting rules,
// that is the moment to reach for a framework — not now.
//
// The zh-Hans pack is the text a Chinese tenant reads; the English pack is for
// a reader whose Multica profile says anything else.

import (
	"strings"
	"sync/atomic"
)

// Locale names the language an installation's users are answered in.
type Locale string

const (
	LocaleZhHans Locale = "zh-Hans"
	LocaleEn     Locale = "en"

	// DefaultLocale is the compile-time fallback: Chinese, because WeCom
	// is a Chinese platform. It is what a deployment that says nothing gets.
	// Read deploymentLocale() rather than this — a deployment can say
	// otherwise, and a room's language is a property of the deployment, not of
	// whichever person happened to speak.
	DefaultLocale = LocaleZhHans
)

// deploymentLocaleValue is the language this server answers in when the reader
// is a room, or a person whose profile says nothing. Set once at boot from
// MULTICA_WECOM_DEFAULT_LOCALE (cmd/server/router.go) and read on every
// message, so it is an atomic rather than a plain var: -race would otherwise
// flag the boot write against the first inbound frame.
//
// It exists because the alternative answers are all worse. A hardcoded
// constant makes an English-speaking tenant's rooms Chinese with no way out
// but a rebuild. An installation-level column was tried and removed: nothing
// could write it. Borrowing the installer's personal profile language repeats,
// with a different person, the exact bug that motivated this — one member's
// setting deciding what a whole room reads. A deployment-level knob is the
// smallest thing that is actually about the deployment.
var deploymentLocaleValue atomic.Value

// SetDeploymentLocale fixes the deployment's language from a raw config string
// and returns what it resolved to, so the caller can log it. Anything
// unrecognised — including empty — leaves the current value in place: a typo in
// an env var must not silently switch a tenant's language.
//
// Deliberately NOT resolveLocale. That one reads a user's profile field, which
// the API has already validated to en / zh-Hans / ko / ja, so it can treat
// "anything that isn't Chinese" as a deliberate choice of the English pack. An
// env var has been validated by nobody: under that rule
// MULTICA_WECOM_DEFAULT_LOCALE=zh_Hant, or a stray quote, would quietly put a
// Chinese tenant's rooms into English. So this one matches exactly, and an
// operator who mistypes gets the old language and a log line, not a surprise.
func SetDeploymentLocale(raw string) Locale {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "zh-hans", "zh":
		deploymentLocaleValue.Store(LocaleZhHans)
		return LocaleZhHans
	case "en":
		deploymentLocaleValue.Store(LocaleEn)
		return LocaleEn
	default:
		return deploymentLocale()
	}
}

// deploymentLocale is the configured language, or DefaultLocale before
// anything has configured one — which is also what every test sees.
func deploymentLocale() Locale {
	if v, ok := deploymentLocaleValue.Load().(Locale); ok {
		return v
	}
	return DefaultLocale
}

// resolveLocale maps a user's profile language onto a supported Locale. The
// profile validates to en / zh-Hans / ko / ja (handler/auth.go), and there are
// two packs: Chinese for zh*, English for everyone who chose anything else —
// a ko or ja user deliberately picked "not Chinese", and English is the
// lingua-franca pack we have for them. Only an EMPTY value falls back to
// deploymentLocale: absence is not a choice.
func resolveLocale(s string) Locale {
	switch v := strings.ToLower(strings.TrimSpace(s)); {
	case v == "":
		return deploymentLocale()
	case strings.HasPrefix(v, "zh"):
		return LocaleZhHans
	default:
		return LocaleEn
	}
}

// copyPack is the set of user-visible strings one locale's bubble is written
// in. Everything the bubble can say is a field here; nothing is built by
// concatenating fragments elsewhere.
type copyPack struct {
	// The ways a streaming reply ends in something other than an answer. Each
	// one closes the loading bubble the question opened, so each one has to
	// carry visible text — WeCom discards a closing frame it considers empty
	// and the bubble spins on forever (see hasVisibleChar in ws_frame.go).
	//
	// StreamNoReply — the agent finished with nothing to say.
	// StreamNoReplyWithFiles — the agent finished with no words but produced
	//   files, which arrive as separate messages right after this one.
	//   Distinct from StreamNoReply because that copy says nothing is coming,
	//   and then something arrives: a bubble that contradicts the next message
	//   reads as a bug even though both halves are working.
	// StreamMerged — a QUEUED round's run finished with nothing of its own to
	//   say; the reply ahead of it already covered this message. A first
	//   round's empty finish keeps StreamNoReply, which has no earlier answer
	//   to point at.
	// StreamNotStarted — no run was triggered at all (agent offline or
	//   archived, or the enqueue failed); the replier's own notice follows as
	//   a separate message with the detail.
	// StreamFailed — the run failed, and the platform published no reason of
	//   its own. A failure that DID carry one says that instead (failureText
	//   in typing_indicator.go).
	// StreamCancelled — the user stopped the run, so no answer is coming.
	//   Separate copy from StreamFailed on purpose: inviting a retry of
	//   something somebody just stopped on purpose reads as the bot not having
	//   noticed.
	StreamNoReply          string
	StreamNoReplyWithFiles string
	StreamMerged           string
	StreamNotStarted       string
	StreamFailed           string
	StreamCancelled        string
}

// copyFor returns the pack for a locale, falling back to the deployment's.
func copyFor(l Locale) copyPack {
	if pack, ok := copyPacks[l]; ok {
		return pack
	}
	return copyPacks[deploymentLocale()]
}

var copyPacks = map[Locale]copyPack{
	LocaleZhHans: {
		StreamNoReply:          "（这轮没有需要回复的内容）",
		StreamNoReplyWithFiles: "（这轮没有文字回复，附件在下面）",
		StreamMerged:           "✅ 这条已并入上一条回复一起处理了。",
		StreamNotStarted:       "已收到，但这条暂时没能开始处理。",
		StreamFailed:           "⚠️ 这次没跑通，请稍后再试一次。",
		StreamCancelled:        "⏹️ 这次处理已取消。",
	},
	LocaleEn: {
		StreamNoReply:          "(nothing to reply with this round)",
		StreamNoReplyWithFiles: "(no text this round — the files follow)",
		StreamMerged:           "✅ Handled together with my previous reply.",
		StreamNotStarted:       "Got it, but this one couldn't start processing.",
		StreamFailed:           "⚠️ That run didn't go through. Please try again.",
		StreamCancelled:        "⏹️ That run was cancelled.",
	},
}
