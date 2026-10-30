// @vitest-environment node
import { describe, expect, it } from "vitest";
import { reconcileRemote } from "./use-doc-draft";
import { docRouteTarget } from "./doc-tree";

const base = { revision: 3, title: "Runbook", content: "one\ntwo\nthree" };

describe("reconcileRemote", () => {
  it("ignores a revision the draft already has", () => {
    expect(reconcileRemote(base, "one\ntwo\nthree", { revision: 3, content: "x" })).toEqual({ kind: "stale" });
    expect(reconcileRemote(base, "edited", { revision: 2, content: "x" })).toEqual({ kind: "stale" });
  });

  it("takes the remote text as is when nothing was typed", () => {
    expect(reconcileRemote(base, base.content, { revision: 4, content: "one\nTWO\nthree" })).toEqual({
      kind: "adopt",
      content: "one\nTWO\nthree",
    });
  });

  it("merges edits to different lines", () => {
    const outcome = reconcileRemote(base, "ONE\ntwo\nthree", { revision: 4, content: "one\ntwo\nTHREE" });
    expect(outcome).toEqual({ kind: "merge", content: "ONE\ntwo\nTHREE", conflicts: [] });
  });

  it("keeps the local text where both sides changed the same line, and reports it", () => {
    const outcome = reconcileRemote(base, "one\nmine\nthree", { revision: 4, content: "one\ntheirs\nthree" });
    expect(outcome.kind).toBe("merge");
    if (outcome.kind !== "merge") return;
    expect(outcome.content).toBe("one\nmine\nthree");
    expect(outcome.conflicts).toEqual([
      { mine: ["mine"], theirs: ["theirs"], base: ["two"], at: 1, before: ["one"], after: ["three"] },
    ]);
  });
});

describe("docRouteTarget", () => {
  const docs = "/acme/docs";
  it("reads docs, their history and folders", () => {
    expect(docRouteTarget("/acme/docs/d1", docs)).toEqual({ docId: "d1", folderId: null });
    expect(docRouteTarget("/acme/docs/d1/history", docs)).toEqual({ docId: "d1", folderId: null });
    expect(docRouteTarget("/acme/docs/folders/f1", docs)).toEqual({ docId: null, folderId: "f1" });
  });

  it("is empty on the feed and outside the knowledge base", () => {
    expect(docRouteTarget("/acme/docs", docs)).toEqual({ docId: null, folderId: null });
    expect(docRouteTarget("/acme/issues/MUL-1", docs)).toEqual({ docId: null, folderId: null });
  });
});
