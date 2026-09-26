import { describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import type { TimelineEntry } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../test/i18n";
import { useT } from "../../i18n";
import { useWakeupText } from "./wakeup-presentation";
import { formatWakeupActivity, wakeupActivityChip } from "./wakeup-activity";

vi.mock("./wakeup-condition-names", () => ({
  useConditionNames: () => ({ status: (key: string) => key, label: () => undefined, property: () => undefined, actor: (_type: string, id: string) => id }),
}));
vi.mock("../../common/use-viewing-timezone", () => ({ useViewingTimezone: () => "UTC" }));

const names: Record<string, string> = { a: "Emacs", u: "Jiayuan", g: "Grok" };
const getActorName = (_type: string, id: string) => names[id] ?? id;
const entry = (action: string, details: Record<string, unknown>, patch: Partial<TimelineEntry> = {}): TimelineEntry =>
  ({ type: "activity", id: action, actor_type: "system", actor_id: "", action, details, created_at: "2026-09-24T00:00:00Z", ...patch }) as TimelineEntry;
const reply = { id: "w", kind: "event", mode: "once", event_types: ["comment.created"], agent_id: "a", filter_actor_type: "member", filter_actor_id: "u", created_by: "u" };

function helpers() {
  const { result } = renderHook(() => ({ t: useT("issues").t, text: useWakeupText() }), {
    wrapper: ({ children }) => (
      <I18nProvider locale="zh-Hans" resources={RESOURCES}>
        {children}
      </I18nProvider>
    ),
  });
  return result.current;
}

describe("wakeup timeline entries", () => {
  it("reads each entry as one sentence", () => {
    const { t, text } = helpers();
    const read = (e: TimelineEntry) => formatWakeupActivity(e, t, text, getActorName);
    expect(read(entry("wakeup_created", { wakeup: reply }, { actor_type: "agent", actor_id: "a" }))).toBe(
      "添加了唤醒：当此任务有新评论时（由 Jiayuan 触发）唤醒 Emacs",
    );
    expect(read(entry("wakeup_triggered", { wakeup: { ...reply, condition: { type: "pull_request", event: "checks_finished" } } }))).toBe(
      "当关联 PR 的 CI 结束时，唤醒了 Emacs",
    );
    expect(read(entry("wakeup_triggered", { wakeup: reply, events: ["wakeup.manual"], actor_type: "member", actor_id: "u" }))).toBe(
      "Jiayuan 立即唤醒了 Emacs",
    );
    expect(read(entry("wakeup_triggered", { rule: "child_done", stage: 1, total: 2, target_type: "agent", target_id: "a", outcome: "woke" }))).toBe(
      "第 1 阶段的 2 个子任务已全部结束，唤醒了 Emacs",
    );
    expect(read(entry("wakeup_triggered", { rule: "child_done", total: 3, target_type: "member", target_id: "u", outcome: "notified" }))).toBe(
      "3 个子任务已全部结束，已通知 Jiayuan",
    );
    expect(read(entry("wakeup_triggered", { rule: "child_done", total: 3, target_type: "agent", target_id: "a", outcome: "merged" }))).toBe(
      "3 个子任务已全部结束，并入了 Emacs 待开始的运行",
    );
    expect(read(entry("wakeup_triggered", { rule: "child_done", stage: 2, total: 1, outcome: "none" }))).toBe(
      "第 2 阶段的 1 个子任务已全部结束",
    );
    expect(read(entry("wakeup_timed_out", { wakeup: reply, woke: true }))).toContain("唤醒了 Emacs 处理超时");
    expect(read(entry("wakeup_paused", { wakeup: reply, reason: "loop" }))).toBe("已暂停：与其他唤醒规则互相触发");
    expect(read(entry("wakeup_checkin", { wakeup: reply, note: "进度 72%" }, { actor_type: "agent", actor_id: "g", coalesced_count: 3 }))).toBe(
      "静默检查 3 次 · 最近一次：进度 72%",
    );
  });

  it("tags an entry with the rule it came from", () => {
    const { t, text } = helpers();
    const chip = (e: TimelineEntry) => wakeupActivityChip(e, t, text, getActorName);
    expect(chip(entry("wakeup_triggered", { rule: "child_done" }))).toBe("系统规则");
    expect(chip(entry("wakeup_triggered", { wakeup: reply }))).toBe("Jiayuan 创建");
    expect(chip(entry("wakeup_triggered", { wakeup: { ...reply, created_by_agent_id: "a" } }))).toBe("Emacs 创建");
    expect(chip(entry("wakeup_created", { wakeup: reply }))).toBeNull();
  });

});
