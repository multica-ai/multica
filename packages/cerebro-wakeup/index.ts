// CEREBRO: TECH-3176 — agent wakeup scheduling extension (issue-sidebar list +
// cancel). Per-type on/off lives in @multica/cerebro-feature-flags
// (cerebro_wakeup_time / cerebro_wakeup_issue_status / cerebro_wakeup_github_ci)
// and is enforced server-side in server/internal/cerebro/wakeup.
export { CerebroWakeupSection } from "./components/wakeup-section";
