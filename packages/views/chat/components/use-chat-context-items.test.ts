// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseCurrentContextRoute, resolvePageIssueContext } from "./use-chat-context-items";

describe("parseCurrentContextRoute", () => {
  const cases: Array<[string, string, ReturnType<typeof parseCurrentContextRoute>]> = [
    ["/acme/issues/MUL-1", "", { type: "issue", id: "MUL-1" }],
    [
      "/acme/issues/0190aaaa-0000-7000-8000-000000000001",
      "",
      { type: "issue", id: "0190aaaa-0000-7000-8000-000000000001" },
    ],
    ["/acme/issues/MUL%2D1", "", { type: "issue", id: "MUL-1" }],
    ["/acme/inbox", "issue=issue-uuid", { type: "issue", id: "issue-uuid" }],
    ["/acme/inbox", "view=archived&issue=issue-uuid", { type: "issue", id: "issue-uuid" }],
    ["/acme/projects/project-uuid", "", { type: "project", id: "project-uuid" }],
    ["/acme/inbox", "", null],
    ["/acme/issues", "", null],
    ["/acme/issues/MUL-1/activity", "", null],
    ["/acme/chat", "", null],
    ["/acme/my-issues", "issue=issue-uuid", null],
  ];

  it.each(cases)("%s?%s", (pathname, search, expected) => {
    expect(parseCurrentContextRoute(pathname, new URLSearchParams(search))).toEqual(expected);
  });
});

describe("resolvePageIssueContext", () => {
  const issue = { id: "issue-uuid-1", identifier: "MUL-12", title: "Fix login redirect" };

  it("carries the open issue", () => {
    expect(resolvePageIssueContext(issue, null)).toBe(issue);
  });

  it("carries nothing when no issue is open", () => {
    expect(resolvePageIssueContext(undefined, null)).toBeNull();
    expect(resolvePageIssueContext(undefined, "issue-uuid-1")).toBeNull();
  });

  it("drops the issue the user removed", () => {
    expect(resolvePageIssueContext(issue, "issue-uuid-1")).toBeNull();
  });

  it("keeps an issue other than the one removed", () => {
    expect(resolvePageIssueContext(issue, "issue-uuid-2")).toBe(issue);
  });
});
