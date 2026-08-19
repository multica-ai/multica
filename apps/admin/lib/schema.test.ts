import { describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { parseWithFallback } from "./schema";
import { LiteLlmKeyListSchema, LiteLlmTeamActivitySchema } from "./litellm-schema";

describe("parseWithFallback", () => {
  it("returns the parsed data when the schema matches", () => {
    const schema = z.object({ ok: z.boolean() });
    const result = parseWithFallback({ ok: true }, schema, { ok: false }, { endpoint: "/test" });
    expect(result).toEqual({ ok: true });
  });

  it("falls back and warns (not throws) on a malformed response", () => {
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const schema = z.object({ ok: z.boolean() });
    const result = parseWithFallback({ ok: "not-a-boolean" }, schema, { ok: false }, {
      endpoint: "/test",
    });
    expect(result).toEqual({ ok: false });
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining("/test"),
      expect.anything(),
    );
    warnSpy.mockRestore();
  });
});

describe("LiteLLM schema tolerance (API Response Compatibility boundary)", () => {
  it("degrades /key/list to an empty list when the response is missing entirely", () => {
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const result = parseWithFallback(
      { unexpected: "shape" },
      LiteLlmKeyListSchema,
      { keys: [] },
      { endpoint: "/key/list" },
    );
    expect(result.keys).toEqual([]);
    warnSpy.mockRestore();
  });

  it("tolerates keys missing team_alias/team_id (nullish fields)", () => {
    const result = LiteLlmKeyListSchema.safeParse({
      keys: [{ key_alias: "acme-workspace" }],
    });
    expect(result.success).toBe(true);
    expect(result.data?.keys[0]).toEqual({
      key_alias: "acme-workspace",
      team_alias: undefined,
      team_id: undefined,
    });
  });

  it("degrades /team/daily/activity to empty results on a malformed payload", () => {
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const result = parseWithFallback(
      { results: "not-an-array" },
      LiteLlmTeamActivitySchema,
      { results: [] },
      { endpoint: "/team/daily/activity" },
    );
    expect(result.results).toEqual([]);
    warnSpy.mockRestore();
  });
});
