import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { ExecApprovalManager } from "./exec-approval-manager.js";

describe("ExecApprovalManager", () => {
  let manager: ExecApprovalManager;
  let sendToClient: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.useFakeTimers();
    sendToClient = vi.fn();
    manager = new ExecApprovalManager(sendToClient, 5000); // 5s timeout for tests
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("sends approval request to client and resolves on decision", async () => {
    const promise = manager.requestApproval({
      agentId: "agent-1",
      command: "rm -rf /tmp/test",
      cwd: "/workspace",
      riskLevel: "dangerous",
      riskReasons: ["Recursive delete"],
    });

    // Verify sendToClient was called
    expect(sendToClient).toHaveBeenCalledTimes(1);
    const [agentId, request] = sendToClient.mock.calls[0]!;
    expect(agentId).toBe("agent-1");
    expect(request.command).toBe("rm -rf /tmp/test");
    expect(request.approvalId).toBeTruthy();

    // Resolve the approval
    const resolved = manager.resolveApproval(request.approvalId, "allow-once");
    expect(resolved).toBe(true);

    const result = await promise;
    expect(result.approved).toBe(true);
    expect(result.decision).toBe("allow-once");
  });

  it("resolves with deny when decision is deny", async () => {
    const promise = manager.requestApproval({
      agentId: "agent-1",
      command: "sudo reboot",
      riskLevel: "dangerous",
      riskReasons: [],
    });

    const request = sendToClient.mock.calls[0]![1];
    manager.resolveApproval(request.approvalId, "deny");

    const result = await promise;
    expect(result.approved).toBe(false);
    expect(result.decision).toBe("deny");
  });

  it("resolves with allow-always", async () => {
    const promise = manager.requestApproval({
      agentId: "agent-1",
      command: "git push",
      riskLevel: "needs-review",
      riskReasons: [],
    });

    const request = sendToClient.mock.calls[0]![1];
    manager.resolveApproval(request.approvalId, "allow-always");

    const result = await promise;
    expect(result.approved).toBe(true);
    expect(result.decision).toBe("allow-always");
  });

  it("auto-denies on timeout (fail-closed)", async () => {
    const promise = manager.requestApproval({
      agentId: "agent-1",
      command: "dangerous-command",
      riskLevel: "dangerous",
      riskReasons: [],
    });

    // Fast-forward past timeout
    vi.advanceTimersByTime(6000);

    const result = await promise;
    expect(result.approved).toBe(false);
    expect(result.decision).toBe("deny");
  });

  it("honors askFallback full on timeout", async () => {
    const promise = manager.requestApproval({
      agentId: "agent-1",
      command: "cmd",
      riskLevel: "needs-review",
      riskReasons: [],
      askFallback: "full",
    });

    vi.advanceTimersByTime(6000);

    const result = await promise;
    expect(result.approved).toBe(true);
    expect(result.decision).toBe("allow-once");
  });

  it("honors askFallback allowlist on timeout", async () => {
    const allowPromise = manager.requestApproval({
      agentId: "agent-1",
      command: "cmd",
      riskLevel: "needs-review",
      riskReasons: [],
      askFallback: "allowlist",
      allowlistSatisfied: true,
    });

    vi.advanceTimersByTime(6000);

    const allowResult = await allowPromise;
    expect(allowResult.approved).toBe(true);
    expect(allowResult.decision).toBe("allow-once");

    const denyPromise = manager.requestApproval({
      agentId: "agent-1",
      command: "cmd",
      riskLevel: "needs-review",
      riskReasons: [],
      askFallback: "allowlist",
      allowlistSatisfied: false,
    });

    vi.advanceTimersByTime(6000);

    const denyResult = await denyPromise;
    expect(denyResult.approved).toBe(false);
    expect(denyResult.decision).toBe("deny");
  });

  it("returns false when resolving unknown approval", () => {
    const resolved = manager.resolveApproval("unknown-id", "allow-once");
    expect(resolved).toBe(false);
  });

  it("returns false when resolving already-resolved approval", async () => {
    const promise = manager.requestApproval({
      agentId: "agent-1",
      command: "cmd",
      riskLevel: "needs-review",
      riskReasons: [],
    });

    const request = sendToClient.mock.calls[0]![1];

    // First resolve succeeds
    expect(manager.resolveApproval(request.approvalId, "allow-once")).toBe(true);
    // Second resolve fails
    expect(manager.resolveApproval(request.approvalId, "deny")).toBe(false);

    await promise;
  });

  it("cancels all pending approvals for an agent", async () => {
    const promise1 = manager.requestApproval({
      agentId: "agent-1",
      command: "cmd1",
      riskLevel: "needs-review",
      riskReasons: [],
    });

    const promise2 = manager.requestApproval({
      agentId: "agent-1",
      command: "cmd2",
      riskLevel: "needs-review",
      riskReasons: [],
    });

    const promise3 = manager.requestApproval({
      agentId: "agent-2",
      command: "cmd3",
      riskLevel: "needs-review",
      riskReasons: [],
    });

    // Cancel agent-1's approvals
    manager.cancelPending("agent-1");

    const result1 = await promise1;
    const result2 = await promise2;

    expect(result1.approved).toBe(false);
    expect(result1.decision).toBe("deny");
    expect(result2.approved).toBe(false);
    expect(result2.decision).toBe("deny");

    // agent-2's approval should still be pending
    expect(manager.pendingCount).toBe(1);

    // Resolve agent-2's approval
    const request3 = sendToClient.mock.calls[2]![1];
    manager.resolveApproval(request3.approvalId, "allow-once");
    const result3 = await promise3;
    expect(result3.approved).toBe(true);
  });

  it("auto-denies when sendToClient throws", async () => {
    const failingSender = vi.fn().mockImplementation(() => {
      throw new Error("Connection lost");
    });
    const failManager = new ExecApprovalManager(failingSender, 5000);

    const result = await failManager.requestApproval({
      agentId: "agent-1",
      command: "cmd",
      riskLevel: "needs-review",
      riskReasons: [],
    });

    expect(result.approved).toBe(false);
    expect(result.decision).toBe("deny");
  });

  it("getSnapshot returns request details", () => {
    manager.requestApproval({
      agentId: "agent-1",
      command: "ls",
      riskLevel: "safe",
      riskReasons: [],
    });

    const request = sendToClient.mock.calls[0]![1];
    const snapshot = manager.getSnapshot(request.approvalId);

    expect(snapshot).toBeTruthy();
    expect(snapshot!.command).toBe("ls");
    expect(snapshot!.agentId).toBe("agent-1");
  });

  it("getSnapshot returns null for unknown id", () => {
    expect(manager.getSnapshot("unknown")).toBeNull();
  });

  it("tracks pendingCount correctly", () => {
    expect(manager.pendingCount).toBe(0);

    manager.requestApproval({
      agentId: "agent-1",
      command: "cmd1",
      riskLevel: "needs-review",
      riskReasons: [],
    });
    expect(manager.pendingCount).toBe(1);

    manager.requestApproval({
      agentId: "agent-1",
      command: "cmd2",
      riskLevel: "needs-review",
      riskReasons: [],
    });
    expect(manager.pendingCount).toBe(2);

    const request = sendToClient.mock.calls[0]![1];
    manager.resolveApproval(request.approvalId, "deny");
    expect(manager.pendingCount).toBe(1);
  });
});
