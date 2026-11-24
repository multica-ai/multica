// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ActiveRuntimeUpdateResponseSchema, RuntimeUpdateSchema } from "./schemas";

describe("RuntimeUpdateSchema", () => {
  it("parses a queued update", () => {
    const parsed = RuntimeUpdateSchema.parse({
      id: "u1",
      runtime_id: "r1",
      status: "scheduled",
      target_version: "v0.12.0",
      active_tasks: 2,
      cancellable: true,
      created_at: "2026-11-20T00:00:00Z",
      updated_at: "2026-11-20T00:00:00Z",
    });
    expect(parsed).toMatchObject({ status: "scheduled", active_tasks: 2, cancellable: true });
  });

  it("degrades unknown and missing fields instead of failing", () => {
    const parsed = RuntimeUpdateSchema.parse({ id: "u1", status: "paused_by_future_server", active_tasks: "two" });
    expect(parsed).toMatchObject({ status: "pending", cancellable: false, active_tasks: undefined, target_version: "" });
  });

  it("rejects a response without an id", () => {
    expect(RuntimeUpdateSchema.safeParse({ status: "pending" }).success).toBe(false);
  });
});

describe("ActiveRuntimeUpdateResponseSchema", () => {
  it("falls back to no update when the payload is unusable", () => {
    expect(ActiveRuntimeUpdateResponseSchema.parse({ update: null })).toEqual({ update: null });
    expect(ActiveRuntimeUpdateResponseSchema.parse({ update: "nope" })).toEqual({ update: null });
  });
});
