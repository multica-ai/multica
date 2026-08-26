// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  canCustomizeStarterPrompts,
  selectStarterPrompts,
} from "./starter-prompts";

const fallback = [
  { label: "Fallback one", prompt: "Fallback prompt one." },
  { label: "Fallback two", prompt: "Fallback prompt two." },
];

describe("selectStarterPrompts", () => {
  it("falls back when the agent configured nothing", () => {
    expect(selectStarterPrompts([], fallback)).toEqual({
      prompts: fallback,
      isFallback: true,
    });
    expect(selectStarterPrompts(undefined, fallback).isFallback).toBe(true);
    expect(selectStarterPrompts(null, fallback).isFallback).toBe(true);
  });

  it("keeps only rows where both halves carry text", () => {
    const result = selectStarterPrompts(
      [
        { label: "Review the release PR", prompt: "Review it and list risks." },
        { label: "  ", prompt: "Orphan prompt." },
        { label: "Orphan label", prompt: "   " },
      ],
      fallback,
    );

    expect(result).toEqual({
      prompts: [
        { label: "Review the release PR", prompt: "Review it and list risks." },
      ],
      isFallback: false,
    });
  });

  it("falls back when every configured row is half-filled", () => {
    expect(
      selectStarterPrompts([{ label: "Orphan", prompt: "" }], fallback),
    ).toEqual({ prompts: fallback, isFallback: true });
  });
});

describe("canCustomizeStarterPrompts", () => {
  const live = { archived_at: null };
  const allowed = { starterPromptsSupported: true, canEditAgent: true };

  it("offers the editor to someone who may edit a live agent", () => {
    expect(canCustomizeStarterPrompts(live, allowed)).toBe(true);
  });

  it("stays silent for a reader", () => {
    expect(
      canCustomizeStarterPrompts(live, { ...allowed, canEditAgent: false }),
    ).toBe(false);
  });

  it("stays silent on a backend that drops starter prompts", () => {
    expect(
      canCustomizeStarterPrompts(live, {
        ...allowed,
        starterPromptsSupported: false,
      }),
    ).toBe(false);
  });

  it("stays silent for an archived agent, editor or not", () => {
    expect(
      canCustomizeStarterPrompts({ archived_at: "2026-08-01T00:00:00Z" }, allowed),
    ).toBe(false);
  });

  it("stays silent while no agent is resolved", () => {
    expect(canCustomizeStarterPrompts(null, allowed)).toBe(false);
  });
});
