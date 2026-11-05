// @vitest-environment node
import { randomUUID } from "node:crypto";
import { describe, expect, it } from "vitest";
import { jobSchema, jsonSchema } from "./index.js";

describe("verification boundary schemas", () => {
  it("requires actual output for completed jobs and diagnostics for unsuccessful terminal states", () => {
    const base = {
      schemaVersion: 1,
      jobId: randomUUID(),
      receiptId: randomUUID(),
      operation: "test",
      resourceRefs: [],
      phase: "test",
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };
    expect(jobSchema.safeParse({ ...base, state: "completed" }).success).toBe(
      false,
    );
    expect(
      jobSchema.safeParse({
        ...base,
        state: "completed",
        output: { schema: "test/v1", value: null },
      }).success,
    ).toBe(true);
    expect(jobSchema.safeParse({ ...base, state: "interrupted" }).success).toBe(
      false,
    );
    expect(
      jobSchema.safeParse({ ...base, state: "new-server-state" }).success,
    ).toBe(false);
  });

  it("rejects non-JSON values rather than silently changing persisted command input", () => {
    expect(jsonSchema.safeParse({ value: undefined }).success).toBe(false);
    expect(jsonSchema.safeParse({ value: Number.NaN }).success).toBe(false);
    expect(jsonSchema.safeParse({ value: Infinity }).success).toBe(false);
  });
});
