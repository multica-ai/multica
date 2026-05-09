// Phase 7b — Release schema + merge_state schema contract tests.
//
// These exercise the API-compat layer for the new merge train fields.
// Per CLAUDE.md "API Response Compatibility", every endpoint that
// adds new fields needs at least one test that feeds a malformed
// response through the schema and confirms the fallback fires
// instead of throwing.

import { describe, it, expect } from "vitest";
import {
  ReleaseSchema,
  MergeStateResponseSchema,
  EMPTY_MERGE_STATE_RESPONSE,
} from "../api/schemas";
import { parseWithFallback } from "../api/schema";

describe("ReleaseSchema (Phase 7b additions)", () => {
  it("parses a complete release with merge_paused + merge_method", () => {
    const raw = {
      id: "rel-1",
      workspace_id: "ws-1",
      project_id: "p-1",
      title: "May rollout",
      description: null,
      stage: "merging",
      risk_level: "medium",
      channel_id: null,
      issue_id: null,
      approver_id: null,
      second_approver_id: null,
      staging_deploy_id: null,
      production_deploy_id: null,
      created_by: null,
      created_at: "",
      updated_at: "",
      merged_at: null,
      staged_at: null,
      promoted_at: null,
      done_at: null,
      rollback_reason: null,
      pr_count: 3,
      merge_paused: true,
      merge_method: "squash",
    };
    const parsed = ReleaseSchema.parse(raw);
    expect(parsed.merge_paused).toBe(true);
    expect(parsed.merge_method).toBe("squash");
  });

  it("falls back to merge_paused=false / merge_method=merge when fields are absent", () => {
    // Older backend that doesn't yet send the new fields. Per the
    // API-compat contract, the schema's `.default(...)` calls fill
    // them in so the consumer never sees `undefined`.
    const raw = {
      id: "rel-1",
      workspace_id: "ws-1",
      project_id: "p-1",
      title: "Old style release",
      stage: "assembling",
      risk_level: "low",
    };
    const parsed = ReleaseSchema.parse(raw);
    expect(parsed.merge_paused).toBe(false);
    expect(parsed.merge_method).toBe("merge");
  });
});

describe("MergeStateResponseSchema", () => {
  it("parses the full poll response shape", () => {
    const raw = {
      release_id: "rel-1",
      stage: "merging",
      merge_paused: false,
      merge_method: "merge",
      merged_count: 1,
      total: 3,
      pull_requests: [
        {
          pull_request_id: "pr-a",
          position: 0,
          merge_state: "merged",
          merged_sha: "abcd1234",
          merge_error: null,
        },
        {
          pull_request_id: "pr-b",
          position: 1,
          merge_state: "merging",
          merged_sha: null,
          merge_error: null,
        },
        {
          pull_request_id: "pr-c",
          position: 2,
          merge_state: "queued",
          merged_sha: null,
          merge_error: null,
        },
      ],
    };
    const parsed = MergeStateResponseSchema.parse(raw);
    expect(parsed.merged_count).toBe(1);
    expect(parsed.pull_requests).toHaveLength(3);
    expect(parsed.pull_requests[0]?.merge_state).toBe("merged");
  });

  it("falls back to EMPTY_MERGE_STATE_RESPONSE on a non-object body", () => {
    // Simulates a server that returned `null` (e.g. a future-deleted
    // endpoint shape). parseWithFallback must NOT throw here — that
    // would crash the polling UI.
    const fallback = parseWithFallback(
      null,
      MergeStateResponseSchema,
      EMPTY_MERGE_STATE_RESPONSE,
      { endpoint: "test" },
    );
    expect(fallback).toEqual(EMPTY_MERGE_STATE_RESPONSE);
  });

  it("treats unknown merge_state values as opaque strings (forward compat)", () => {
    // A future server-side enum addition shouldn't crash the parser.
    const raw = {
      release_id: "rel-1",
      stage: "merging",
      merge_paused: false,
      merge_method: "merge",
      merged_count: 0,
      total: 1,
      pull_requests: [
        {
          pull_request_id: "pr-x",
          position: 0,
          merge_state: "exotic_new_state",
          merged_sha: null,
          merge_error: null,
        },
      ],
    };
    const parsed = MergeStateResponseSchema.parse(raw);
    expect(parsed.pull_requests[0]?.merge_state).toBe("exotic_new_state");
  });
});
