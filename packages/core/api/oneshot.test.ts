// @vitest-environment node
import { describe, expect, it } from "vitest";
import { OneShotRunSchema } from "./schemas";
import { parseWithFallback } from "./schema";

describe("one-shot run response", () => {
  it("defaults omitted optional configuration without changing prompt bytes", () => {
    const prompt = "<request>\r\nReview.\r\n</request>\n";
    const run = OneShotRunSchema.parse({ task: { id: "run" }, input: { runtime_id: "runtime", user_prompt: prompt } });
    expect(run.input).toEqual({ runtime_id: "runtime", user_prompt: prompt, model_id: "", effort: "", skills: [], system_prompt: [] });
  });
  it.each([null, {}, { task: {}, input: {} }, { task: { id: "run" }, input: { runtime_id: "runtime", user_prompt: 42 } }])("rejects malformed responses: %j", (raw) => {
    expect(parseWithFallback(raw, OneShotRunSchema, null, { endpoint: "GET /api/tasks/oneshot/:id" })).toBeNull();
  });
});
