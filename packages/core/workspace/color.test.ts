import { describe, it, expect } from "vitest";
import { workspaceColor, WORKSPACE_COLOR_PALETTE } from "./color";

describe("workspaceColor", () => {
  it("returns a palette entry for a valid UUID", () => {
    const color = workspaceColor("00000000-0000-0000-0000-000000000000");
    expect(WORKSPACE_COLOR_PALETTE).toContain(color);
  });

  it("is deterministic — same UUID always produces the same color", () => {
    const id = "6940266a-b0d5-43e2-b63c-d0f9b091e11f";
    expect(workspaceColor(id)).toBe(workspaceColor(id));
  });

  it("is case-insensitive on hex characters", () => {
    expect(workspaceColor("6940266A-B0D5-43E2-B63C-D0F9B091E11F")).toBe(
      workspaceColor("6940266a-b0d5-43e2-b63c-d0f9b091e11f"),
    );
  });

  it("accepts UUIDs without hyphens (the 32 hex chars are what matters)", () => {
    expect(workspaceColor("6940266ab0d543e2b63cd0f9b091e11f")).toBe(
      workspaceColor("6940266a-b0d5-43e2-b63c-d0f9b091e11f"),
    );
  });

  it("differentiates similar UUIDs", () => {
    const a = workspaceColor("00000000-0000-0000-0000-000000000001");
    const b = workspaceColor("00000000-0000-0000-0000-000000000002");
    // FNV-1a is a hash, not a counter — two adjacent UUIDs almost always
    // land in different palette buckets. We don't *require* it (a hash
    // collision in a 12-bucket modulo is normal), but checking a handful
    // of nearby IDs gives us confidence the function isn't a no-op.
    const c = workspaceColor("00000000-0000-0000-0000-000000000003");
    const d = workspaceColor("00000000-0000-0000-0000-000000000004");
    const buckets = new Set([a, b, c, d]);
    expect(buckets.size).toBeGreaterThan(1);
  });

  it("falls back to the first palette entry for invalid input", () => {
    expect(workspaceColor("not-a-uuid")).toBe(WORKSPACE_COLOR_PALETTE[0]);
    expect(workspaceColor("")).toBe(WORKSPACE_COLOR_PALETTE[0]);
  });

  // Frozen reference vectors. If these ever change, the backend palette in
  // server/internal/util/wscolor.go (MUL-4) has drifted from this file —
  // fix the divergent side, do NOT just update the test.
  //   00..00 (nil UUID) → fnv1a32 = 0x69691905 → idx 9 → #3b82f6 (blue)
  //   ff..ff (all ones) → fnv1a32 = 0x360779f5 → idx 1 → #f97316 (orange)
  it("matches frozen reference vectors", () => {
    expect(workspaceColor("00000000-0000-0000-0000-000000000000")).toBe(
      "#3b82f6",
    );
    expect(workspaceColor("ffffffff-ffff-ffff-ffff-ffffffffffff")).toBe(
      "#f97316",
    );
  });
});
