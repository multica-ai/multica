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
  ])("rejects a malformed payload without logging secrets: %j", async (raw) => {
    const warn = vi.fn();
    setSchemaLogger({ ...noopLogger, warn });
    respond(raw);
    await expect(client.getAgentEnv("a")).rejects.toThrow(
      "Invalid environment configuration response",
    );
    expect(warn).not.toHaveBeenCalled();
  });
});
