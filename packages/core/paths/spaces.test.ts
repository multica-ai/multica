// @vitest-environment node
import { describe, expect, it } from "vitest";
import { paths } from "./paths";
import { WORKSPACE_PAGES, type WorkspacePageKey } from "./route-icons";
import { spaceForPath } from "./spaces";

describe("spaceForPath", () => {
  const ws = paths.workspace("acme");

  it("groups every workspace nav page into a space", () => {
    const unassigned = (Object.keys(WORKSPACE_PAGES) as WorkspacePageKey[]).filter(
      (key) => spaceForPath(`/acme/${WORKSPACE_PAGES[key].segment}`) === null,
    );
    expect(unassigned).toEqual([]);
  });

  it.each([
    [ws.inbox(), "inbox"],
    [ws.myIssues(), "work"],
    [ws.issues(), "work"],
    [ws.issueDetail("MUL-1"), "work"],
    [ws.projectDetail("p-1"), "work"],
    [ws.agentDetail("a-1"), "team"],
    [ws.squadDetail("s-1"), "team"],
    [ws.memberDetail("m-1"), "team"],
    [ws.runtimeDetail("r-1"), "team"],
    [ws.skills(), "team"],
    [ws.usage(), "team"],
    [ws.chatSession("c-1"), "chat"],
    [ws.autopilotDetail("ap-1"), "autopilots"],
    [`${ws.settings()}?tab=members`, "settings"],
  ])("maps %s to %s", (path, space) => {
    expect(spaceForPath(path)).toBe(space);
  });

  it("returns null outside every space", () => {
    expect(spaceForPath(ws.attachmentPreview("f-1"))).toBeNull();
    expect(spaceForPath("/acme")).toBeNull();
    expect(spaceForPath("/")).toBeNull();
  });
});
