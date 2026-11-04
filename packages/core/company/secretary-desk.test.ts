// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue } from "../types";
import { directiveQuote, secretaryDeskItems } from "./secretary-desk";

function issue(id: string, createdAt: string, metadata: Record<string, unknown> = {}): Issue {
  return { id, created_at: createdAt, metadata } as unknown as Issue;
}

describe("directiveQuote", () => {
  it("returns the stored quote, trimmed", () => {
    expect(directiveQuote({ directive: { quote: "  ship the pricing page  " } })).toBe("ship the pricing page");
  });

  it("returns null when the directive or its quote is absent or blank", () => {
    expect(directiveQuote({})).toBeNull();
    expect(directiveQuote({ directive: {} })).toBeNull();
    expect(directiveQuote({ directive: { quote: "   " } })).toBeNull();
    expect(directiveQuote(null)).toBeNull();
    expect(directiveQuote({ directive: "not an object" })).toBeNull();
  });
});

describe("secretaryDeskItems", () => {
  it("merges directives and requests, newest first, with the quote on directives only", () => {
    const items = secretaryDeskItems(
      [issue("d-old", "2026-11-01T00:00:00Z", { directive: { quote: "old" } }), issue("d-new", "2026-11-03T00:00:00Z", { directive: { quote: "new" } })],
      [issue("r-mid", "2026-11-02T00:00:00Z", { directive: { quote: "ignored" } })],
    );
    expect(items.map((item) => [item.issue.id, item.kind, item.quote])).toEqual([
      ["d-new", "directive", "new"],
      ["r-mid", "request", null],
      ["d-old", "directive", "old"],
    ]);
  });
});
