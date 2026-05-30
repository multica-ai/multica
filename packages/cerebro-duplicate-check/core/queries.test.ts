import { describe, expect, it } from "vitest";
import { parseSimilarResponse } from "./queries";

describe("parseSimilarResponse", () => {
  it("returns [] for null", () => {
    expect(parseSimilarResponse(null)).toEqual([]);
  });

  it("returns [] for a malformed top-level shape", () => {
    expect(parseSimilarResponse({ unexpected: 1 })).toEqual([]);
  });

  it("drops unrelated verdicts and keeps duplicate + related", () => {
    const raw = {
      matches: [
        { id: "1", verdict: "duplicate", title: "A" },
        { id: "2", verdict: "related", title: "B" },
        { id: "3", verdict: "unrelated", title: "C" },
      ],
    };
    const out = parseSimilarResponse(raw);
    expect(out.map((m) => m.id)).toEqual(["1", "2"]);
    expect(out[0]?.identifier).toBe(""); // missing string defaults safely
    expect(out[1]?.reason).toBe("");
  });

  it("tolerates entries with missing optional fields", () => {
    const raw = { matches: [{ id: "x", verdict: "duplicate" }] };
    const out = parseSimilarResponse(raw);
    expect(out).toHaveLength(1);
    expect(out[0]).toMatchObject({
      id: "x",
      verdict: "duplicate",
      title: "",
      status: "",
      url: "",
    });
  });

  it("drops entries with the wrong type for id (schema fail → fallback)", () => {
    // id is required as string — a numeric id fails the whole schema, so
    // parseWithFallback returns the empty fallback rather than throwing.
    const out = parseSimilarResponse({
      matches: [{ id: 7, verdict: "duplicate" }],
    });
    expect(out).toEqual([]);
  });

  it("treats an unknown verdict as non-actionable (filter, don't crash)", () => {
    const out = parseSimilarResponse({
      matches: [{ id: "x", verdict: "maybe-someday" }],
    });
    expect(out).toEqual([]);
  });
});
