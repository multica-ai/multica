import { describe, expect, it } from "vitest";
import { parseWithFallback } from "@multica/core/api/schema";
import { commandsListSchema } from "./schemas";

describe("commandsListSchema", () => {
  it("degrades a malformed response to an empty list", () => {
    const result = parseWithFallback({ commands: null }, commandsListSchema, { commands: [] }, { endpoint: "test" });
    expect(result.commands).toEqual([]);
  });
});
