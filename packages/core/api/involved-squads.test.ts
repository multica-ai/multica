// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

const list = (body: unknown) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(body)));
  return new ApiClient("https://api.example.test").listInvolvedSquadIds();
};

describe("listInvolvedSquadIds", () => {
  it("returns the ids and tolerates extra fields", async () => {
    await expect(list({ squad_ids: ["s-1", "s-2"], extra: true })).resolves.toEqual(["s-1", "s-2"]);
  });

  it("drops entries that are not ids", async () => {
    await expect(list({ squad_ids: ["s-1", 7, null, "", "s-2"] })).resolves.toEqual(["s-1", "s-2"]);
  });

  // An empty fallback would make "my agents" hide every squad row; failing
  // lets the surface show its retryable error instead.
  it.each([
    ["a missing list", {}],
    ["a non-array list", { squad_ids: "s-1" }],
    ["a null body", null],
  ])("fails for %s", async (_name, body) => {
    await expect(list(body)).rejects.toThrow("malformed");
  });
});
