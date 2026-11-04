import { describe, expect, it } from "vitest";
import {
  AssignSquadProjectResponseSchema,
  EMPTY_ASSIGN_SQUAD_PROJECT_RESPONSE,
  parseSquadProjectMapping,
} from "./schemas";
import { parseWithFallback } from "../api/schema";

describe("parseSquadProjectMapping", () => {
  it("returns the statuses a 409 asks the caller to map", () => {
    const body = {
      code: "workflow_status_mapping_required",
      error: "choose a destination status",
      plan: {
        required: [{ status_key: "backlog", issue_count: 3, suggested_status_key: "todo" }],
        unchanged: [],
        total_issues: 3,
        affected_issues: 3,
      },
    };
    expect(parseSquadProjectMapping(body)).toEqual([
      { status_key: "backlog", issue_count: 3, suggested_status_key: "todo" },
    ]);
  });

  it("returns null for any other error body", () => {
    expect(parseSquadProjectMapping({ code: "project_workflow_set" })).toBeNull();
    expect(parseSquadProjectMapping(null)).toBeNull();
  });
});

describe("AssignSquadProjectResponseSchema", () => {
  it("falls back to the empty response on a malformed body", () => {
    const parsed = parseWithFallback({ project: { id: 1 } }, AssignSquadProjectResponseSchema, EMPTY_ASSIGN_SQUAD_PROJECT_RESPONSE, {
      endpoint: "test",
    });
    expect(parsed).toBe(EMPTY_ASSIGN_SQUAD_PROJECT_RESPONSE);
  });

  it("defaults a missing squad_id to null", () => {
    const parsed = AssignSquadProjectResponseSchema.parse({ project: { id: "p1" } });
    expect(parsed.project.squad_id).toBeNull();
  });
});
