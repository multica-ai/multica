import { describe, expect, it } from "vitest";
import { pluginSurfaceLaunchOptions } from "./queries";

describe("pluginSurfaceLaunchOptions", () => {
  it("uses a new cache entry when a mounted panel moves to another issue", () => {
    const issueOne = pluginSurfaceLaunchOptions(
      "workspace-1",
      "installation-1",
      "panel",
      "version-1",
      "frame-1",
      "issue-1",
    );
    const issueTwo = pluginSurfaceLaunchOptions(
      "workspace-1",
      "installation-1",
      "panel",
      "version-1",
      "frame-1",
      "issue-2",
    );

    expect(issueOne.queryKey).not.toEqual(issueTwo.queryKey);
    expect(issueOne.queryKey.at(-1)).toBe("issue-1");
    expect(issueTwo.queryKey.at(-1)).toBe("issue-2");
  });
});
