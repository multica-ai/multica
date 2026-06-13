import { describe, expect, it } from "vitest";
import { ApiError } from "./api";
import { createQueryClient } from "./query-client";

describe("createQueryClient", () => {
  it("does not retry 404 ApiError responses", () => {
    const qc = createQueryClient();
    const retry = qc.getDefaultOptions().queries?.retry;

    expect(typeof retry).toBe("function");
    expect(
      retry instanceof Function
        ? retry(0, new ApiError("missing", 404, "Not Found"))
        : retry,
    ).toBe(false);
  });

  it("keeps one retry for transient errors", () => {
    const qc = createQueryClient();
    const retry = qc.getDefaultOptions().queries?.retry;

    expect(
      retry instanceof Function ? retry(0, new Error("boom")) : retry,
    ).toBe(true);
    expect(
      retry instanceof Function ? retry(1, new Error("boom")) : retry,
    ).toBe(false);
  });
});
