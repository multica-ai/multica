// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { EMPTY_VCS_CONNECTION } from "./schemas";

afterEach(() => vi.unstubAllGlobals());

describe("updateVCSConnectionToken", () => {
  const connection = {
    id: "conn-1",
    workspace_id: "ws-1",
    provider: "gitlab",
    instance_url: "https://gitlab.example.test",
    account_login: "service-bot",
  };

  function respond(body: unknown, status = 200) {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), {
      status, headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }

  it("posts only the replacement token and parses the connection response", async () => {
    const fetchMock = respond({ ...connection, webhook_secret: "should-not-be-exposed" });
    const client = new ApiClient("https://api.example.test");
    const result = await client.updateVCSConnectionToken("ws-1", "conn-1", { access_token: "new-token" });
    expect(fetchMock).toHaveBeenCalledWith(
      "https://api.example.test/api/workspaces/ws-1/vcs/connections/conn-1/token",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ access_token: "new-token" }) }),
    );
    expect(result).toEqual({ ...connection, webhook_url: "", webhook_path: "", created_at: "" });
    expect(result).not.toHaveProperty("webhook_secret");
  });

  it.each([null, { id: 12 }, { ...connection, instance_url: [] }])(
    "safely handles a malformed response: %j", async (body) => {
      respond(body);
      await expect(new ApiClient("").updateVCSConnectionToken("ws-1", "conn-1", {
        access_token: "new-token",
      })).resolves.toEqual(EMPTY_VCS_CONNECTION);
    },
  );

  it("preserves an unknown provider and defaults malformed optional fields", async () => {
    respond({ ...connection, provider: "future-provider", account_login: null, webhook_url: 42 });
    const result = await new ApiClient("").updateVCSConnectionToken("ws-1", "conn-1", { access_token: "new-token" });
    expect(result).toMatchObject({ provider: "future-provider", account_login: "", webhook_url: "" });
  });

  it("surfaces a rejected token without treating it as a successful update", async () => {
    respond({ error: "the provider rejected the access token" }, 400);
    await expect(new ApiClient("").updateVCSConnectionToken("ws-1", "conn-1", {
      access_token: "bad-token",
    })).rejects.toThrow("the provider rejected the access token");
  });
});
