// @vitest-environment node

import { describe, expect, it } from "vitest";
import { selectConversationStarters } from "./conversation-starters";

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
