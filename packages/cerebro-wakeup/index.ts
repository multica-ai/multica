// CEREBRO: TECH-3176 — agent wakeup scheduling extension (issue-sidebar list +
// cancel). Per-type on/off lives in @multica/cerebro-feature-flags
// (cerebro_wakeup_time / cerebro_wakeup_issue_status / cerebro_wakeup_github_ci)
// and is enforced server-side in server/internal/cerebro/wakeup.
export { CerebroWakeupSection } from "./components/wakeup-section";
// CEREBRO: FIR-1714 — pending wakeups are folded into the running-agent activity
// bar (AgentLiveCard) via its existing multi-collapse, so a working agent + a
// scheduled run read as ONE bar, not two stacked banners. These are the reusable
// pieces that bar consumes: the query hook and the row renderer. Replaces the old
// standalone CerebroWakeupBar. Feature flag cerebro_wakeup_bar.
export { useIssueWakeups, WakeupActivityRow, type IssueWakeups } from "./components/wakeup-activity";
// CEREBRO: FIR-1521 (part 2) — small orange clock pip that stacks next to the
// running-agent indicator on issue lists + board cards. Feature flag
// cerebro_activity_wakeup_dot.
export { CerebroIssueWakeupPip } from "./components/wakeup-pip";
// CEREBRO: FIR-1521 (part 2 polish) — orange ticking clock for an inbox row whose
// issue has a pending wakeup (replaces the static grey clock).
export { CerebroInboxWakeupPip } from "./components/inbox-wakeup-pip";
// CEREBRO: TECH-3298 — wakeup action-note rendering in the issue timeline +
// the self-wakeup limit settings shown inline in the Cerebro features tab.
export { WakeupNote, isWakeupEntry } from "./components/wakeup-note";
export { WakeupLimitsSettings } from "./components/wakeup-limits-settings";
