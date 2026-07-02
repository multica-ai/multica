import { describe, expect, it, vi } from "vitest";
import type { SearchIssueResult } from "@multica/core/types";
import {
  issueResultTarget,
  issueSnippet,
  searchGroupTitle,
} from "./search-utils";

const baseIssue: SearchIssueResult = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "MUL-1",
  title: "Search polish",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  metadata: {},
  position: 0,
  start_date: null,
  due_date: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  match_source: "title",
};

describe("mobile search utils", () => {
  it("renders total counts from the API response", () => {
    expect(searchGroupTitle("Issues", 37)).toBe("Issues · 37");
    expect(searchGroupTitle("Projects", 0)).toBe("Projects");
  });

  it("selects description and comment snippets from their matched fields", () => {
    expect(
      issueSnippet({
        ...baseIssue,
        match_source: "description",
        matched_description_snippet: "description hit",
        matched_comment_snippet: "comment hit",
      }),
    ).toEqual({ icon: "document-text-outline", text: "description hit" });

    expect(
      issueSnippet({
        ...baseIssue,
        match_source: "comment",
        matched_description_snippet: "description hit",
        matched_comment_snippet: "comment hit",
      }),
    ).toEqual({ icon: "chatbubble-outline", text: "comment hit" });
  });

  it("builds a comment deep link with the matched comment id and a nonce", () => {
    vi.setSystemTime(new Date("2026-05-28T12:00:00.123Z"));

    expect(
      issueResultTarget(
        {
          ...baseIssue,
          match_source: "comment",
          matched_comment_id: "comment-42",
        },
        "acme",
      ),
    ).toBe("/acme/issue/issue-1?highlight=comment-42&h=1779969600123");

    vi.useRealTimers();
  });
});
