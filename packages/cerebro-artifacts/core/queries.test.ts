import { describe, expect, it } from "vitest";
import { artifactKeys } from "./queries";

describe("artifactKeys", () => {
  it("derives stable, scope-distinct cache keys", () => {
    const wsId = "ws-1";
    expect(artifactKeys.all(wsId)).toEqual(["artifacts", "ws-1"]);
    expect(artifactKeys.byIssue(wsId, "issue-1")).toEqual([
      "artifacts",
      "ws-1",
      "by-issue",
      "issue-1",
    ]);
    expect(artifactKeys.byProject(wsId, "proj-1")).toEqual([
      "artifacts",
      "ws-1",
      "by-project",
      "proj-1",
    ]);
    expect(artifactKeys.detail(wsId, "art-1")).toEqual([
      "artifacts",
      "ws-1",
      "detail",
      "art-1",
    ]);
  });

  it("keys are workspace-scoped so switching workspace changes the cache", () => {
    expect(artifactKeys.byIssue("ws-a", "issue-1")).not.toEqual(
      artifactKeys.byIssue("ws-b", "issue-1"),
    );
  });
});
