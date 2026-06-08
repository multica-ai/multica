import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";
import type { Agent } from "@multica/core/types";

// Mutable mock state, varied per test.
const flagState = { on: true };
const queryState = {
  data: [] as Array<{ id: string; metadata: Record<string, unknown> }>,
  isSuccess: true,
};

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => flagState.on,
}));

vi.mock("@tanstack/react-query", () => ({
  // `runtimeListOptions` (from @multica/core/runtimes) calls queryOptions at the
  // hook's call site, so keep it as a passthrough. The hook only reads `data` +
  // `isSuccess` off the useQuery result; the options arg is ignored.
  queryOptions: (opts: unknown) => opts,
  useQuery: () => queryState,
}));

import { useQuickCreateVersionAutoSwitch } from "./use-version-auto-switch";

// Minimal Agent stub — the hook only touches `id` and `runtime_id`.
function agent(id: string, runtimeId: string): Agent {
  return { id, runtime_id: runtimeId } as unknown as Agent;
}

const OLD = "0.2.19"; // below MIN_QUICK_CREATE_CLI_VERSION (0.2.20)
const OK = "0.2.20";

beforeEach(() => {
  flagState.on = true;
  queryState.data = [];
  queryState.isSuccess = true;
});

describe("useQuickCreateVersionAutoSwitch", () => {
  it("switches to manual when the picked agent's CLI is too old", () => {
    queryState.data = [{ id: "rt1", metadata: { cli_version: OLD } }];
    const onSwitch = vi.fn();
    renderHook(() =>
      useQuickCreateVersionAutoSwitch({
        workspaceId: "ws1",
        selectedAgent: agent("a1", "rt1"),
        onSwitchToManual: onSwitch,
      }),
    );
    expect(onSwitch).toHaveBeenCalledTimes(1);
  });

  it("switches when the runtime reports no CLI version (fail closed)", () => {
    queryState.data = [{ id: "rt1", metadata: {} }];
    const onSwitch = vi.fn();
    renderHook(() =>
      useQuickCreateVersionAutoSwitch({
        workspaceId: "ws1",
        selectedAgent: agent("a1", "rt1"),
        onSwitchToManual: onSwitch,
      }),
    );
    expect(onSwitch).toHaveBeenCalledTimes(1);
  });

  it("does not switch when the CLI version is new enough", () => {
    queryState.data = [{ id: "rt1", metadata: { cli_version: OK } }];
    const onSwitch = vi.fn();
    renderHook(() =>
      useQuickCreateVersionAutoSwitch({
        workspaceId: "ws1",
        selectedAgent: agent("a1", "rt1"),
        onSwitchToManual: onSwitch,
      }),
    );
    expect(onSwitch).not.toHaveBeenCalled();
  });

  it("does not switch when the feature flag is off", () => {
    flagState.on = false;
    queryState.data = [{ id: "rt1", metadata: { cli_version: OLD } }];
    const onSwitch = vi.fn();
    renderHook(() =>
      useQuickCreateVersionAutoSwitch({
        workspaceId: "ws1",
        selectedAgent: agent("a1", "rt1"),
        onSwitchToManual: onSwitch,
      }),
    );
    expect(onSwitch).not.toHaveBeenCalled();
  });

  it("does not switch before the runtime list has loaded", () => {
    queryState.isSuccess = false;
    queryState.data = [{ id: "rt1", metadata: { cli_version: OLD } }];
    const onSwitch = vi.fn();
    renderHook(() =>
      useQuickCreateVersionAutoSwitch({
        workspaceId: "ws1",
        selectedAgent: agent("a1", "rt1"),
        onSwitchToManual: onSwitch,
      }),
    );
    expect(onSwitch).not.toHaveBeenCalled();
  });

  it("does not switch when no agent is picked", () => {
    queryState.data = [{ id: "rt1", metadata: { cli_version: OLD } }];
    const onSwitch = vi.fn();
    renderHook(() =>
      useQuickCreateVersionAutoSwitch({
        workspaceId: "ws1",
        selectedAgent: null,
        onSwitchToManual: onSwitch,
      }),
    );
    expect(onSwitch).not.toHaveBeenCalled();
  });

  it("fires at most once per picked agent across re-renders", () => {
    queryState.data = [{ id: "rt1", metadata: { cli_version: OLD } }];
    const onSwitch = vi.fn();
    const { rerender } = renderHook(() =>
      useQuickCreateVersionAutoSwitch({
        workspaceId: "ws1",
        selectedAgent: agent("a1", "rt1"),
        onSwitchToManual: onSwitch,
      }),
    );
    rerender();
    rerender();
    expect(onSwitch).toHaveBeenCalledTimes(1);
  });
});
