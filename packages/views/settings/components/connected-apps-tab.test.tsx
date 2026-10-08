import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { ApiError } from "@multica/core/api";
import { configStore } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";

const state = vi.hoisted(() => ({
  error: null as Error | null,
  calls: [] as { enabled?: boolean }[],
}));
vi.mock("@tanstack/react-query", () => ({
  queryOptions: <T,>(opts: T) => opts,
  useQuery: (opts: { enabled?: boolean }) => {
    state.calls.push(opts);
    return { error: opts.enabled === false ? null : state.error };
  },
}));

import { useComposioAvailable } from "./connected-apps-tab";

beforeEach(() => {
  state.error = null;
  state.calls = [];
  configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: true });
});

describe("useComposioAvailable", () => {
  it("offers Connected apps when the flag is on and the catalog answers", () => {
    expect(renderHook(() => useComposioAvailable()).result.current).toBe(true);
  });

  it("keeps the page while the catalog is loading or fails transiently", () => {
    state.error = new ApiError("boom", 500, "Internal Server Error");
    expect(renderHook(() => useComposioAvailable()).result.current).toBe(true);
  });

  it("hides the page and skips the request when the flag is off", () => {
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: false });
    expect(renderHook(() => useComposioAvailable()).result.current).toBe(false);
    expect(state.calls.every((call) => call.enabled === false)).toBe(true);
  });

  it("hides the page when the server reports Composio unconfigured", () => {
    state.error = new ApiError("unavailable", 403, "Forbidden", {
      code: "composio_not_configured",
    });
    expect(renderHook(() => useComposioAvailable()).result.current).toBe(false);
  });

  it("keeps hiding the page when an older server reports 503", () => {
    state.error = new ApiError("unavailable", 503, "Service Unavailable");
    expect(renderHook(() => useComposioAvailable()).result.current).toBe(false);
  });
});
