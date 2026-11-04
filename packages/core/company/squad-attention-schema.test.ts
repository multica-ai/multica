// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import { SquadAttentionSchema } from "./squad-attention-schema";

const attention = {
  squad_id: "s1",
  pending_cap: 4,
  weekly_cap: 5,
  computed_pending_cap: 4,
  computed_weekly_cap: 5,
  pinned: false,
  pinned_pending_cap: null,
  pinned_weekly_cap: null,
  acceptance_rate: 0.72,
  sample_size: 9,
  reason: "above_threshold",
  computed_at: "2026-11-02T03:00:00Z",
};

const parse = (body: unknown) => parseWithFallback(body, SquadAttentionSchema, null, { endpoint: "t" });

describe("SquadAttentionSchema", () => {
  it("reads the effective and computed caps with the rate and reason", () => {
    expect(parse(attention)).toEqual(attention);
  });

  it("reads a pin with its caps", () => {
    expect(
      parse({ ...attention, pinned: true, pinned_pending_cap: 1, pinned_weekly_cap: 2, reason: "pinned" }),
    ).toMatchObject({ pinned: true, pinned_pending_cap: 1, pinned_weekly_cap: 2, reason: "pinned" });
  });

  it("reads an empty window with a null rate", () => {
    expect(parse({ ...attention, acceptance_rate: null, sample_size: 0, reason: "sample_too_small" })).toMatchObject({
      acceptance_rate: null,
      sample_size: 0,
      reason: "sample_too_small",
    });
  });

  it("reads a reason a newer server adds as unknown", () => {
    expect(parse({ ...attention, reason: "new_band" })).toMatchObject({ reason: "unknown" });
  });

  it("reads each known reason as itself", () => {
    for (const reason of ["pinned", "sample_too_small", "below_threshold", "above_threshold", "default"]) {
      expect(parse({ ...attention, reason })).toMatchObject({ reason });
    }
  });

  it("reads an out-of-range rate as unknown", () => {
    expect(parse({ ...attention, acceptance_rate: 72 })).toMatchObject({ acceptance_rate: null });
  });

  it("fills zero caps and an unknown reason for a sparse body", () => {
    expect(parse({ squad_id: "s1" })).toMatchObject({
      squad_id: "s1",
      pending_cap: 0,
      weekly_cap: 0,
      pinned: false,
      acceptance_rate: null,
      sample_size: 0,
      reason: "unknown",
    });
  });

  it("falls back to null when the body is not an object", () => {
    expect(parse([])).toBeNull();
    expect(parse("oops")).toBeNull();
  });
});
