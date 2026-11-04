// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { SessionLoadService } from "./session-load-service";

const flush = async () => { for (let i = 0; i < 5; i++) await Promise.resolve(); };

function setup() {
  const answers: { resolve(): void; reject(error: unknown): void }[] = [];
  const load = vi.fn((_id: string) => new Promise<void>((resolve, reject) => { answers.push({ resolve, reject }); }));
  const blocked = new Set<string>();
  const service = new SessionLoadService({
    load, canLoad: (id) => !blocked.has(id),
    describeError: (error) => ({ code: "c", message: error instanceof Error ? error.message : null }),
  });
  return { service, load, answers, blocked, record: (id = "s") => service.all().find((item) => item.agentSessionId === id) };
}

describe("SessionLoadService", () => {
  it("reads once at a time and follows with what was asked for meanwhile", async () => {
    const { service, load, answers, record } = setup();
    service.request("s", { state: true, messages: false });
    service.request("s", { state: false, messages: true });
    await flush();
    expect(load).toHaveBeenCalledTimes(1);
    expect(record()).toMatchObject({ inFlight: "state", pendingMessages: true, messagesHydrated: false });
    answers[0]!.resolve();
    await flush();
    expect(load).toHaveBeenCalledTimes(2);
    expect(record()).toMatchObject({ inFlight: "messages", pendingMessages: false });
    answers[1]!.resolve();
    await flush();
    expect(record()).toMatchObject({ inFlight: null, messagesHydrated: true });
  });

  it("shows a failed read, and a later read clears it", async () => {
    const { service, answers, record } = setup();
    service.request("s", { state: true, messages: true });
    await flush();
    answers[0]!.reject(new Error("offline"));
    await flush();
    expect(record()).toMatchObject({ inFlight: null, messagesHydrated: false, errorCode: "c", errorMessage: "offline" });
    service.request("s", { state: true, messages: true });
    expect(record()).toMatchObject({ inFlight: "state_and_messages", errorMessage: null });
  });

  it("does not read what cannot be read, and hydrates from messages that arrived otherwise", async () => {
    const { service, load, blocked, record } = setup();
    blocked.add("s");
    service.request("s", { state: true, messages: true });
    await flush();
    expect(load).not.toHaveBeenCalled();
    expect(record()).toBeUndefined();
    service.markHydrated("s");
    expect(record()?.messagesHydrated).toBe(true);
  });

  it("ignores an answer for a removed conversation", async () => {
    const { service, answers, record } = setup();
    service.request("s", { state: true, messages: false });
    await flush();
    service.remove("s");
    answers[0]!.resolve();
    await flush();
    expect(record()).toBeUndefined();
  });
});
