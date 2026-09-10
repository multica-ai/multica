// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildIssueStatusCatalog } from "../issue-statuses";
import type { IssueStatusEntry } from "../types";
import { statusColumnKeys, visibleStatusKeys } from "./status-category";

const catalog = buildIssueStatusCatalog([
  { key: "awaiting_response", category: "started", name: "Awaiting Response", position: 1, is_system: false },
  { key: "old_review", category: "started", name: "Old Review", position: 2, is_system: false, archived_at: "2026-01-01" },
] as IssueStatusEntry[]);

describe("concrete status columns", () => {
  it("keeps every built-in and custom key independent, including archived work", () => {
    expect(statusColumnKeys(catalog)).toEqual([
      "backlog", "todo", "in_progress", "in_review", "blocked",
      "awaiting_response", "old_review", "done", "cancelled",
    ]);
  });
  it("hiding or selecting one status does not affect category siblings", () => {
    expect(visibleStatusKeys([], ["backlog", "in_review"], catalog)).toContain("todo");
    expect(visibleStatusKeys([], ["backlog", "in_review"], catalog)).toContain("awaiting_response");
    expect(visibleStatusKeys(["awaiting_response"], [], catalog)).toEqual(["awaiting_response"]);
    expect(visibleStatusKeys(["backlog"], ["backlog"], catalog)).toEqual(["backlog"]);
  });
});
