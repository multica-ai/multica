import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { findMessageMatches } from "./channel-message-search";

function msg(id: string, content: string | null): TimelineEntry {
  return {
    id,
    type: "comment",
    content,
    actor_type: "member",
    actor_id: "u1",
    created_at: "2026-06-18T10:00:00Z",
  } as unknown as TimelineEntry;
}

describe("findMessageMatches", () => {
  const stream = [
    msg("a", "Deploy til staging er ude"),
    msg("b", "Link til Purchase product selector: https://x"),
    msg("c", "Tak, er inde nu"),
    msg("d", "Opdateret dokumentation for purchase PRODUCT selector"),
    msg("e", null),
  ];

  it("returns matching ids in chronological order", () => {
    expect(findMessageMatches(stream, "product selector")).toEqual(["b", "d"]);
  });

  it("is case-insensitive", () => {
    expect(findMessageMatches(stream, "PURCHASE")).toEqual(["b", "d"]);
  });

  it("returns nothing for an empty or whitespace query", () => {
    expect(findMessageMatches(stream, "")).toEqual([]);
    expect(findMessageMatches(stream, "   ")).toEqual([]);
  });

  it("trims the query before matching", () => {
    expect(findMessageMatches(stream, "  staging  ")).toEqual(["a"]);
  });

  it("tolerates null content", () => {
    expect(findMessageMatches(stream, "anything")).toEqual([]);
  });

  it("matches a substring anywhere in the content", () => {
    expect(findMessageMatches(stream, "dokumentation")).toEqual(["d"]);
    expect(findMessageMatches(stream, "https://")).toEqual(["b"]);
  });
});
