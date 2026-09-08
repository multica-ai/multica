// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseIssueDeepLink } from "./issue-deep-link";

const ISSUE_ID = "11111111-2222-3333-4444-555555555555";
const WORKSPACE_ID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee";
const COMMENT_ID = "99999999-8888-7777-6666-555555555555";

describe("parseIssueDeepLink", () => {
  it("parses an issue destination with an optional source comment", () => {
    expect(
      parseIssueDeepLink(
        `multica://issue/${ISSUE_ID}?workspace=${WORKSPACE_ID}&comment=${COMMENT_ID}`,
      ),
    ).toEqual({
      issueId: ISSUE_ID,
      workspaceId: WORKSPACE_ID,
      commentId: COMMENT_ID,
    });
    expect(
      parseIssueDeepLink(
        `multica://issue/${ISSUE_ID}?workspace=${WORKSPACE_ID}`,
      ),
    ).toEqual({ issueId: ISSUE_ID, workspaceId: WORKSPACE_ID });
  });

  it.each([
    "https://issue/11111111-2222-3333-4444-555555555555?workspace=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
    "multica://issues/11111111-2222-3333-4444-555555555555?workspace=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
    "multica://issue/not-a-uuid?workspace=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
    "multica://issue/11111111-2222-3333-4444-555555555555",
    "multica://issue/11111111-2222-3333-4444-555555555555/extra?workspace=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
    "multica://issue/11111111-2222-3333-4444-555555555555?workspace=bad",
    "multica://issue/11111111-2222-3333-4444-555555555555?workspace=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee&comment=bad",
    "multica://issue/11111111-2222-3333-4444-555555555555?workspace=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee&route=/settings",
  ])("rejects malformed or out-of-scope input %s", (value) => {
    expect(parseIssueDeepLink(value)).toBeNull();
  });
});
