// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, BatchOpsUnsupportedError } from "./client";

afterEach(() => vi.unstubAllGlobals());

function respond(body: unknown) {
  const fetch = vi.fn().mockResolvedValue(Response.json(body));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

const client = () => new ApiClient("https://api.example.test");

describe("batchUpdateIssues", () => {
  it("sends label and property operations and parses per-issue results", async () => {
    const fetch = respond({
      updated: 1,
      results: [
        { issue_id: "a", identifier: "MUL-1", outcome: "updated" },
        { issue_id: "b", identifier: "MUL-2", outcome: "skipped", code: "not_found", message: "issue not found" },
      ],
    });
    await expect(
      client().batchUpdateIssues(["a", "b"], {}, { labels: { add: ["l1"] }, properties: { unset: ["p1"] } }),
    ).resolves.toEqual({
      updated: 1,
      results: [
        { issue_id: "a", identifier: "MUL-1", outcome: "updated" },
        { issue_id: "b", identifier: "MUL-2", outcome: "skipped", code: "not_found", message: "issue not found" },
      ],
    });
    const [url, init] = fetch.mock.calls[0]!;
    expect(new URL(url).pathname).toBe("/api/issues/batch-update");
    expect(JSON.parse(init.body)).toEqual({
      issue_ids: ["a", "b"],
      updates: {},
      labels: { add: ["l1"] },
      properties: { unset: ["p1"] },
    });
  });

  it("keeps an older server's bare count for field updates", async () => {
    respond({ updated: 2 });
    await expect(client().batchUpdateIssues(["a", "b"], { priority: "high" })).resolves.toEqual({ updated: 2 });
  });

  it("refuses an older server's bare count for label or property operations", async () => {
    respond({ updated: 2 });
    await expect(client().batchUpdateIssues(["a"], {}, { labels: { clear: true } })).rejects.toBeInstanceOf(
      BatchOpsUnsupportedError,
    );
  });

  it("reads an outcome it does not know as skipped, never as success", async () => {
    respond({ updated: 1, results: [{ issue_id: "a", outcome: "queued", message: "later" }] });
    const response = await client().batchUpdateIssues(["a"], {}, { labels: { add: ["l1"] } });
    expect(response.results).toEqual([{ issue_id: "a", outcome: "skipped", message: "later" }]);
  });

  it("tolerates malformed optional fields on a result", async () => {
    respond({ updated: 1, dry_run: "yes", results: [{ issue_id: "a", outcome: "updated", identifier: 7, code: null }] });
    await expect(client().batchUpdateIssues(["a"], {})).resolves.toEqual({
      updated: 1,
      results: [{ issue_id: "a", outcome: "updated" }],
    });
  });

  it.each([
    null,
    {},
    { updated: "2" },
    { updated: -1 },
    { updated: 1, results: "none" },
    { updated: 1, results: [{ outcome: "updated" }] },
    { updated: 1, results: [{ issue_id: "a" }] },
  ])("reports a malformed response as unconfirmed, not as success: %j", async (body) => {
    respond(body);
    await expect(client().batchUpdateIssues(["a"], {}, { labels: { add: ["l1"] } })).resolves.toEqual({
      updated: 0,
      unconfirmed: true,
    });
  });
});
