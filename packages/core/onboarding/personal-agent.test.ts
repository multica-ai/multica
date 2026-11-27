import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  createAgent: vi.fn(),
  createChatSession: vi.fn(),
}));

vi.mock("../api", async () => {
  const { ApiError } = await vi.importActual<typeof import("../api/client")>("../api/client");
  return { api: mocks, ApiError };
});

import { QueryClient } from "@tanstack/react-query";
import { ApiError } from "../api/client";
import { chatKeys } from "../chat/queries";
import { createPersonalAgent, findPersonalAgent, uniqueAgentName } from "./personal-agent";

describe("findPersonalAgent", () => {
  it("picks an active agent the member owns, never Mika or an archived one", () => {
    const agents = [
      { id: "mika", owner_id: "u1", archived_at: null, system_key: "mika" },
      { id: "old", owner_id: "u1", archived_at: "2026-01-01T00:00:00Z", system_key: undefined },
      { id: "theirs", owner_id: "u2", archived_at: null, system_key: undefined },
      { id: "mine", owner_id: "u1", archived_at: null, system_key: undefined },
    ];
    expect(findPersonalAgent(agents, "u1")?.id).toBe("mine");
    expect(findPersonalAgent(agents.slice(0, 3), "u1")).toBeUndefined();
  });
});

describe("uniqueAgentName", () => {
  it("numbers a taken name", () => {
    expect(uniqueAgentName("Ada's assistant", [])).toBe("Ada's assistant");
    expect(uniqueAgentName("Ada's assistant", ["ada's assistant"])).toBe("Ada's assistant 2");
    expect(uniqueAgentName("Ada's assistant", ["Ada's assistant", "Ada's assistant 2"])).toBe(
      "Ada's assistant 3",
    );
  });
});

describe("createPersonalAgent", () => {
  beforeEach(() => {
    mocks.createAgent.mockReset();
    mocks.createChatSession.mockReset();
    mocks.createChatSession.mockResolvedValue({ id: "session-1" });
  });

  const target = () => ({ workspaceSlug: "acme", workspaceId: "ws-1", queryClient: new QueryClient() });

  it("creates a private agent in the named workspace and opens a chat", async () => {
    mocks.createAgent.mockResolvedValue({ id: "agent-1" });
    const queryClient = new QueryClient();
    queryClient.setQueryData(chatKeys.sessions("ws-1"), [{ id: "older" }]);
    const result = await createPersonalAgent({
      workspaceSlug: "acme",
      workspaceId: "ws-1",
      queryClient,
      runtimeId: "rt-1",
      name: " Ada's assistant ",
      description: "Ada's personal agent.",
    });
    expect(mocks.createAgent).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "Ada's assistant",
        runtime_id: "rt-1",
        visibility: "private",
        description: "Ada's personal agent.",
      }),
      "acme",
    );
    expect(mocks.createAgent.mock.calls[0]![0]).not.toHaveProperty("instructions");
    expect(mocks.createAgent.mock.calls[0]![0]).not.toHaveProperty("avatar_url");
    expect(mocks.createChatSession).toHaveBeenCalledWith({ agent_id: "agent-1" }, "acme");
    expect(result.chatSession.id).toBe("session-1");
    // The chat page drops an active session missing from the cached list.
    expect(queryClient.getQueryData(chatKeys.sessions("ws-1"))).toEqual([{ id: "session-1" }, { id: "older" }]);
  });

  it("retries with the next free number when the name was taken meanwhile", async () => {
    mocks.createAgent
      .mockRejectedValueOnce(new ApiError("taken", 409, "Conflict"))
      .mockRejectedValueOnce(new ApiError("taken", 409, "Conflict"))
      .mockResolvedValue({ id: "agent-3" });
    await createPersonalAgent({ ...target(), runtimeId: "rt-1", name: "Ada", description: "" });
    expect(mocks.createAgent.mock.calls.map((call) => call[0].name)).toEqual(["Ada", "Ada 2", "Ada 3"]);
  });

  it("keeps the avatar picked in the step", async () => {
    mocks.createAgent.mockResolvedValue({ id: "agent-1" });
    await createPersonalAgent({ ...target(), runtimeId: "rt-1", name: "Ada", description: "", avatarUrl: "emoji:🦉" });
    expect(mocks.createAgent.mock.calls[0]![0]).toMatchObject({ avatar_url: "emoji:🦉" });
  });

  it("does not retry other failures", async () => {
    mocks.createAgent.mockRejectedValue(new ApiError("nope", 403, "Forbidden"));
    await expect(
      createPersonalAgent({ ...target(), runtimeId: "rt-1", name: "Ada", description: "" }),
    ).rejects.toThrow("nope");
    expect(mocks.createAgent).toHaveBeenCalledTimes(1);
    expect(mocks.createChatSession).not.toHaveBeenCalled();
  });
});
