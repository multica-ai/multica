// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import {
  EMPTY_ISSUE_TABLE_GROUPS_RESPONSE,
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
