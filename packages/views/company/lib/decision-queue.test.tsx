/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { Proposal } from "@multica/core/company";
import { splitDecisionQueue, useDecisionQueue } from "./decision-queue";

const NOW = Date.parse("2026-11-02T12:00:00Z");
const DAY = 24 * 60 * 60 * 1000;

function proposal(id: string, patch: Partial<Proposal> = {}): Proposal {
  return { id, state: "pending", snoozed_until: null, ...patch } as Proposal;
}

describe("splitDecisionQueue", () => {
  it("keeps pending proposals not put aside in the queue", () => {
    const { needsDecision, decidedLater, waiting } = splitDecisionQueue([proposal("a")], NOW);
    expect(needsDecision.map((p) => p.id)).toEqual(["a"]);
    expect(decidedLater).toEqual([]);
    expect(waiting).toEqual([]);
  });

  it("hides a proposal snoozed past now from the queue and lists it as put aside", () => {
    const snoozed = proposal("b", { snoozed_until: new Date(NOW + 2 * DAY).toISOString() });
    const { needsDecision, decidedLater } = splitDecisionQueue([proposal("a"), snoozed], NOW);
    expect(needsDecision.map((p) => p.id)).toEqual(["a"]);
    expect(decidedLater.map((p) => p.id)).toEqual(["b"]);
  });

  it("returns a proposal to the queue once its snooze has ended", () => {
    const ended = proposal("b", { snoozed_until: new Date(NOW - 1000).toISOString() });
    const { needsDecision, decidedLater } = splitDecisionQueue([ended], NOW);
    expect(needsDecision.map((p) => p.id)).toEqual(["b"]);
    expect(decidedLater).toEqual([]);
  });

  it("lists revising proposals as waiting on the squad, and leaves out other states", () => {
    const list = [
      proposal("r", { state: "revising" }),
      proposal("e", { state: "expired" }),
      proposal("ap", { state: "approved" }),
      proposal("u", { state: "unknown" }),
    ];
    const { needsDecision, decidedLater, waiting } = splitDecisionQueue(list, NOW);
    expect(waiting.map((p) => p.id)).toEqual(["r"]);
    expect(needsDecision).toEqual([]);
    expect(decidedLater).toEqual([]);
  });
});

describe("useDecisionQueue", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("brings a put-aside proposal back to the queue when its date passes, without a refetch", () => {
    const snoozed = proposal("b", { snoozed_until: new Date(NOW + 2 * DAY).toISOString() });
    const { result } = renderHook(({ list }) => useDecisionQueue(list), { initialProps: { list: [snoozed] } });

    expect(result.current.needsDecision).toEqual([]);
    expect(result.current.decidedLater.map((p) => p.id)).toEqual(["b"]);

    act(() => {
      vi.advanceTimersByTime(2 * DAY - 1);
    });
    expect(result.current.needsDecision).toEqual([]);

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current.needsDecision.map((p) => p.id)).toEqual(["b"]);
    expect(result.current.decidedLater).toEqual([]);
  });

  it("re-arms for a snooze longer than one timer can hold", () => {
    const thirtyDays = proposal("b", { snoozed_until: new Date(NOW + 30 * DAY).toISOString() });
    const { result } = renderHook(({ list }) => useDecisionQueue(list), { initialProps: { list: [thirtyDays] } });

    act(() => {
      vi.advanceTimersByTime(30 * DAY);
    });
    expect(result.current.needsDecision.map((p) => p.id)).toEqual(["b"]);
  });

  it("follows a new list as soon as it arrives", () => {
    const { result, rerender } = renderHook(({ list }) => useDecisionQueue(list), { initialProps: { list: [proposal("a")] } });
    expect(result.current.needsDecision.map((p) => p.id)).toEqual(["a"]);

    rerender({ list: [proposal("a", { snoozed_until: new Date(NOW + DAY).toISOString() })] });
    expect(result.current.needsDecision).toEqual([]);
    expect(result.current.decidedLater.map((p) => p.id)).toEqual(["a"]);
  });
});
