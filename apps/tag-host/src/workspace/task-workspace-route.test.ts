// @vitest-environment node

import { describe, expect, it, vi } from "vitest";
import type { NavigationAdapter } from "@multica/views/navigation";
import { createTaskCenterNavigationAdapter } from "./task-workspace-route";

describe("TaskWorkspaceRoute navigation", () => {
  it("folds legacy collection links into Tasks tabs", () => {
    const push = vi.fn();
    const replace = vi.fn();
    const adapter = createTaskCenterNavigationAdapter({
      push,
      replace,
      back: vi.fn(),
      pathname: "/studio/issues",
      searchParams: new URLSearchParams(),
      getShareableUrl: (path) => `https://tag.test${path}`,
    } satisfies NavigationAdapter);

    adapter.push("/studio/inbox?issue=MUL-7");
    adapter.replace("/studio/autopilots");

    expect(push).toHaveBeenCalledWith("/studio/issues?issue=MUL-7&tab=activity");
    expect(replace).toHaveBeenCalledWith("/studio/issues?tab=automations");
  });
});
