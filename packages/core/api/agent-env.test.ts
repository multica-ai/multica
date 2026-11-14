// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { setSchemaLogger } from "./schema";
import { noopLogger } from "../logger";

afterEach(() => {
  vi.unstubAllGlobals();
  setSchemaLogger(noopLogger);
});

function respond(body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("agent environment response boundary", () => {
  const client = new ApiClient("https://api.example.test");
  it.each(["get", "update"])(
    "preserves all own string keys on %s",
    async (method) => {
      // JSON.parse creates an own __proto__ property, unlike an object literal.
      const raw = JSON.parse(
        '{"agent_id":"a","custom_env":{"__proto__":"existing-value","constructor":"constructor-value","prototype":"prototype-value","SAFE":"safe-value"},"custom_env_notes":{"__proto__":"existing-note","constructor":"constructor-note","prototype":"prototype-note"}}',
      );
      const fetchMock = respond(raw);
      const result =
        method === "get"
          ? await client.getAgentEnv("a")
          : await client.updateAgentEnv("a", raw);
      expect(result).toEqual(raw);
      expect(Object.hasOwn(result.custom_env, "__proto__")).toBe(true);
      expect(Object.hasOwn(result.custom_env_notes!, "__proto__")).toBe(true);
      expect(Object.getPrototypeOf(result.custom_env)).toBe(Object.prototype);
      expect(Object.getPrototypeOf(result.custom_env_notes)).toBe(
        Object.prototype,
      );
      expect(Object.hasOwn(Object.prototype, "SAFE")).toBe(false);
      if (method === "update") {
        expect(JSON.parse(fetchMock.mock.calls[0]?.[1]?.body)).toEqual(raw);
      }
    },
  );

  it.each([undefined, null, {}])(
    "preserves special values without notes (%j)",
    async (notes) => {
      const raw = JSON.parse(
        '{"custom_env":{"__proto__":"existing-value","constructor":"existing-constructor","SAFE":"safe"}}',
      );
      raw.custom_env_notes = notes;
      respond(raw);
      const result = await client.getAgentEnv("a");
      expect(result.custom_env).toEqual(raw.custom_env);
      expect(Object.hasOwn(result.custom_env, "__proto__")).toBe(true);
      expect(result.custom_env_notes).toEqual(notes ?? undefined);
    },
  );

  it("accepts older servers without notes and round-trips separate metadata", async () => {
    respond({ agent_id: "a", custom_env: { KEY: "value" } });
    expect(await client.getAgentEnv("a")).toEqual({
      agent_id: "a",
      custom_env: { KEY: "value" },
    });
    const body = {
      custom_env: { KEY: "value" },
      custom_env_notes: { KEY: "private note" },
    };
    const fetchMock = respond({ agent_id: "a", ...body });
    expect(await client.updateAgentEnv("a", body)).toEqual({
      agent_id: "a",
      ...body,
    });
    expect(JSON.parse(fetchMock.mock.calls[0]?.[1]?.body)).toEqual(body);
  });
  it.each([
    null,
    {},
    { custom_env: { KEY: 12 } },
    { custom_env: { KEY: "secret" }, custom_env_notes: { KEY: 12 } },
    { custom_env: { KEY: "secret" }, custom_env_notes: "private note" },
    { custom_env: [] },
    { custom_env: null },
    { custom_env: { KEY: "secret" }, custom_env_notes: [] },
    JSON.parse('{"custom_env":{"__proto__":12}}'),
    JSON.parse(
      '{"custom_env":{"SAFE":"secret"},"custom_env_notes":{"__proto__":{"polluted":true}}}',
    ),
  ])("rejects a malformed payload without logging secrets: %j", async (raw) => {
    const warn = vi.fn();
    setSchemaLogger({ ...noopLogger, warn });
    respond(raw);
    await expect(client.getAgentEnv("a")).rejects.toThrow(
      "Invalid environment configuration response",
    );
    expect(warn).not.toHaveBeenCalled();
    expect(Object.hasOwn(Object.prototype, "polluted")).toBe(false);
  });
});
