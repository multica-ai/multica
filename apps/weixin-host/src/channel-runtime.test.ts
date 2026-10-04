// @vitest-environment node
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { chunkText, createChannelRuntime } from "./channel-runtime.ts";
import { MulticaApiError, type MulticaClient } from "./multica-client.ts";
import { SessionStore } from "./session-store.ts";

let stateDir: string;

beforeEach(() => {
  stateDir = fs.mkdtempSync(path.join(os.tmpdir(), "weixin-host-test-"));
});

afterEach(() => {
  fs.rmSync(stateDir, { recursive: true, force: true });
});

function fakeClient(overrides: Partial<MulticaClient> = {}) {
  return {
    createChatSession: vi.fn(async () => ({ id: "chat-1" })),
    sendChatMessage: vi.fn(async () => ({ task_id: "task-1" })),
    waitForReply: vi.fn(async () => ({
      id: "m2",
      role: "assistant",
      content: "你好，我是 Mika",
      task_id: "task-1",
      failure_reason: null,
    })),
    ...overrides,
  } as unknown as MulticaClient & Record<string, ReturnType<typeof vi.fn>>;
}

function setup(client = fakeClient()) {
  const sessions = new SessionStore(stateDir);
  const runtime = createChannelRuntime({
    client,
    sessions,
    agentId: "agent-1",
    agentName: "Mika",
    stateDir,
    replyTimeoutMs: 1000,
    log: () => {},
  });
  const dispatcher = {
    deliver: vi.fn(async () => {}),
    onError: vi.fn(),
    typing: { start: vi.fn(async () => {}), stop: vi.fn(async () => {}) },
  };
  return { client, sessions, runtime, dispatcher };
}

const inbound = { Body: "在吗", From: "peer@im.wechat", AccountId: "bot-im-bot" };

describe("dispatchReplyFromConfig", () => {
  it("forwards the message to the agent and delivers its reply", async () => {
    const { client, sessions, runtime, dispatcher } = setup();

    await runtime.reply.dispatchReplyFromConfig({ ctx: inbound, dispatcher });

    expect(client.createChatSession).toHaveBeenCalledWith("agent-1", "微信 · peer");
    expect(client.sendChatMessage).toHaveBeenCalledWith("chat-1", "在吗");
    expect(dispatcher.deliver).toHaveBeenCalledWith({ text: "你好，我是 Mika" });
    expect(dispatcher.typing.start).toHaveBeenCalled();
    expect(dispatcher.typing.stop).toHaveBeenCalled();
    expect(sessions.get("bot-im-bot", "peer@im.wechat")).toBe("chat-1");
  });

  it("reuses the Chat for the same peer", async () => {
    const { client, runtime, dispatcher } = setup();

    await runtime.reply.dispatchReplyFromConfig({ ctx: inbound, dispatcher });
    await runtime.reply.dispatchReplyFromConfig({ ctx: inbound, dispatcher });

    expect(client.createChatSession).toHaveBeenCalledTimes(1);
    expect(client.sendChatMessage).toHaveBeenCalledTimes(2);
  });

  it("starts a new Chat when the stored one is gone", async () => {
    const client = fakeClient({
      sendChatMessage: vi
        .fn()
        .mockRejectedValueOnce(new MulticaApiError(404, "chat session not found"))
        .mockResolvedValue({ task_id: "task-1" }),
      createChatSession: vi.fn(async () => ({ id: "chat-2" })),
    });
    const { sessions, runtime, dispatcher } = setup(client);
    sessions.set("bot-im-bot", "peer@im.wechat", "deleted-chat");

    await runtime.reply.dispatchReplyFromConfig({ ctx: inbound, dispatcher });

    expect(client.sendChatMessage).toHaveBeenLastCalledWith("chat-2", "在吗");
    expect(sessions.get("bot-im-bot", "peer@im.wechat")).toBe("chat-2");
    expect(dispatcher.deliver).toHaveBeenCalled();
  });

  it("tells the agent about media it cannot see yet", async () => {
    const { client, runtime, dispatcher } = setup();

    await runtime.reply.dispatchReplyFromConfig({
      ctx: { ...inbound, Body: "", MediaPath: "/tmp/a.jpg", MediaType: "image/*" },
      dispatcher,
    });

    expect(client.sendChatMessage).toHaveBeenCalledWith(
      "chat-1",
      "[对方通过微信发送了一张图片，当前版本还不能转发给你]",
    );
  });

  it("reports a failed agent turn instead of staying silent", async () => {
    const client = fakeClient({
      waitForReply: vi.fn(async () => ({
        id: "m2",
        role: "assistant",
        content: "",
        task_id: "task-1",
        failure_reason: "runtime offline",
      })),
    });
    const { runtime, dispatcher } = setup(client);

    await runtime.reply.dispatchReplyFromConfig({ ctx: inbound, dispatcher });

    expect(dispatcher.deliver).toHaveBeenCalledWith({
      text: "⚠️ Mika 没能完成这次回复：runtime offline",
    });
  });

  it("hands transport errors to the plugin's error notice", async () => {
    const client = fakeClient({ waitForReply: vi.fn().mockRejectedValue(new Error("timed out")) });
    const { runtime, dispatcher } = setup(client);

    await runtime.reply.dispatchReplyFromConfig({ ctx: inbound, dispatcher });

    expect(dispatcher.onError).toHaveBeenCalledWith(expect.any(Error), { kind: "final" });
    expect(dispatcher.typing.stop).toHaveBeenCalled();
  });
});

describe("chunkText", () => {
  it("keeps short text whole", () => {
    expect(chunkText("hello", 10)).toEqual(["hello"]);
  });

  it("prefers paragraph boundaries", () => {
    expect(chunkText("aaaa\n\nbbbb", 8)).toEqual(["aaaa", "bbbb"]);
  });

  it("hard-splits text without boundaries", () => {
    expect(chunkText("abcdefghij", 4)).toEqual(["abcd", "efgh", "ij"]);
  });
});
