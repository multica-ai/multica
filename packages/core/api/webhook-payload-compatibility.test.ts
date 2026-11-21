// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { configStore } from "../config";

afterEach(() => {
  configStore.getState().setWebhookPayloadConditionsSupported(false);
  vi.unstubAllGlobals();
});

describe("webhook payload condition compatibility", () => {
  const filters = [{ event: "pull_request", conditions: [{ path: "draft", op: "equals" as const, value: false }] }];

  it("refuses condition writes before an older backend can silently drop them", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");
    await expect(client.createAutopilotTrigger("ap", { kind: "webhook", event_filters: filters })).rejects.toThrow(/does not support webhook payload conditions/);
    await expect(client.updateAutopilotTrigger("ap", "tr", { event_filters: filters })).rejects.toThrow(/does not support webhook payload conditions/);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("allows supported condition writes and keeps old event-only writes working", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => new Response('{"id":"tr"}', { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");
    await client.createAutopilotTrigger("ap", { kind: "webhook", event_filters: [{ event: "push" }] });
    await client.updateAutopilotTrigger("ap", "tr", { event_filters: [] });
    configStore.getState().setWebhookPayloadConditionsSupported(true);
    await client.createAutopilotTrigger("ap", { kind: "webhook", event_filters: filters });
    await client.updateAutopilotTrigger("ap", "tr", { event_filters: filters });
    expect(JSON.parse(String(fetchMock.mock.calls[2]?.[1]?.body)).event_filters).toEqual(filters);
    expect(JSON.parse(String(fetchMock.mock.calls[3]?.[1]?.body)).event_filters).toEqual(filters);
  });
});
