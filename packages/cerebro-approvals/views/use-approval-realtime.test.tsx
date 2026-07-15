import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const invalidateQueries = vi.fn();
const handlers = new Map<string, () => void>();
const featureFlagState = { enabled: true };

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries }),
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: () => void) => handlers.set(event, handler),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFlagValue: () => featureFlagState.enabled,
}));

import { ApprovalRealtime } from "./use-approval-realtime";

beforeEach(() => {
  invalidateQueries.mockReset();
  handlers.clear();
  featureFlagState.enabled = true;
});

describe("ApprovalRealtime", () => {
  it("does not subscribe when the approvals feature flag is off", () => {
    featureFlagState.enabled = false;
    render(<ApprovalRealtime wsId="workspace-1" />);
    expect(handlers).toHaveLength(0);
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
