// @vitest-environment node
import { createI18n } from "@multica/core/i18n/react";
import { describe, expect, it } from "vitest";
import { RESOURCES } from "./index";

const i18n = createI18n("ru", RESOURCES);

const countedMessages = [
  ["schedule interval", (count: number) => i18n.t(($) => $.plugins.schedule.frequency_minutes, { ns: "settings", count })],
  ["workspace invitations", (count: number) => i18n.t(($) => $.batch.submit_join, { ns: "invite", count })],
  ["automation deletion", (count: number) => i18n.t(($) => $.actions.delete_dialog.description, { ns: "autopilots", count, name: "Example" })],
  ["skill deletion", (count: number) => i18n.t(($) => $.actions.delete_dialog_desc, { ns: "skills", count })],
  ["issue deletion", (count: number) => i18n.t(($) => $.batch.delete_dialog_desc, { ns: "issues", count })],
  ["Qoder pause", (count: number) => i18n.t(($) => $.qoder.stop_confirm.description_agents, { ns: "runtimes", count })],
  ["Qoder disconnect", (count: number) => i18n.t(($) => $.qoder.disconnect_confirm.description_agents, { ns: "runtimes", count })],
  ["completed runs", (count: number) => i18n.t(($) => $.activity.task_completed, { ns: "issues", count })],
  ["failed runs", (count: number) => i18n.t(($) => $.activity.task_failed, { ns: "issues", count })],
  ["stage sub-issues", (count: number) => i18n.t(($) => $.activity.wakeup_child_done_stage, { ns: "issues", count, stage: "Example" })],
  ["all sub-issues", (count: number) => i18n.t(($) => $.activity.wakeup_child_done_all, { ns: "issues", count })],
] as const;

describe("Russian plural rendering", () => {
  it("keeps multi-status filters in Russian for counts ending in one", () => {
    expect(i18n.t(($) => $.filters.chip_status_count, { ns: "issues", count: 21 })).toBe("21 статус");
    expect(i18n.t(($) => $.filters.chip_status_count, { ns: "issues", count: 31 })).toBe("31 статус");
  });

  it.each(countedMessages)("%s preserves counts ending in one", (_name, render) => {
    for (const count of [1, 21, 31, 101]) {
      const message = render(count);
      expect(message, `count=${count}`).toContain(String(count));
      expect(message).not.toContain("{{");
    }
  });
});
