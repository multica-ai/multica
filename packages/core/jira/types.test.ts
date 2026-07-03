import { describe, expect, it } from "vitest";
import { parseJiraSearch, parseJiraInput } from "./types";

const RAW = {
  issues: [
    {
      key: "PROJ-1",
      fields: {
        summary: "Fix login",
        description: null,
        duedate: "2026-07-01",
        updated: "2026-06-30T10:00:00.000+0000",
        status: { name: "In Progress" },
        priority: { name: "High" },
        subtasks: [{ key: "PROJ-2" }],
        comment: { comments: [] },
      },
    },
  ],
  total: 1,
};

describe("parseJiraSearch", () => {
  it("parses a well-formed search response", () => {
    const res = parseJiraSearch(RAW);
    expect(res.issues[0]!.key).toBe("PROJ-1");
    expect(res.issues[0]!.fields.status.name).toBe("In Progress");
    expect(res.issues[0]!.fields.subtasks[0]!.key).toBe("PROJ-2");
  });

  it("falls back to an empty result on malformed input", () => {
    const res = parseJiraSearch({ issues: "nope" });
    expect(res).toEqual({ issues: [], total: 0 });
  });

  it("defaults optional/nullable fields without throwing", () => {
    const res = parseJiraSearch({
      issues: [{ key: "PROJ-3", fields: { summary: "x", status: { name: "Done" } } }],
      total: 1,
    });
    expect(res.issues[0]!.fields.priority).toBeNull();
    expect(res.issues[0]!.fields.subtasks).toEqual([]);
    expect(res.issues[0]!.fields.comment.comments).toEqual([]);
  });
});

describe("parseJiraInput", () => {
  it("returns null for empty input (use default JQL)", () => {
    expect(parseJiraInput("")).toBeNull();
    expect(parseJiraInput("   ")).toBeNull();
    expect(parseJiraInput("\t\n")).toBeNull();
  });

  it("extracts issue key from a /browse/ URL", () => {
    expect(parseJiraInput("https://acme.atlassian.net/browse/PROJ-123")).toEqual({
      jql: "key = PROJ-123",
    });
    expect(
      parseJiraInput("https://acme.atlassian.net/browse/PROJ-123?fields=summary"),
    ).toEqual({ jql: "key = PROJ-123" });
  });

  it("extracts issue key from a selectedIssue query param", () => {
    expect(
      parseJiraInput(
        "https://acme.atlassian.net/jira/software/projects/PROJ/boards/1?selectedIssue=PROJ-123",
      ),
    ).toEqual({ jql: "key = PROJ-123" });
  });

  it("converts a bare Jira key to single-issue JQL", () => {
    expect(parseJiraInput("PROJ-123")).toEqual({ jql: "key = PROJ-123" });
    expect(parseJiraInput("ABC-1")).toEqual({ jql: "key = ABC-1" });
  });

  it("treats arbitrary text as raw JQL", () => {
    expect(parseJiraInput("assignee = currentUser()")).toEqual({
      jql: "assignee = currentUser()",
    });
    expect(parseJiraInput("project = FOO AND status = Open")).toEqual({
      jql: "project = FOO AND status = Open",
    });
  });

  it("trims surrounding whitespace", () => {
    expect(parseJiraInput("  PROJ-123  ")).toEqual({ jql: "key = PROJ-123" });
    expect(parseJiraInput("  assignee = currentUser()  ")).toEqual({
      jql: "assignee = currentUser()",
    });
  });
});
