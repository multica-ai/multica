// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { openIssueDeepLink } from "./issue-deep-link";

const destination = {
  issueId: "11111111-2222-3333-4444-555555555555",
  workspaceId: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  commentId: "99999999-8888-7777-6666-555555555555",
};

describe("openIssueDeepLink", () => {
  it("resolves the current workspace slug and applies the comment anchor", () => {
    const tabs = {
      switchWorkspace: vi.fn(),
      navigateActiveSession: vi.fn(),
    };

    expect(
      openIssueDeepLink(
        destination,
        [{ id: destination.workspaceId.toUpperCase(), slug: "current-slug" }],
        tabs,
      ),
    ).toBe(true);
    const path = `/current-slug/issues/${destination.issueId}#comment-${destination.commentId}`;
    expect(tabs.switchWorkspace).toHaveBeenCalledWith("current-slug", path);
    expect(tabs.navigateActiveSession).toHaveBeenCalledWith(path, {
      replace: true,
    });
  });

  it("does nothing when the signed-in account cannot resolve the workspace", () => {
    const tabs = {
      switchWorkspace: vi.fn(),
      navigateActiveSession: vi.fn(),
    };

    expect(openIssueDeepLink(destination, [], tabs)).toBe(false);
    expect(tabs.switchWorkspace).not.toHaveBeenCalled();
    expect(tabs.navigateActiveSession).not.toHaveBeenCalled();
  });
});
