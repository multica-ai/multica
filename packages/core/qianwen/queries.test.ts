import { describe, expect, it } from "vitest";
import { qianwenInstallationsOptions, qianwenKeys } from "./queries";

describe("Qianwen installation queries", () => {
  it("isolates caller-relative binding state by workspace and current user", () => {
    expect(qianwenKeys.installations("workspace-1", "user-1")).toEqual([
      "qianwen",
      "workspace-1",
      "installations",
      "user",
      "user-1",
    ]);
    expect(qianwenKeys.installations("workspace-1", "user-1")).not.toEqual(
      qianwenKeys.installations("workspace-1", "user-2"),
    );

    expect(qianwenInstallationsOptions("workspace-1", "user-1").enabled).toBe(true);
    expect(qianwenInstallationsOptions("workspace-1", "").enabled).toBe(false);
  });
});
