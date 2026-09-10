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
  it("uses saved positions for built-in and custom columns alike", () => {
    const ordered = buildIssueStatusCatalog([
      { key: "in_review", category: "started", position: 1, is_system: true },
      { key: "qa", category: "started", position: 2, is_system: false },
      { key: "blocked", category: "started", position: 3, is_system: true },
      { key: "in_progress", category: "started", position: 4, is_system: true },
    ] as IssueStatusEntry[]);
    expect(statusColumnKeys(ordered)).toEqual([
      "backlog", "todo", "in_review", "qa", "blocked", "in_progress", "done", "cancelled",
    ]);
  });
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
