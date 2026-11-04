// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ACTIVATION_CONFIRMATION_TIMEOUT_MS, ActivationService, NEW_ACTIVATION_TIMEOUT_MS } from "./activation-service";

type Settings = Record<string, unknown>;
interface Request { mode: "new" | "existing"; requestId: string; agentSessionId: string; settings?: Settings }

function setup() {
  const activate = vi.fn<(request: Request, signal: AbortSignal) => Promise<string>>();
  const applyResult = vi.fn<(request: Request, result: string) => void>();
  const updateSettings = vi.fn(async (_id: string, _settings: Settings) => {});
  const reconcile = vi.fn<(id: string) => void>();
  const deleted = new Set<string>();
  const service = new ActivationService<Request, Settings, string>({
    workspaceId: "w", nowUnixMs: () => Date.now(), isDeleted: (id) => deleted.has(id), activate, applyResult, updateSettings, reconcile,
    describeError: (error) => ({ code: "failed_code", message: error instanceof Error ? error.message : null }),
  });
  return { service, activate, applyResult, updateSettings, reconcile, deleted };
}
const created = (requestId = "r1"): Request => ({ mode: "new", requestId, agentSessionId: "s" });
const record = (service: ReturnType<typeof setup>["service"], id = "r1") => service.activations().find((item) => item.request.requestId === id);

describe("ActivationService", () => {
  beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(1000); });
  afterEach(() => { vi.useRealTimers(); });

  it("confirms from its own answer even when the session's clock is behind", async () => {
    const { service, activate, applyResult } = setup();
    activate.mockResolvedValue("ok");
    expect(service.request(created())).toBe(true);
    expect(record(service)?.status).toBe("requested");
    expect(service.isCreating("s")).toBe(true);
    service.observeSessions([{ agentSessionId: "s", workspaceId: "w", createdAtUnixMs: 1, updatedAtUnixMs: 1 }]);
    expect(record(service)?.status).toBe("requested");
    await vi.advanceTimersByTimeAsync(0);
    expect(applyResult).toHaveBeenCalledWith(created(), "ok");
    expect(record(service)?.status).toBe("confirmed");
    expect(service.isCreating("s")).toBe(false);
  });

  it("confirms early from a session created after the request", () => {
    const { service, activate } = setup();
    activate.mockReturnValue(new Promise(() => {}));
    service.request(created());
    service.observeSessions([{ agentSessionId: "s", workspaceId: "other", createdAtUnixMs: 2000 }]);
    expect(record(service)?.status).toBe("requested");
    service.observeSessions([{ agentSessionId: "s", workspaceId: "w", createdAtUnixMs: 2000 }]);
    expect(record(service)?.status).toBe("confirmed");
  });

  it("reports the failure and refuses a request for a deleted conversation", async () => {
    const { service, activate, deleted } = setup();
    activate.mockRejectedValue(new Error("no runtime"));
    service.request(created());
    await vi.advanceTimersByTimeAsync(0);
    expect(record(service)).toMatchObject({ status: "failed", errorCode: "failed_code", errorMessage: "no runtime" });
    deleted.add("s");
    expect(service.request(created("r2"))).toBe(false);
  });

  it("goes uncertain on a timeout, asks what happened, and accepts the session that turns up", async () => {
    const { service, activate, reconcile, applyResult } = setup();
    let signal!: AbortSignal;
    let answer!: (value: string) => void;
    activate.mockImplementation((_request, received) => { signal = received; return new Promise((resolve) => { answer = resolve; }); });
    service.request(created());
    await vi.advanceTimersByTimeAsync(NEW_ACTIVATION_TIMEOUT_MS);
    expect(record(service)?.status).toBe("uncertain");
    expect(signal.aborted).toBe(true);
    expect(reconcile).toHaveBeenCalledWith("s");
    answer("late");
    await vi.advanceTimersByTimeAsync(0);
    expect(applyResult).not.toHaveBeenCalled();
    service.observeSessions([{ agentSessionId: "s", workspaceId: "w", updatedAtUnixMs: 2000 }]);
    expect(record(service)?.status).toBe("confirmed");
  });

  it("fails an activation nothing ever settled", async () => {
    const { service, activate, reconcile } = setup();
    activate.mockReturnValue(new Promise(() => {}));
    service.request(created());
    await vi.advanceTimersByTimeAsync(ACTIVATION_CONFIRMATION_TIMEOUT_MS);
    expect(record(service)).toMatchObject({ status: "failed", errorCode: "activation_confirmation_expired" });
    expect(reconcile).toHaveBeenCalledTimes(2);
  });

  it("does not expire a confirmed activation", async () => {
    const { service, activate } = setup();
    activate.mockResolvedValue("ok");
    service.request(created());
    await vi.advanceTimersByTimeAsync(ACTIVATION_CONFIRMATION_TIMEOUT_MS * 2);
    expect(record(service)?.status).toBe("confirmed");
  });

  it("replaces an earlier attempt but keeps a failure until cleared", async () => {
    const { service, activate, applyResult } = setup();
    let first!: (value: string) => void;
    activate.mockImplementationOnce(() => new Promise((resolve) => { first = resolve; })).mockResolvedValue("second");
    service.request(created("r1"));
    service.request(created("r2"));
    expect(record(service, "r1")).toBeUndefined();
    await vi.advanceTimersByTimeAsync(0);
    first("first");
    await vi.advanceTimersByTimeAsync(0);
    expect(applyResult).toHaveBeenCalledTimes(1);
    expect(applyResult).toHaveBeenCalledWith(created("r2"), "second");
    service.recordFailure(created("f"), { code: "c", message: "m" });
    service.request(created("r3"));
    expect(record(service, "f")?.status).toBe("failed");
    service.clearFailure("s");
    expect(record(service, "f")).toBeUndefined();
  });

  it("sends settings chosen early once confirmed, and only the latest result counts", async () => {
    const { service, activate, updateSettings } = setup();
    let finish!: () => void;
    activate.mockReturnValue(new Promise(() => {}));
    updateSettings.mockImplementationOnce(() => new Promise((resolve) => { finish = () => resolve(); }));
    service.request(created());
    service.patchSettings("s", { model: "a" });
    expect(record(service)?.settings).toEqual({ model: "a" });
    expect(updateSettings).not.toHaveBeenCalled();
    service.observeSessions([{ agentSessionId: "s", workspaceId: "w", createdAtUnixMs: 2000 }]);
    await vi.advanceTimersByTimeAsync(0);
    expect(updateSettings).toHaveBeenCalledWith("s", { model: "a" });
    expect(record(service)?.settingsStatus).toBe("inFlight");
    updateSettings.mockRejectedValueOnce(new Error("rejected"));
    service.patchSettings("s", { effort: "high" });
    await vi.advanceTimersByTimeAsync(0);
    expect(updateSettings).toHaveBeenLastCalledWith("s", { model: "a", effort: "high" });
    expect(record(service)).toMatchObject({ settingsStatus: "failed", errorCode: "failed_code", errorMessage: "rejected" });
    finish();
    await vi.advanceTimersByTimeAsync(0);
    expect(record(service)?.settingsStatus).toBe("failed");
  });

  it("marks a conversation not started until it is requested again", () => {
    const { service, activate } = setup();
    activate.mockReturnValue(new Promise(() => {}));
    service.request(created());
    service.unactivate("s");
    expect(service.activations()).toEqual([]);
    expect(service.inactiveSessionIds()).toEqual(["s"]);
    service.request(created("r2"));
    expect(service.inactiveSessionIds()).toEqual([]);
    service.removeSession("s");
    expect(service.activations()).toEqual([]);
  });

  it("ignores what arrives after it is disposed", async () => {
    const { service, activate, applyResult } = setup();
    let answer!: (value: string) => void;
    activate.mockImplementation(() => new Promise((resolve) => { answer = resolve; }));
    service.request(created());
    await vi.advanceTimersByTimeAsync(0);
    service.dispose();
    answer("late");
    await vi.advanceTimersByTimeAsync(0);
    expect(applyResult).not.toHaveBeenCalled();
    expect(service.request(created("r2"))).toBe(false);
  });
});
