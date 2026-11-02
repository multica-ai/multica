// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";

import { formatActivity } from "./format-activity";
import { i18n } from "./i18n/singleton";

const noActor = () => "";

function entry(action: string, details: Record<string, unknown>): TimelineEntry {
  return {
    type: "activity",
    id: "a1",
    actor_type: "member",
    actor_id: "m1",
    created_at: "2026-09-24T00:00:00Z",
    action,
    details,
  };
}

describe("formatActivity duplicate marks (MUL-7349)", () => {
  afterEach(async () => {
    await i18n.changeLanguage("en");
  });

  it("keeps the English copy web uses", async () => {
    await i18n.changeLanguage("en");
    const f = (action: string, details: Record<string, unknown>) =>
      formatActivity(entry(action, details), noActor);

    expect(f("duplicate_marked", { original_identifier: "MUL-1" })).toBe(
      "marked this issue as a duplicate of MUL-1",
    );
    expect(
      f("duplicate_unmarked", {
        original_identifier: "MUL-1",
        reason: "original_deleted",
      }),
    ).toBe("removed the duplicate mark, MUL-1 was deleted");
    expect(
      f("duplicate_unmarked", { original_identifier: "MUL-1", to: "todo" }),
    ).toBe("unmarked this issue as a duplicate of MUL-1 and moved it to Todo");
    expect(f("duplicate_unmarked", { original_identifier: "MUL-1" })).toBe(
      "unmarked this issue as a duplicate of MUL-1",
    );
    expect(f("duplicate_added", { duplicate_identifier: "MUL-2" })).toBe(
      "marked MUL-2 as a duplicate of this issue",
    );
    expect(f("duplicate_removed", { duplicate_identifier: "MUL-2" })).toBe(
      "unmarked MUL-2 as a duplicate of this issue",
    );
  });

  it("renders Chinese copy with the status as its lowercase identifier", async () => {
    await i18n.changeLanguage("zh-Hans");
    expect(
      formatActivity(
        entry("duplicate_unmarked", { original_identifier: "MUL-1", to: "todo" }),
        noActor,
      ),
    ).toBe("取消了这个任务对 MUL-1 的重复标记，并移至 todo");
    expect(
      formatActivity(entry("duplicate_added", { duplicate_identifier: "MUL-2" }), noActor),
    ).toBe("把 MUL-2 标记为这个任务的重复");
  });
});
