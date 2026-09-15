// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import {
  EMPTY_ISSUE_TABLE_GROUPS_RESPONSE,
  EMPTY_ISSUE_TABLE_FACETS_RESPONSE,
  IssueTableFacetsResponseSchema,
  IssueTableGroupsResponseSchema,
} from "./schemas";

describe("table category wire compatibility", () => {
  it.each(["in_review", "in_progress", "cancelled", "started", "closed", "custom_qa"])(
    "preserves %s without changing the corresponding group key",
    (status) => {
      const key = `status_category:${status}`;
      const parsed = IssueTableGroupsResponseSchema.parse({
        query_fingerprint: "query",
        total: 2,
        groups: [{ key, count: 2, value: { kind: "status", status } }],
      });
      expect(parsed.groups[0]).toEqual({ key, count: 2, value: { kind: "status", status } });
    },
  );

  it("falls back on malformed group values before UI filtering", () => {
    const parsed = parseWithFallback({
      query_fingerprint: "query", total: 2,
      groups: [{ key: "status_category:started", count: 2, value: { kind: "status", status: null } }],
    }, IssueTableGroupsResponseSchema, EMPTY_ISSUE_TABLE_GROUPS_RESPONSE, {
      endpoint: "POST /api/issues/table/groups",
    });
    expect(parsed).toEqual(EMPTY_ISSUE_TABLE_GROUPS_RESPONSE);
  });
});


describe("workflow status facets", () => {
  it("preserves node identity and rejects malformed counts", () => {
    const payload = { query_fingerprint: "query", total: 1, facets: [
      { kind: "workflow_status", values: [{ key: "node-1", count: 1 }] },
    ] };
    expect(IssueTableFacetsResponseSchema.parse(payload)).toEqual(payload);
    expect(parseWithFallback({ ...payload, facets: [{ kind: "workflow_status", values: [{ key: null, count: "bad" }] }] },
      IssueTableFacetsResponseSchema, EMPTY_ISSUE_TABLE_FACETS_RESPONSE, { endpoint: "POST /api/issues/table/facets" },
    )).toEqual(EMPTY_ISSUE_TABLE_FACETS_RESPONSE);
  });
});
