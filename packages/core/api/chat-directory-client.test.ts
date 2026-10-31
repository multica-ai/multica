// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
afterEach(() => vi.unstubAllGlobals());
const directory = { daemon_id: "daemon", path: "/home/me/项目 files" };
function reply(body: unknown) {
  const fetchMock = vi.fn().mockImplementation(async () => new Response(JSON.stringify(body), { status: 200 }));
  vi.stubGlobal("fetch", fetchMock);
  return { client: new ApiClient("https://api.example.test"), fetchMock };
}
describe("chat directory API boundary", () => {
  it("validates create responses and retains the requested directory", async () => {
    const { client, fetchMock } = reply({ id: "session", working_directory: directory });
    expect((await client.createChatSession({ agent_id: "agent", working_directory: directory })).working_directory).toEqual(directory);
    expect(JSON.parse(fetchMock.mock.calls[0]?.[1]?.body)).toEqual({ agent_id: "agent", working_directory: directory });
  });
  it("refuses to send into a chat when a server dropped its requested directory", async () => {
    const { client } = reply({ id: "session" });
    await expect(client.createChatSession({ agent_id: "agent", working_directory: directory })).rejects.toThrow();
  });
  it("normalizes malformed create-session payloads through the shared schema", async () => {
    const { client } = reply({ id: "session", title: 42 });
    expect((await client.createChatSession({ agent_id: "agent" })).id).toBe("");
  });
  it("parses directory requests with a safe terminal failure on malformed replies", async () => {
    const { client } = reply({ status: "future" });
    expect((await client.createRuntimeDirectoryRequest("runtime", "/home/me")).status).toBe("failed");
    expect((await client.getRuntimeDirectoryRequest("runtime", "request")).status).toBe("failed");
  });
});
