// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

function stub(body: unknown) {
  const fetch = vi.fn().mockResolvedValue(Response.json(body));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("project subscription API", () => {
  it("reads the viewer's subscription", async () => {
    stub({ subscribed: true, extra: 1 });
    await expect(new ApiClient("https://api.example.test").getProjectSubscription("p")).resolves.toBe(true);
  });

  it.each([
    [true, "PUT"],
    [false, "DELETE"],
  ])("sets subscribed=%s with %s", async (subscribed, method) => {
    const fetch = stub({ subscribed });
    await expect(new ApiClient("https://api.example.test").setProjectSubscription("p", subscribed)).resolves.toBe(subscribed);
    expect(fetch.mock.calls[0]?.[0]).toContain("/api/projects/p/subscription");
    expect(fetch.mock.calls[0]?.[1]).toMatchObject({ method });
  });

  it.each([
    ["a missing flag", {}],
    ["a non-boolean flag", { subscribed: "yes" }],
    ["a null body", null],
  ])("reads %s as not subscribed", async (_name, body) => {
    stub(body);
    await expect(new ApiClient("https://api.example.test").getProjectSubscription("p")).resolves.toBe(false);
  });
});
