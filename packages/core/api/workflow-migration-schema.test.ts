// @vitest-environment node
import { expect, it } from "vitest";
import { EMPTY_ISSUE_LIFECYCLE_RESPONSE, IssueWorkflowResponseSchema } from "./schemas";

it("keeps migration previews typed and rejects malformed confirmation cursors", () => {
  const migration = { rows: [], issue_count: 0, view_count: 0, blocked_issue_ids: [], fingerprint: "snapshot" };
  const response = { ...EMPTY_ISSUE_LIFECYCLE_RESPONSE, dry_run: true, plan: { migration } };
  expect(IssueWorkflowResponseSchema.parse(response).plan?.migration).toEqual(migration);
  expect(IssueWorkflowResponseSchema.safeParse({ ...response, plan: { migration: { ...migration, fingerprint: 12 } } }).success).toBe(false);
  expect(IssueWorkflowResponseSchema.safeParse({ ...response, plan: { migration: { ...migration, issue_count: -1 } } }).success).toBe(false);
  expect(IssueWorkflowResponseSchema.parse(EMPTY_ISSUE_LIFECYCLE_RESPONSE).plan).toBeUndefined();
});
