// @vitest-environment node
import { describe, it, expect, vi } from "vitest";
import { ComputerBroker, type BrokerOptions } from "./broker";

const scope = { userId: "u", workspaceId: "w" };
const request = (op: string, action?: unknown, turnId = "turn", conversationId = "c") => ({ op, scope, conversationId, turnId, ...(action ? { action } : {}) });
const signal = () => new AbortController().signal;
const apps: Record<string, { id: string; name: string }> = { Example: { id: "example.app", name: "Example" }, Other: { id: "other.app", name: "Other" } };
function fixture(approve = true) {
  const native = { generation: 0 };
  const options: BrokerOptions = {
    currentUser: () => "u", generation: () => 1,
    native: vi.fn(async (action) => action.op === "status" ? { available: true, accessibility: true, screenRecording: true } : action.op === "get_app" ? apps[action.app as string] : { status: "applied" }),
    nativeGeneration: () => native.generation, forgetNative: vi.fn(),
    confirm: vi.fn(async () => approve), permissions: vi.fn(async () => false), activity: vi.fn(), stopNative: vi.fn(),
    stopped: vi.fn(),
  };
  return { options, native, broker: new ComputerBroker(options) };
}
async function holding(broker: ComputerBroker, app: string, conversationId = "c") {
  await broker.handle(request("begin", undefined, "turn", conversationId), signal());
  await broker.handle(request("action", { op: "get_app", app }, "turn", conversationId), signal());
}
describe("Computer Use broker", () => {
  it("requires a real app approval before reading or acting", async () => {
    const { broker, options } = fixture();
    await broker.handle(request("begin"), signal());
    await expect(broker.handle(request("action", { op: "observe", app: "example.app" }), signal())).rejects.toThrow("app_not_allowed");
    await broker.handle(request("action", { op: "get_app", app: "Example" }), signal());
    await broker.handle(request("action", { op: "observe", app: "example.app" }), signal());
    expect(options.confirm).toHaveBeenCalledTimes(1);
  });
  it("does not launch or expose a declined app", async () => {
    const { broker, options } = fixture(false);
    await broker.handle(request("begin"), signal());
    await expect(broker.handle(request("action", { op: "get_app", app: "Example" }), signal())).rejects.toThrow("declined");
    expect(vi.mocked(options.native).mock.calls.some(([a]) => a.op === "launch")).toBe(false);
  });
  it("rejects forged approvals and wrong accounts", async () => {
    const { broker } = fixture();
    await expect(broker.handle({ ...request("begin"), scope: { userId: "other", workspaceId: "w" } }, signal())).rejects.toThrow("account_changed");
    await broker.handle(request("begin"), signal());
    await expect(broker.handle(request("action", { op: "get_app", app: "Example", approved: true }), signal())).rejects.toThrow();
  });
  it("runs turns on different apps concurrently and keeps each app to one turn", async () => {
    const { broker, options } = fixture();
    await holding(broker, "Example", "a");
    await holding(broker, "Other", "b");
    await broker.handle(request("action", { op: "observe", app: "example.app" }, "turn", "a"), signal());
    await broker.handle(request("action", { op: "observe", app: "other.app" }, "turn", "b"), signal());
    await expect(broker.handle(request("action", { op: "get_app", app: "Example" }, "turn", "b"), signal())).rejects.toThrow("app_busy");
    await expect(broker.handle(request("action", { op: "observe", app: "example.app" }, "turn", "b"), signal())).rejects.toThrow("app_not_allowed");
    expect(vi.mocked(options.activity).mock.lastCall?.[0]).toEqual([expect.objectContaining({ conversationId: "a", app: "Example" }), expect.objectContaining({ conversationId: "b", app: "Other" })]);
    await broker.handle(request("release", undefined, "turn", "a"), signal());
    expect(options.forgetNative).toHaveBeenCalledWith(["example.app"]);
    expect(options.stopNative).not.toHaveBeenCalled();
    await broker.handle(request("action", { op: "get_app", app: "Example" }, "turn", "b"), signal());
    await broker.handle(request("release", undefined, "turn", "b"), signal());
    expect(options.stopNative).toHaveBeenCalledTimes(1);
  });
  it("does not let a pending approval claim an app another turn took meanwhile", async () => {
    const { broker, options } = fixture();
    await broker.handle(request("begin", undefined, "turn", "a"), signal());
    options.confirm = vi.fn(async () => { options.confirm = async () => true; await holding(broker, "Example", "b"); return true; });
    await expect(broker.handle(request("action", { op: "get_app", app: "Example" }, "turn", "a"), signal())).rejects.toThrow("app_busy");
  });
  it("cancels and stops one turn without ending the others", async () => {
    const { broker, options } = fixture();
    await holding(broker, "Example", "a");
    await holding(broker, "Other", "b");
    broker.cancel(request("action", { op: "observe", app: "example.app" }, "turn", "a"));
    await expect(broker.handle(request("action", { op: "observe", app: "example.app" }, "turn", "a"), signal())).rejects.toThrow("execution_ended");
    const [b] = vi.mocked(options.activity).mock.lastCall![0];
    broker.revoke(b!.key);
    expect(options.stopped).toHaveBeenCalledWith(expect.objectContaining({ conversationId: "b" }));
    await expect(broker.handle(request("begin", undefined, "turn", "b"), signal())).rejects.toThrow("user stopped");
    await holding(broker, "Example", "c");
  });
  it("asks for the app again after the native helper restarts, without a new approval", async () => {
    const { broker, options, native } = fixture();
    await holding(broker, "Example");
    native.generation++;
    await expect(broker.handle(request("action", { op: "observe", app: "example.app" }), signal())).rejects.toThrow("native_restarted");
    await broker.handle(request("action", { op: "get_app", app: "Example" }), signal());
    await broker.handle(request("action", { op: "observe", app: "example.app" }), signal());
    expect(options.confirm).toHaveBeenCalledTimes(1);
  });
  it("revokes in-flight work and cannot reacquire a manually stopped turn", async () => {
    const { broker, options } = fixture();
    await broker.handle(request("begin"), signal());
    broker.revoke();
    await expect(broker.handle(request("begin"), signal())).rejects.toThrow("user stopped");
    await expect(broker.handle(request("action", { op: "observe", app: "example.app" }), signal())).rejects.toThrow("execution_ended");
    expect(options.stopNative).toHaveBeenCalled();
    expect(options.stopped).toHaveBeenCalledWith(expect.objectContaining({ conversationId: "c", turnId: "turn" }));
    await broker.handle(request("begin", undefined, "next-turn"), signal());
  });
  it("approves apps for this device's task runs without a dialog", async () => {
    const { broker, options } = fixture(false);
    const task = (op: string, action?: unknown) => ({ op, scope, origin: "task", conversationId: "task:t", turnId: "t", ...(action ? { action } : {}) });
    await broker.handle(task("begin"), signal());
    await broker.handle(task("action", { op: "get_app", app: "Example" }), signal());
    await broker.handle(task("action", { op: "observe", app: "example.app" }), signal());
    expect(options.confirm).not.toHaveBeenCalled();
    expect(vi.mocked(options.native).mock.calls.some(([a]) => a.op === "launch")).toBe(true);
  });
  it("requires every request, including task runs, to name the signed-in account", async () => {
    const { broker } = fixture();
    await expect(broker.handle({ ...request("begin"), scope: { workspaceId: "w" } }, signal())).rejects.toThrow();
    await expect(broker.handle({ op: "begin", scope: { userId: "other", workspaceId: "w" }, origin: "task", conversationId: "task:t", turnId: "t" }, signal())).rejects.toThrow("account_changed");
  });
  it("rechecks identity after a pending approval", async () => {
    const { broker, options } = fixture();
    await broker.handle(request("begin"), signal());
    options.confirm = async () => { broker.stop(); return true; };
    await expect(broker.handle(request("action", { op: "get_app", app: "Example" }), signal())).rejects.toThrow("execution_ended");
    expect(vi.mocked(options.native).mock.calls.some(([a]) => a.op === "launch")).toBe(false);
  });
});
