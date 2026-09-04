// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  canCustomizeConversationStarters,
  selectConversationStarters,
} from "./conversation-starters";

const fallback = [
  { label: "Fallback one", prompt: "Fallback prompt one." },
  { label: "Fallback two", prompt: "Fallback prompt two." },
];

describe("selectConversationStarters", () => {
  it("falls back when the agent configured nothing", () => {
    expect(selectConversationStarters([], fallback)).toEqual({
      starters: fallback,
      isFallback: true,
    });
    expect(selectConversationStarters(undefined, fallback).isFallback).toBe(true);
    expect(selectConversationStarters(null, fallback).isFallback).toBe(true);
  });

  it("keeps only rows where both halves carry text", () => {
    const result = selectConversationStarters(
      [
        { label: "Review the release PR", prompt: "Review it and list risks." },
        { label: "  ", prompt: "Orphan prompt." },
        { label: "Orphan label", prompt: "   " },
      ],
      fallback,
    );

    expect(result).toEqual({
      starters: [
        { label: "Review the release PR", prompt: "Review it and list risks." },
      ],
      isFallback: false,
    });
  });

  it("falls back when every configured row is half-filled", () => {
    expect(
      selectConversationStarters([{ label: "Orphan", prompt: "" }], fallback),
    ).toEqual({ starters: fallback, isFallback: true });
  });
});

describe("canCustomizeConversationStarters", () => {
  const live = { archived_at: null };
  const allowed = { conversationStartersSupported: true, canEditAgent: true };

  it("offers the editor to someone who may edit a live agent", () => {
    expect(canCustomizeConversationStarters(live, allowed)).toBe(true);
  });

  it("stays silent for a reader", () => {
    expect(
      canCustomizeConversationStarters(live, { ...allowed, canEditAgent: false }),
    ).toBe(false);
  });

  it("stays silent on a backend that drops conversation starters", () => {
    expect(
      canCustomizeConversationStarters(live, {
        ...allowed,
        conversationStartersSupported: false,
      }),
    ).toBe(false);
  });

  it("stays silent for an archived agent, editor or not", () => {
    expect(
      canCustomizeConversationStarters({ archived_at: "2026-08-01T00:00:00Z" }, allowed),
    ).toBe(false);
  });

  it("stays silent while no agent is resolved", () => {
    expect(canCustomizeConversationStarters(null, allowed)).toBe(false);
  });
});
