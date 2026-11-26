import { describe, expect, it } from "vitest";
import { autoArchivePreferences, autoArchivePreset } from "./auto-archive-presets";

describe("autoArchivePreset", () => {
  it.each([
    [{}, "handled"],
    [{ auto_archive_on_agent_trigger: "muted" }, "completed"],
    [
      {
        auto_archive_on_agent_trigger: "muted",
        auto_archive_on_comment: "muted",
        auto_archive_on_status: "muted",
        auto_archive_on_assignee: "muted",
      },
      "completed",
    ],
    [autoArchivePreferences([]), "never"],
    [{ auto_archive_on_labels: "all" }, "custom"],
    [{ auto_archive_on_comment: "all" }, "custom"],
    [{ auto_archive_completed: "muted" }, "custom"],
  ] as const)("%j is %s", (preferences, preset) => {
    expect(autoArchivePreset(preferences)).toBe(preset);
  });
});

describe("autoArchivePreferences", () => {
  it("sets every action, on or off", () => {
    expect(autoArchivePreferences(["auto_archive_completed", "auto_archive_on_labels"])).toEqual({
      auto_archive_completed: "all",
      auto_archive_on_agent_trigger: "muted",
      auto_archive_on_comment: "muted",
      auto_archive_on_status: "muted",
      auto_archive_on_assignee: "muted",
      auto_archive_on_priority: "muted",
      auto_archive_on_project: "muted",
      auto_archive_on_dates: "muted",
      auto_archive_on_labels: "all",
    });
  });
});
