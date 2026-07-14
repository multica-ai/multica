import { beforeEach, describe, expect, it, vi } from "vitest";

const { cerebroRequest } = vi.hoisted(() => ({ cerebroRequest: vi.fn() }));
vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, cerebroRequest } };
});

import { listApps } from "./api";

describe("mini apps API boundary", () => {
  beforeEach(() => cerebroRequest.mockReset());

  it("keeps the catalog rendering when an older server returns a malformed app", async () => {
    cerebroRequest.mockResolvedValue({ apps: [{ id: null, status: "future-status" }] });
    await expect(listApps()).resolves.toEqual({ apps: [] });
  });
});
