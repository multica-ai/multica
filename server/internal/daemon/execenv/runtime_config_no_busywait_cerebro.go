package execenv

// CEREBRO-PATCH(runtime-config-no-busywait): FIR-2610 — standing runtime-brief
// rule that stops agents from busy-waiting on CI / deploy / any long external
// job with sleep/until poll-loops. The error catalog (FIR-2396) showed the
// single largest token bill from "stuck" runs was not a frozen tool call (0
// real cases) but agents that actively sat in wait-loops polling a deploy or CI
// until the 2-hour ceiling killed them. A per-tool-call timeout would only
// catch the (non-existent) frozen-call case while risking legitimate long
// builds; the real lever is behavioural — tell the agent to hand the wait back
// to the platform (auto-merge + a fresh run on the deploy webhook) instead of
// burning live agent time on it. Emitted into the standing brief for tasks that
// operate on a real issue (assignment- and comment-triggered), which is exactly
// where checkout → build → merge → deploy work happens.

// cerebroNoBusyWaitRule returns the standing "never busy-wait" section for the
// agent runtime brief.
func cerebroNoBusyWaitRule() string {
	var b []byte
	add := func(s string) { b = append(b, s...) }
	add("## Long-running work — never busy-wait\n\n")
	add("Never actively poll CI, a deploy, or any long external job with `sleep` / `until` wait-loops. Live agent time spent watching something you do not need to watch is the single largest source of multi-hour runs and wasted tokens on this platform — a run that sits in a poll-loop burns the full time budget for nothing and blocks the dispatch queue behind it.\n\n")
	add("- **A PR you just merged:** enable auto-merge (`gh pr merge --auto`) or just report the PR URL and exit. The deploy fires from a webhook and a fresh run picks up the result — you do not need to stay alive for it.\n")
	add("- **CI on an open PR:** check it once and report status, or report the PR URL and exit. Do not loop until it goes green.\n")
	add("- **Any other external state you would otherwise wait on:** report what you are waiting on (in a comment or issue metadata) and exit. The platform re-runs you when there is a reason to continue; a stopped run costs nothing, a polling run costs the whole budget.\n")
	add("- The only thing that may legitimately run for many minutes is a single foreground command doing real work (a build, a test suite, a migration). Waiting on someone else's job is not that.\n\n")
	return string(b)
}
