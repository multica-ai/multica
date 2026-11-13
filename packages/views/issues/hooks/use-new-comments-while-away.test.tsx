import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { useNewCommentsWhileAway } from "./use-new-comments-while-away";

const ME = "user-me";
let clock = Date.parse("2026-11-10T00:00:00Z");

function comment(id: string, actor: { type?: string; id?: string } = {}, extra: Partial<TimelineEntry> = {}): TimelineEntry {
  clock += 1000;
  return {
    type: "comment",
    id,
    actor_type: actor.type ?? "agent",
    actor_id: actor.id ?? "agent-1",
    created_at: new Date(clock).toISOString(),
    content: id,
    ...extra,
  } as TimelineEntry;
}

type Props = { timeline: TimelineEntry[]; ready: boolean; away: boolean; resetKey: string };

function setup(initial: Props) {
  return renderHook(
    (props: Props) => useNewCommentsWhileAway(props.timeline, { ...props, userId: ME }),
    { initialProps: initial },
  );
}

describe("useNewCommentsWhileAway", () => {
  it("treats the loaded timeline as the baseline and counts later arrivals while away", () => {
    const base = [comment("c1"), comment("c2")];
    const { result, rerender } = setup({ timeline: base, ready: true, away: true, resetKey: "i1" });
    expect(result.current.ids).toEqual([]);

    rerender({ timeline: [...base, comment("c3"), comment("c4")], ready: true, away: true, resetKey: "i1" });
    expect(result.current.ids).toEqual(["c3", "c4"]);
  });

  it("ignores an older thread merged into the bounded timeline", () => {
    const old = comment("old", {}, { created_at: "2026-01-01T00:00:00Z" });
    const base = [comment("c1")];
    const { result, rerender } = setup({ timeline: base, ready: true, away: true, resetKey: "i1" });
    rerender({ timeline: [old, ...base], ready: true, away: true, resetKey: "i1" });
    expect(result.current.ids).toEqual([]);

    rerender({ timeline: [old, ...base, comment("c2")], ready: true, away: true, resetKey: "i1" });
    expect(result.current.ids).toEqual(["c2"]);
  });

  it("waits for the timeline to load before taking the baseline", () => {
    const { result, rerender } = setup({ timeline: [], ready: false, away: true, resetKey: "i1" });
    rerender({ timeline: [comment("c1"), comment("c2")], ready: true, away: true, resetKey: "i1" });
    expect(result.current.ids).toEqual([]);
  });

  it("ignores arrivals while the reader is at the latest entry", () => {
    const base = [comment("c1")];
    const { result, rerender } = setup({ timeline: base, ready: true, away: false, resetKey: "i1" });
    rerender({ timeline: [...base, comment("c2")], ready: true, away: false, resetKey: "i1" });
    rerender({ timeline: [...base, comment("c2")], ready: true, away: true, resetKey: "i1" });
    expect(result.current.ids).toEqual([]);
  });

  it("never counts the reader's own comments", () => {
    const base = [comment("c1")];
    const { result, rerender } = setup({ timeline: base, ready: true, away: true, resetKey: "i1" });
    rerender({
      timeline: [...base, comment("mine", { type: "member", id: ME }), comment("theirs", { type: "member", id: "other" })],
      ready: true,
      away: true,
      resetKey: "i1",
    });
    expect(result.current.ids).toEqual(["theirs"]);
  });

  it("empties once the reader is back and drops comments deleted meanwhile", () => {
    const base = [comment("c1")];
    const { result, rerender } = setup({ timeline: base, ready: true, away: true, resetKey: "i1" });
    rerender({ timeline: [...base, comment("c2"), comment("c3")], ready: true, away: true, resetKey: "i1" });
    rerender({
      timeline: [...base, comment("c2", {}, { deleted_at: "2026-11-10T00:01:00Z" }), comment("c3")],
      ready: true,
      away: true,
      resetKey: "i1",
    });
    expect(result.current.ids).toEqual(["c3"]);

    rerender({ timeline: [...base, comment("c3")], ready: true, away: false, resetKey: "i1" });
    expect(result.current.ids).toEqual([]);
  });

  it("clears on demand", () => {
    const base = [comment("c1")];
    const { result, rerender } = setup({ timeline: base, ready: true, away: true, resetKey: "i1" });
    rerender({ timeline: [...base, comment("c2")], ready: true, away: true, resetKey: "i1" });
    act(() => result.current.clear());
    expect(result.current.ids).toEqual([]);
  });

  it("starts a fresh baseline for another issue", () => {
    const { result, rerender } = setup({ timeline: [comment("a1")], ready: true, away: true, resetKey: "i1" });
    rerender({ timeline: [comment("b1"), comment("b2")], ready: true, away: true, resetKey: "i2" });
    expect(result.current.ids).toEqual([]);
    rerender({ timeline: [comment("b1"), comment("b2"), comment("b3")], ready: true, away: true, resetKey: "i2" });
    expect(result.current.ids).toEqual(["b3"]);
  });
});
