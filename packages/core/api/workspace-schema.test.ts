// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { WorkspaceSchema } from "./schemas";
const base = { id: "workspace", slug: "test", name: "Test", repos: [] };
describe("workspace settings response compatibility", () => {
  it("preserves valid default and unknown nested settings", () => {
    const settings = { default_chat_agent_id: "6cf8d8f8-a688-4023-bd69-e0354f32ce31", nested: { keep: [true, 1] } };
    expect(WorkspaceSchema.parse({ ...base, settings }).settings).toEqual(settings);
  });
  it.each([null, [], "legacy", 42, undefined])("normalizes historical settings %j", (settings) => {
    expect(WorkspaceSchema.parse({ ...base, settings }).settings).toEqual({});
  });
  it.each([null, [], "bad", "", 42])("ignores a malformed default %j without losing other keys", (id) => {
    const result = WorkspaceSchema.parse({ ...base, settings: { default_chat_agent_id: id, keep: true } });
    expect(result.settings.default_chat_agent_id).toBeUndefined();
    expect(result.settings.keep).toBe(true);
  });
});

afterEach(() => vi.unstubAllGlobals());
it("sends only top-level default intent and schema-guards the update response", async () => {
  const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ ...base, settings: { default_chat_agent_id: ["bad"], keep: true } }), { status: 200, headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal("fetch", fetch);
  const client = new ApiClient("https://api.example.test");
  const result = await client.updateWorkspace("workspace", { default_chat_agent_id: null });
  expect(JSON.parse(String(fetch.mock.calls[0]?.[1]?.body))).toEqual({ default_chat_agent_id: null });
  expect(result.settings.keep).toBe(true);
  expect(result.settings.default_chat_agent_id).toBeUndefined();
});
