// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { DocSummary, SearchDocResult } from "@multica/core/types";
import { mergeDocHits } from "./use-root-model";

const summary = (id: string, title: string): DocSummary => ({
  id, workspaceId: "ws", folderId: "eng", title, revision: 1, versionCount: 1,
  createdByType: "member", createdById: "u", updatedByType: "member", updatedById: "u",
  createdAt: "2026-11-19T00:00:00Z", updatedAt: "2026-11-19T00:00:00Z",
});
const hit = (id: string, title: string, snippet: string | null): SearchDocResult => ({
  ...summary(id, title), matchSource: snippet ? "content" : "title", matchedSnippet: snippet,
});
const folders = new Map([["eng", "Engineering"]]);
const local = [{ item: { doc: summary("title-hit", "Key rotation"), folder: "Engineering" }, score: 90 }];
const server = [hit("title-hit", "Key rotation", "...rotation steps..."), hit("body-hit", "Runbook", "...rotate keys...")];

describe("mergeDocHits", () => {
  it("adds server content hits after the instant title hits, lending excerpts", () => {
    const merged = mergeDocHits(local, server, folders, "rotat");
    expect(merged.map((m) => [m.item.doc.id, m.item.snippet, m.item.stale])).toEqual([
      ["title-hit", "...rotation steps...", undefined],
      ["body-hit", "...rotate keys...", undefined],
    ]);
    expect(merged[1]!.item.folder).toBe("Engineering");
  });

  it("keeps an earlier query's server rows painted but disabled, without lending excerpts", () => {
    const merged = mergeDocHits(local, server, folders, "rotati", true);
    expect(merged.map((m) => [m.item.doc.id, m.item.snippet, m.item.stale])).toEqual([
      ["title-hit", undefined, undefined],
      ["body-hit", "...rotate keys...", true],
    ]);
  });
});
