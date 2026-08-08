import { describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { createSafeId } from "@multica/core/utils";
import type { Comment, TimelineEntry } from "@multica/core/types";

// issue-ws-updaters imports issueKeys from data/queries/issue-keys, which
// transitively imports the native fetch client. Mock it so the Node test
// never loads RN modules. The key factory is pure and needs nothing from
// the api surface.
vi.mock("@/data/api", () => ({ api: {} }));

import {
  appendTimelineEntry,
  commentToTimelineEntry,
  replaceOptimisticWithReal,
} from "./issue-ws-updaters";
import { issueKeys } from "@/data/queries/issue-keys";

const WS = "ws-1";
const ISSUE = "issue-1";

function optEntry(id: string, created_at: string, content = "hello"): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "user-1",
    content,
    parent_id: null,
    created_at,
    updated_at: created_at,
    comment_type: "comment",
    reactions: [],
    attachments: [],
  };
}

function realComment(id: string, created_at: string, content = "hello"): Comment {
  return {
    id,
    type: "comment",
    issue_id: ISSUE,
    author_type: "member",
    author_id: "user-1",
    parent_id: null,
    content,
    created_at,
    updated_at: created_at,
    reactions: [],
    attachments: [],
    resolved_at: null,
    resolved_by_type: null,
    resolved_by_id: null,
    source_task_id: null,
  } as unknown as Comment;
}

describe("replaceOptimisticWithReal (mutation onSuccess canonical replacement)", () => {
  it("replaces the optimistic row with the real entry when WS has not landed", () => {
    const optimistic = optEntry("optimistic-1", "2026-01-01T00:00:00Z");
    const old = [optEntry("root", "2025-01-01T00:00:00Z"), optimistic];
    const real = commentToTimelineEntry(realComment("cmt-real", "2026-01-01T00:00:00Z"));

    const next = replaceOptimisticWithReal(old, "optimistic-1", real);

    expect(next?.map((e) => e.id)).toEqual(["root", "cmt-real"]);
    // Input is not mutated.
    expect(old.map((e) => e.id)).toEqual(["root", "optimistic-1"]);
  });

  it("B1 regression: when WS already inserted the real id, drops ONLY this mutation's optimistic row", () => {
    // WS event beat the HTTP response: both the optimistic and real rows
    // are present. onSuccess must keep the real entry and remove the
    // optimistic one (NOT append a second real).
    const optimistic = optEntry("optimistic-1", "2026-01-01T00:00:00Z");
    const real = commentToTimelineEntry(realComment("cmt-real", "2026-01-01T00:00:00Z"));
    const old = [optimistic, real];

    const next = replaceOptimisticWithReal(old, "optimistic-1", real);

    expect(next?.map((e) => e.id)).toEqual(["cmt-real"]);
  });

  it("does nothing when there is no optimistic id (e.g. unauthenticated submit)", () => {
    const old = [optEntry("root", "2025-01-01T00:00:00Z")];
    const real = commentToTimelineEntry(realComment("cmt-real", "2026-01-01T00:00:00Z"));
    expect(replaceOptimisticWithReal(old, null, real)).toBe(old);
  });

  it("identical concurrent submits never mis-pair: each ctx replaces only its own optimistic id", () => {
    // Two submits with identical content/parent/actor are in flight at the
    // same time. The pairing key is the mutation's own optimisticId from
    // ctx, NOT a fuzzy content match. When the first server response lands,
    // only optimistic-A is replaced by cmt-A; optimistic-B stays pending.
    const a = optEntry("optimistic-A", "2026-01-01T00:00:00Z", "same text");
    const b = optEntry("optimistic-B", "2026-01-01T00:00:01Z", "same text");
    const old = [a, b];

    const afterA = replaceOptimisticWithReal(
      old,
      "optimistic-A",
      commentToTimelineEntry(realComment("cmt-A", "2026-01-01T00:00:00Z", "same text")),
    );
    expect(afterA?.map((e) => e.id)).toEqual(["cmt-A", "optimistic-B"]);

    // Second response lands; only B is replaced.
    const afterB = replaceOptimisticWithReal(
      afterA!,
      "optimistic-B",
      commentToTimelineEntry(realComment("cmt-B", "2026-01-01T00:00:01Z", "same text")),
    );
    expect(afterB?.map((e) => e.id)).toEqual(["cmt-A", "cmt-B"]);
  });

  it("same-millisecond concurrent submits get distinct optimistic ids (no Date.now collision)", () => {
    // Regression: the optimistic id used to be `optimistic-${Date.now()}`.
    // Two submits fired in the same millisecond (identical content/parent)
    // produced the same optimistic id, making onSuccess's one-to-one
    // replacement ambiguous. The mutation now suffixes createSafeId() which
    // is cryptographically random, so even same-ms ids never collide.
    const ids = Array.from({ length: 50 }, () => `optimistic-${createSafeId()}`);
    const unique = new Set(ids);
    expect(unique.size).toBe(ids.length);

    // Two such ids coexist in the cache and are replaced independently when
    // their own server response lands.
    const idA = `optimistic-${createSafeId()}`;
    const idB = `optimistic-${createSafeId()}`;
    expect(idA).not.toBe(idB);
    const old = [
      optEntry("root", "2025-01-01T00:00:00Z"),
      optEntry(idA, "2026-01-01T00:00:00Z", "same text"),
      optEntry(idB, "2026-01-01T00:00:00Z", "same text"),
    ];

    const afterA = replaceOptimisticWithReal(
      old,
      idA,
      commentToTimelineEntry(
        realComment("cmt-A", "2026-01-01T00:00:00Z", "same text"),
      ),
    );
    expect(afterA?.map((e) => e.id)).toEqual(["root", "cmt-A", idB]);

    const afterB = replaceOptimisticWithReal(
      afterA!,
      idB,
      commentToTimelineEntry(
        realComment("cmt-B", "2026-01-01T00:00:00Z", "same text"),
      ),
    );
    expect(afterB?.map((e) => e.id)).toEqual(["root", "cmt-A", "cmt-B"]);
  });
});

describe("appendTimelineEntry (WS handler is idempotent by real id, does NO optimistic pairing)", () => {
  function freshQc(): QueryClient {
    const qc = new QueryClient();
    qc.setQueryData<TimelineEntry[]>(issueKeys.timeline(WS, ISSUE), []);
    return qc;
  }

  it("inserts a new real entry sorted ASC", () => {
    const qc = freshQc();
    appendTimelineEntry(
      qc,
      WS,
      ISSUE,
      commentToTimelineEntry(realComment("cmt-2", "2026-01-02T00:00:00Z")),
    );
    appendTimelineEntry(
      qc,
      WS,
      ISSUE,
      commentToTimelineEntry(realComment("cmt-1", "2026-01-01T00:00:00Z")),
    );
    const entries = qc.getQueryData<TimelineEntry[]>(issueKeys.timeline(WS, ISSUE));
    expect(entries?.map((e) => e.id)).toEqual(["cmt-1", "cmt-2"]);
  });

  it("ignores a duplicate real id (reconnect re-emit / two-client echo) without replacing optimistic rows", () => {
    const qc = freshQc();
    // Seed with a pending optimistic entry (WS must not touch it).
    qc.setQueryData<TimelineEntry[]>(issueKeys.timeline(WS, ISSUE), [
      optEntry("optimistic-1", "2026-01-01T00:00:00Z"),
    ]);
    const real = commentToTimelineEntry(realComment("cmt-real", "2026-01-01T00:00:00Z"));
    appendTimelineEntry(qc, WS, ISSUE, real);
    // Real appended alongside optimistic (transient duplicate that
    // onSuccess will collapse). Order is ASC by (created_at, id).
    let entries = qc.getQueryData<TimelineEntry[]>(issueKeys.timeline(WS, ISSUE));
    expect(entries?.map((e) => e.id).sort()).toEqual(["cmt-real", "optimistic-1"]);
    // A re-emitted WS event with the same real id is ignored — optimistic
    // row remains for onSuccess to collapse, and the real content is not
    // overwritten by the duplicate payload.
    appendTimelineEntry(qc, WS, ISSUE, { ...real, content: "changed" });
    entries = qc.getQueryData<TimelineEntry[]>(issueKeys.timeline(WS, ISSUE));
    expect(entries?.map((e) => e.id).sort()).toEqual(["cmt-real", "optimistic-1"]);
    expect(entries?.find((e) => e.id === "cmt-real")?.content).toBe("hello");
  });
});
