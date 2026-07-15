import { describe, expect, it } from "vitest";
import { parseWithFallback } from "@multica/core/api";
import { evalsListSchema } from "./schemas";

describe("eval API schemas", () => {
  it("fails malformed responses closed to an empty catalog", () => {
    const result = parseWithFallback({ evals: null }, evalsListSchema, { evals: [] }, { endpoint: "test" });
    expect(result.evals).toEqual([]);
  });
});
