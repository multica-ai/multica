package wecom

// strings.go — the strings the streaming bubble says, in one place, selected
// per READER.
//
// Which pack a given message uses is decided by the DESTINATION, not by the
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
// TRIMMED TO THE BUBBLE. The pack here carries only the lines the streaming
// bubble writes — the closing copy, the progress heading and the step lines.
// Everything else this adapter says (the offline and archived notices in
// replier.go, the binding prompt, the unsupported-kind receipt, the media
// notices, the inbox card's labels) stays where it is today, as the Chinese
// constants those files already carry; moving them onto the pack is a change
// of its own, and it also brings the deployment-level knob
// (SetDeploymentLocale) that a room's language is read from. Until then
// deploymentLocale is the compile-time default.

import (
	"strings"
)

// Locale names the language an installation's users are answered in.
type Locale string

const (
	LocaleZhHans Locale = "zh-Hans"
	LocaleEn     Locale = "en"

	// DefaultLocale is the compile-time fallback: Chinese, because WeCom
	// is a Chinese platform. It is what a deployment that says nothing gets.
	// Read deploymentLocale() rather than this — a room's language is a
	// property of the deployment, not of whichever person happened to speak.
	DefaultLocale = LocaleZhHans
)

// deploymentLocale is the language this server answers in when the reader is
// a room, or a person whose profile says nothing. There is no knob for it yet
// (see the file comment), so it is the compile-time default.
func deploymentLocale() Locale {
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

// copyPack is the set of user-visible strings the bubble writes, for one
// locale. Everything the bubble can say is a field here; nothing is built by
// concatenating fragments elsewhere.
type copyPack struct {
	// The ways a streaming reply ends in something other than an answer. Each
	// one closes the loading bubble the question opened, so each one has to
	// carry visible text — WeCom discards a closing frame it considers empty
	// and the bubble spins on forever (see hasVisibleChar in ws_frame.go).
	//
	// StreamNoReply — the agent finished with nothing to say.
	// StreamMerged — a QUEUED round's run finished with nothing of its own to
	//   say; the reply ahead of it already covered this message. A first
	//   round's empty finish keeps StreamNoReply, which has no earlier answer
	//   to point at.
	// StreamNotStarted — no run was triggered at all (agent offline or
	//   archived, or the enqueue failed); the replier's own notice follows as
	//   a separate message with the detail.
	// StreamFailed — the run failed.
	// StreamCancelled — the user stopped the run, so no answer is coming.
	//   Separate copy from StreamFailed on purpose: inviting a retry of
	//   something somebody just stopped on purpose reads as the bot not having
	//   noticed.
	// StreamContinued — the run outlived one stream's window, so the bubble
	//   is sealed with this and the run carries on in a fresh bubble right
	//   underneath it (the rotation in typing_indicator.go's fireGuard). A
	//   statement of fact, not a promise: nothing is owed on the strength of
	//   it, the next bubble is already on screen.
	// StreamNoReplyWithFiles — the agent finished with no words but produced
	// files, which arrive as separate messages right after this one. Distinct
	// from StreamNoReply because that copy says nothing is coming, and then
	// something arrives: a bubble that contradicts the next message reads as a
	// bug even though both halves are working.
	//
	// There is no failure line here that names the agent or carries the
	// platform's reason. StreamFailed is what a failed run says, from
	// handleTaskFailed, and it names neither. Reinstating "failed because X,
	// handled by Y" is a product decision with a promise behind it — something
	// has to know the reason at send time — so it comes back with its sender,
	// not as strings waiting for one.
	StreamNoReply          string
	StreamNoReplyWithFiles string
	StreamMerged           string
	StreamNotStarted       string
	StreamFailed           string
	StreamCancelled        string
	StreamContinued        string

	// StreamStuck is the odd one out among the Stream* lines: it does not close
	// a bubble, it explains one that can no longer be closed. The server has
	// disowned the stream mid-run — another connection owns this conversation
	// now — so the spinner on the user's screen will turn for good and the rest
	// of the round has to arrive as new messages. Said once per bubble.
	StreamStuck string

	// StreamProgressPrefix heads the list of steps inside an open bubble. It
	// has to be there: without a heading, a list of actions sitting in a chat
	// reads as the answer arriving rather than as a status.
	StreamProgressPrefix string

	// Progress words each step the run takes. Kept in its own struct because
	// there are twenty-odd of them and they are only ever read together.
	Progress progressCopy
}

// progressCopy is what the bubble says while the run is still going: one whole
// line per kind of work the agent can be doing.
//
// Every line comes in two forms, and which one is used depends on whether the
// call named anything. The %s is the argument that identifies the work, and it
// has been through progress_render.go's own cleaning first — never a content
// block, never a control character, never longer than a few lines on a phone.
// See the two rules at the top of that file.
type progressCopy struct {
	// Read / Edit name the file; the Plain variants cover a call that names
	// no file this adapter recognises.
	Read      string
	ReadPlain string
	Edit      string
	EditPlain string

	// Command is a shell call; CommandNamed carries the command line.
	Command      string
	CommandNamed string

	// Four kinds whose plain form says only what sort of work it was. Each
	// Named variant carries the one thing that separates this call from the
	// next one: the search term, the URL, the subagent's brief, the plan.
	Search       string
	SearchNamed  string
	Web          string
	WebNamed     string
	Subtask      string
	SubtaskNamed string
	Plan         string
	PlanNamed    string

	// Service words an MCP call as "<server> · <tool>"; ServiceArgs adds the
	// call's parameters, which for an MCP tool are the only description of
	// what it is doing that this adapter can produce.
	Service     string
	ServiceArgs string

	// Skill / SkillPlain name the packaged procedure a Skill call ran. The
	// tool is always called Skill, so the skill's own name is the line.
	Skill      string
	SkillPlain string

	// Tool / ToolArgs / Fallback cover a tool this adapter has not been
	// taught. Saying something vague beats saying nothing: a step the user
	// never sees happen is indistinguishable from a run that has stalled.
	Tool     string
	ToolArgs string
	Fallback string

	// Failed marks a step that errored; FailedNamed carries the message. The
	// run may still recover, which is why the line says so either way.
	Failed      string
	FailedNamed string

	// Thinking heads the agent's own reasoning, which sits under the step
	// list. It needs a heading because without one a paragraph of prose in
	// the middle of a status block reads as the answer arriving early.
	Thinking string

	// Elapsed closes the list with how long the user has been waiting. A
	// spinner with no clock on it reads as stuck.
	Elapsed string
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
		StreamContinued:        "处理时间较长，接下一条",
		StreamStuck:            "⚠️ 上面那条进度不会再更新了，这轮的结果我用新消息发你。",

		StreamProgressPrefix: "正在处理：",
		Progress: progressCopy{
			Read:         "正在读取 %s",
			ReadPlain:    "正在读取文件",
			Edit:         "正在修改 %s",
			EditPlain:    "正在修改文件",
			Command:      "正在执行命令",
			CommandNamed: "正在执行 %s",
			Search:       "正在检索代码",
			SearchNamed:  "正在检索 %s",
			Web:          "正在查资料",
			WebNamed:     "正在查 %s",
			Subtask:      "正在派子任务",
			SubtaskNamed: "正在派子任务：%s",
			Plan:         "正在梳理计划",
			PlanNamed:    "正在梳理计划：%s",
			Service:      "正在调用 %s · %s",
			ServiceArgs:  "正在调用 %s · %s：%s",
			Skill:        "正在启用技能 %s",
			SkillPlain:   "正在启用技能",
			Tool:         "正在使用 %s",
			ToolArgs:     "正在使用 %s：%s",
			Fallback:     "正在处理",
			Failed:       "上一步出错了，正在继续",
			FailedNamed:  "上一步出错了：%s，正在继续",
			Thinking:     "思考：",
			Elapsed:      "已用时 %s",
		},
	},
	LocaleEn: {
		StreamNoReply:          "(nothing to reply with this round)",
		StreamNoReplyWithFiles: "(no text this round — the files follow)",
		StreamMerged:           "✅ Handled together with my previous reply.",
		StreamNotStarted:       "Got it, but this one couldn't start processing.",
		StreamFailed:           "⚠️ That run didn't go through. Please try again.",
		StreamCancelled:        "⏹️ That run was cancelled.",
		StreamContinued:        "Still working, continued below",
		StreamStuck:            "⚠️ The status above won't update any further. I'll send this round's result as a new message.",

		StreamProgressPrefix: "Working on it: ",
		Progress: progressCopy{
			Read:         "Reading %s",
			ReadPlain:    "Reading a file",
			Edit:         "Editing %s",
			EditPlain:    "Editing a file",
			Command:      "Running a command",
			CommandNamed: "Running %s",
			Search:       "Searching the code",
			SearchNamed:  "Searching for %s",
			Web:          "Looking things up",
			WebNamed:     "Looking up %s",
			Subtask:      "Handing off a subtask",
			SubtaskNamed: "Handing off a subtask: %s",
			Plan:         "Working out a plan",
			PlanNamed:    "Working out a plan: %s",
			Service:      "Calling %s · %s",
			ServiceArgs:  "Calling %s · %s: %s",
			Skill:        "Using the %s skill",
			SkillPlain:   "Using a skill",
			Tool:         "Using %s",
			ToolArgs:     "Using %s: %s",
			Fallback:     "Working",
			Failed:       "That step errored — carrying on",
			FailedNamed:  "That step errored: %s — carrying on",
			Thinking:     "Thinking:",
			Elapsed:      "%s elapsed",
		},
	},
}
