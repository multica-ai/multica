// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  CodeGraphAvailableSchema,
  CodeGraphBuildSchema,
  CodeGraphQuerySchema,
  CodeGraphRepositorySchema,
} from "./schemas";

describe("CodeGraph contracts", () => {
  it("defaults older access responses and rejects malformed grants", () => {
    expect(CodeGraphRepositorySchema.parse({}).is_workspace_shared).toBe(false);
    expect(
      CodeGraphRepositorySchema.parse({ projects: null }).projects,
    ).toEqual([]);
    expect(
      CodeGraphRepositorySchema.safeParse({ is_workspace_shared: "false" })
        .success,
    ).toBe(false);
    expect(
      CodeGraphRepositorySchema.safeParse({ projects: [{ id: "p" }] }).success,
    ).toBe(false);
    expect(
      CodeGraphAvailableSchema.parse([
        {
          id: "r",
          name: "repo",
          branch: "main",
          status: "ready",
          sources: ["future"],
        },
      ])[0]?.sources,
    ).toEqual(["unknown"]);
    expect(
      CodeGraphAvailableSchema.safeParse([{ id: "r", sources: "workspace" }])
        .success,
    ).toBe(false);
  });
  it("accepts old build history but rejects malformed cancellation metadata", () => {
    const old = {
      id: "build",
      status: "processing",
      created_at: "2026-10-19T12:00:00Z",
    };
    expect(CodeGraphBuildSchema.parse(old).cancel_requested).toBe(false);
    expect(CodeGraphBuildSchema.parse(old).phase).toBe("unknown");
    for (const phase of [
      "waiting_cloning",
      "waiting_extracting",
      "waiting_publishing",
    ]) {
      expect(CodeGraphBuildSchema.parse({ ...old, phase }).phase).toBe(phase);
    }
    expect(
      CodeGraphBuildSchema.safeParse({ ...old, cancel_requested: "false" })
        .success,
    ).toBe(false);
    expect(
      CodeGraphRepositorySchema.parse({
        progress: { ...old, phase: "cancelling", cancel_requested: true },
      }).progress?.phase,
    ).toBe("cancelling");
  });
  it("handles repository responses with missing provenance and unknown build status", () => {
    const result = CodeGraphRepositorySchema.parse({
      id: "repo",
      status: "future-stage",
    });
    expect(result.active_build_id).toBeUndefined();
    expect(result.symbol_count).toBe(0);
    expect(result.status).toBe("pending");
  });
  it("rejects malformed query provenance", () => {
    expect(CodeGraphQuerySchema.safeParse({ data: [] }).success).toBe(false);
    expect(
      CodeGraphQuerySchema.safeParse({
        repository_id: "a",
        repository_name: "A",
        build_id: "b",
        commit: "sha",
        engine_commit: "engine",
        operation: "search",
        data: [],
      }).success,
    ).toBe(true);
  });
  it("keeps unknown truncation from old servers and validates new query limits", () => {
    const response = {
      repository_id: "a",
      repository_name: "A",
      build_id: "b",
      commit: "sha",
      engine_commit: "engine",
      operation: "search",
      data: [],
    };
    expect(CodeGraphQuerySchema.parse(response).truncated).toBeUndefined();
    expect(
      CodeGraphQuerySchema.parse({ ...response, truncated: true }).truncated,
    ).toBe(true);
    expect(
      CodeGraphQuerySchema.safeParse({ ...response, truncated: "false" })
        .success,
    ).toBe(false);
  });
});
