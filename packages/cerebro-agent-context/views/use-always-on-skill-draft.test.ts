// FIR-3805 — the rules that keep the "always on" draft consistent with the
// bound-skill draft on the agent Skills tab.

import { describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
import { useAlwaysOnSkillDraft } from "./use-always-on-skill-draft";

function agentWith(
  skills: Array<{ id: string; always_on?: boolean }>,
): Agent {
  return {
    skills: skills.map((s) => ({
      id: s.id,
      name: s.id,
      description: "",
      always_on: s.always_on,
    })),
  } as unknown as Agent;
}

describe("useAlwaysOnSkillDraft", () => {
  it("seeds from the agent's live always-on skills", () => {
    const { result } = renderHook(() =>
      useAlwaysOnSkillDraft(agentWith([{ id: "a", always_on: true }, { id: "b" }])),
    );
    expect(result.current.alwaysOnIds).toEqual(["a"]);
    expect(result.current.isAlwaysOn("a")).toBe(true);
    expect(result.current.isAlwaysOn("b")).toBe(false);
    expect(result.current.dirty).toBe(false);
  });

  // An older server omits always_on entirely; that must read as "not always on",
  // never as undefined leaking into the id list.
  it("treats a missing always_on field as not always-on", () => {
    const { result } = renderHook(() =>
      useAlwaysOnSkillDraft(agentWith([{ id: "a" }])),
    );
    expect(result.current.alwaysOnIds).toEqual([]);
  });

  it("marks the draft dirty when a box is ticked, and clean again when unticked", () => {
    const { result } = renderHook(() =>
      useAlwaysOnSkillDraft(agentWith([{ id: "a" }])),
    );
    act(() => result.current.setAlwaysOn("a", true));
    expect(result.current.dirty).toBe(true);
    expect(result.current.alwaysOnIds).toEqual(["a"]);

    act(() => result.current.setAlwaysOn("a", false));
    expect(result.current.dirty).toBe(false);
  });

  it("does not duplicate an id when the same box is ticked twice", () => {
    const { result } = renderHook(() =>
      useAlwaysOnSkillDraft(agentWith([{ id: "a" }])),
    );
    act(() => result.current.setAlwaysOn("a", true));
    act(() => result.current.setAlwaysOn("a", true));
    expect(result.current.alwaysOnIds).toEqual(["a"]);
  });

  it("forgets a skill that is removed from the binding draft", () => {
    const { result } = renderHook(() =>
      useAlwaysOnSkillDraft(agentWith([{ id: "a", always_on: true }])),
    );
    act(() => result.current.forget("a"));
    expect(result.current.alwaysOnIds).toEqual([]);
    expect(result.current.dirty).toBe(true);
  });

  it("discards back to the agent's live state", () => {
    const { result } = renderHook(() =>
      useAlwaysOnSkillDraft(agentWith([{ id: "a", always_on: true }, { id: "b" }])),
    );
    act(() => result.current.setAlwaysOn("b", true));
    act(() => result.current.setAlwaysOn("a", false));
    expect(result.current.dirty).toBe(true);

    act(() => result.current.discard());
    expect(result.current.alwaysOnIds).toEqual(["a"]);
    expect(result.current.dirty).toBe(false);
  });

  // The proposal must never carry a flag for a skill it is also unbinding —
  // the server drops it anyway, but the review diff would show a ghost id.
  it("narrows the proposal to skills still in the binding draft", () => {
    const { result } = renderHook(() =>
      useAlwaysOnSkillDraft(
        agentWith([{ id: "a", always_on: true }, { id: "b", always_on: true }]),
      ),
    );
    expect(result.current.proposalValue(["a"])).toEqual(["a"]);
    expect(result.current.proposalValue([])).toEqual([]);
  });
});
