// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { TaskInteractionSchema } from "./task-interaction-schema";

afterEach(() => vi.unstubAllGlobals());
describe("task interaction API", () => {
  it("defaults optional fields and preserves unknown states as non-actionable", () => {
    const value = TaskInteractionSchema.parse({ owner: "process", state: { state: "future_state", activity: 1 }, deadline: "later", updated_at: "now" });
    expect(value).toMatchObject({ commands: [], finishing: false, openingInput: "", state: { state: "future_state" } });
  });
  it("returns null for malformed responses, never enables controls on a guessed state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ owner: "x", commands: "broken" }), { status: 200 })));
    const api = new ApiClient("https://example.test");
    expect(await api.getTaskInteraction("run")).toBeNull();
  });
  it("returns null for a malformed follow-up receipt", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ run_id: 42 }), { status: 201 })));
    const api = new ApiClient("https://example.test");
    expect(await api.followupTaskInteraction("run", "key", "continue")).toBeNull();
  });
});
