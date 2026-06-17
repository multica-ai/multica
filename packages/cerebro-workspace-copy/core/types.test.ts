import { describe, expect, it } from "vitest";

import { CopyResultSchema, RelinkResultSchema } from "./types";

// Per CLAUDE.md "API Response Compatibility": the copy/relink response schemas
// must survive drift — the desktop app is older than the server it talks to.
describe("CopyResultSchema", () => {
  it("parses a full copy result", () => {
    const parsed = CopyResultSchema.parse({
      entity_type: "issue",
      source_id: "src",
      target_id: "tgt",
      source_number: 7,
      target_number: 12,
      comments_copied: 3,
      already_copied: true,
    });
    expect(parsed.target_id).toBe("tgt");
    expect(parsed.already_copied).toBe(true);
  });

  it("parses a minimal copy result (all counts omitted, omitempty)", () => {
    const parsed = CopyResultSchema.parse({
      entity_type: "agent",
      source_id: "a",
      target_id: "b",
    });
    expect(parsed.comments_copied).toBeUndefined();
    expect(parsed.cascade_copied).toBeUndefined();
  });

  it("parses a cascade copy result", () => {
    const parsed = CopyResultSchema.parse({
      entity_type: "issue",
      source_id: "src",
      target_id: "tgt",
      cascade_copied: 14,
    });
    expect(parsed.cascade_copied).toBe(14);
  });

  it("rejects a malformed result so parseWithFallback can fall back", () => {
    // target_id must be a string — a contract violation must throw here so the
    // caller's parseWithFallback returns the stub instead of white-screening.
    const result = CopyResultSchema.safeParse({
      entity_type: "issue",
      source_id: "src",
      target_id: 123,
    });
    expect(result.success).toBe(false);
  });
});

describe("RelinkResultSchema", () => {
  it("parses a relink result", () => {
    const parsed = RelinkResultSchema.parse({
      parents_relinked: 2,
      projects_relinked: 1,
    });
    expect(parsed.parents_relinked).toBe(2);
  });

  it("tolerates an empty relink result (both counts omitted)", () => {
    const parsed = RelinkResultSchema.parse({});
    expect(parsed.parents_relinked).toBeUndefined();
    expect(parsed.projects_relinked).toBeUndefined();
  });

  it("parses the combined relink + rewrite counts", () => {
    const parsed = RelinkResultSchema.parse({
      parents_relinked: 2,
      projects_relinked: 1,
      issues_rewritten: 5,
      comments_rewritten: 9,
    });
    expect(parsed.issues_rewritten).toBe(5);
    expect(parsed.comments_rewritten).toBe(9);
  });
});
