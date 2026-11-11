// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

const list = (body: unknown) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(body)));
  return new ApiClient("https://api.example.test").listIssueIdsCreatedForMe();
};

describe("listIssueIdsCreatedForMe", () => {
  it("returns the ids and tolerates extra fields", async () => {
    await expect(list({ issue_ids: ["i-1", "i-2"], extra: true })).resolves.toEqual(["i-1", "i-2"]);
  });

  it("drops entries that are not ids", async () => {
    await expect(list({ issue_ids: ["i-1", 7, null, "", "i-2"] })).resolves.toEqual(["i-1", "i-2"]);
  });

  // An empty fallback would make "created" hide every quick-created row;
  // failing lets the surface show its retryable error instead.
  it.each([
    ["a missing list", {}],
    ["a non-array list", { issue_ids: "i-1" }],
    ["a null body", null],
  ])("fails for %s", async (_name, body) => {
    await expect(list(body)).rejects.toThrow("malformed");
  });
});
