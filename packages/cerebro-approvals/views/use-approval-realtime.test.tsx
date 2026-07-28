import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const invalidateQueries = vi.fn();
const handlers = new Map<string, () => void>();
const featureFlagState = { inbox: true, gate: true };

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries }),
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: () => void) => handlers.set(event, handler),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFlagValue: (key: string) =>
    key === "cerebro_approval_gate" ? featureFlagState.gate : featureFlagState.inbox,
}));

import { ApprovalRealtime } from "./use-approval-realtime";

beforeEach(() => {
  invalidateQueries.mockReset();
  handlers.clear();
  featureFlagState.inbox = true;
  featureFlagState.gate = true;
});

describe("ApprovalRealtime", () => {
  it("does not subscribe when the approvals feature flag is off", () => {
    featureFlagState.inbox = false;
    featureFlagState.gate = false;
    render(<ApprovalRealtime wsId="workspace-1" />);
    expect(handlers).toHaveLength(0);
  });

  it("subscribes while Ask enforcement is on even if the inbox flag is off", () => {
    featureFlagState.inbox = false;
    featureFlagState.gate = true;
    render(<ApprovalRealtime wsId="workspace-1" />);
    expect(handlers.has("approval:created")).toBe(true);
  });

  it.each([
    "approval:created",
    "approval:decided",
    "approval:delegated",
    "approval:expired",
  ])("invalidates the workspace approval cache on %s", (event) => {
    render(<ApprovalRealtime wsId="workspace-1" />);
    handlers.get(event)?.();
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ["cerebro", "approvals", "workspace-1"],
    });
  });
});
