import { describe, expect, it } from "vitest";
import { safeParseCoupledNotes } from "./coupled-notes";

// FIR-1621 — the coupled-notes list shown on an issue. The API Response
// Compatibility rule requires malformed responses to fail soft, never throw.

describe("safeParseCoupledNotes", () => {
  it("unwraps the {notes:[]} envelope and defaults missing fields", () => {
    const out = safeParseCoupledNotes({ notes: [{ id: "n1", title: "Hi" }] });
    expect(out).toHaveLength(1);
    expect(out[0]!.id).toBe("n1");
    expect(out[0]!.body).toBe("");
    expect(out[0]!.visibility).toBe("private");
  });
  it("accepts a bare array too", () => {
    expect(safeParseCoupledNotes([{ id: "n2", title: "Y" }])).toHaveLength(1);
  });
  it("fails soft on garbage", () => {
    expect(safeParseCoupledNotes("nope")).toEqual([]);
    expect(safeParseCoupledNotes(null)).toEqual([]);
    expect(safeParseCoupledNotes({ notes: "not-an-array" })).toEqual([]);
  });
});
