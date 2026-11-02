// @vitest-environment node
import { describe, it, expect, vi } from "vitest";
import { ComputerBroker, type BrokerOptions } from "./broker";

const scope = { userId: "u", workspaceId: "w" };
const request = (op: string, action?: unknown, turnId = "turn") => ({ op, scope, conversationId: "c", turnId, ...(action ? { action } : {}) });
const signal = () => new AbortController().signal;
function fixture(approve = true) {
  const options: BrokerOptions = {
    currentUser: () => "u", generation: () => 1,
    native: vi.fn(async (action) => action.op === "status" ? { available: true, accessibility: true, screenRecording: true } : action.op === "get_app" ? { id: "example.app", name: "Example" } : { status: "applied" }),
    confirm: vi.fn(async () => approve), permissions: vi.fn(async () => false), activity: vi.fn(), stopNative: vi.fn(),
    stopped: vi.fn(),
  };
  return { options, broker: new ComputerBroker(options) };
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
  it("rejects forged approvals, wrong accounts, and concurrent turns", async () => {
    const { broker } = fixture();
    await expect(broker.handle({ ...request("begin"), scope: { userId: "other", workspaceId: "w" } }, signal())).rejects.toThrow("account_changed");
    await broker.handle(request("begin"), signal());
    await expect(broker.handle(request("begin", undefined, "other"), signal())).rejects.toThrow("device_busy");
    await expect(broker.handle(request("action", { op: "get_app", app: "Example", approved: true }), signal())).rejects.toThrow();
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
  it("rechecks identity after a pending approval", async () => {
    const { broker, options } = fixture();
    await broker.handle(request("begin"), signal());
    options.confirm = async () => { broker.stop(); return true; };
    await expect(broker.handle(request("action", { op: "get_app", app: "Example" }), signal())).rejects.toThrow("execution_ended");
    expect(vi.mocked(options.native).mock.calls.some(([a]) => a.op === "launch")).toBe(false);
  });
});
