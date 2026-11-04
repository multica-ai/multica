// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { groupCompanyIssues } from "./company-issue-groups";

const issue = (id: string, status: string) => ({ id, status }) as Issue;

describe("groupCompanyIssues", () => {
  it("sorts issues by lifecycle into in progress, up next and done", () => {
    const groups = groupCompanyIssues([
      issue("a", "in_progress"),
      issue("b", "in_review"),
      issue("c", "todo"),
      issue("d", "backlog"),
      issue("e", "done"),
    ]);
    expect(groups.active.map((i) => i.id)).toEqual(["a", "b"]);
    expect(groups.planned.map((i) => i.id)).toEqual(["c", "d"]);
    expect(groups.done.map((i) => i.id)).toEqual(["e"]);
  });

  it("leaves cancelled issues out of every group", () => {
    const groups = groupCompanyIssues([issue("x", "cancelled")]);
    expect(groups.active).toEqual([]);
    expect(groups.planned).toEqual([]);
    expect(groups.done).toEqual([]);
  });
});
