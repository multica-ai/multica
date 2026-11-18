/**
 * @vitest-environment jsdom
 */
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { WorkspaceFeaturesSchema } from "../api/schemas";
import type { WorkspaceFeatures } from "../types";
import {
  isFeatureLockedError,
  refreshFeaturesOnLockedError,
  resolvePaidFeatureAccess,
  usePaidFeatureAccess,
  workspaceFeatureKeys,
} from "./paid-features";

function setup(getWorkspaceFeatures: (wsId: string) => Promise<WorkspaceFeatures>) {
  const api = { getWorkspaceFeatures: vi.fn(getWorkspaceFeatures) };
  setApiInstance(api as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return { api, qc, wrapper };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("resolvePaidFeatureAccess", () => {
  it("maps the server state and the resource's own configuration to a UI state", () => {
    expect(resolvePaidFeatureAccess("available", false)).toBe("available");
    expect(resolvePaidFeatureAccess("available", true)).toBe("available");
    expect(resolvePaidFeatureAccess("locked", true)).toBe("read_only");
    expect(resolvePaidFeatureAccess("locked", false)).toBe("unavailable");
  });
});

describe("usePaidFeatureAccess", () => {
  it("is read-only for a resource that already uses a locked feature, and never blocks reductions", async () => {
    const { wrapper } = setup(async () => ({ features: { private_projects: "locked" } }));
    const { result } = renderHook(
      () => usePaidFeatureAccess("ws-1", "private_projects", { hasExistingConfig: true }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isPending).toBe(false));
    expect(result.current).toMatchObject({ state: "read_only", canAdd: false, canReduce: true });
  });

  it("keys the read by workspace", async () => {
    const { api, wrapper } = setup(async (wsId) => ({
      features: { private_projects: wsId === "ws-locked" ? "locked" : "available" },
    }));
    const locked = renderHook(
      () => usePaidFeatureAccess("ws-locked", "private_projects", { hasExistingConfig: false }),
      { wrapper },
    );
    const open = renderHook(
      () => usePaidFeatureAccess("ws-open", "private_projects", { hasExistingConfig: false }),
      { wrapper },
    );
    await waitFor(() => expect(locked.result.current.state).toBe("unavailable"));
    await waitFor(() => expect(open.result.current.state).toBe("available"));
    expect(api.getWorkspaceFeatures).toHaveBeenCalledWith("ws-locked");
    expect(api.getWorkspaceFeatures).toHaveBeenCalledWith("ws-open");
  });

  it("offers no additions while loading, so a locked form never flashes as editable", () => {
    const { wrapper } = setup(() => new Promise<WorkspaceFeatures>(() => {}));
    const { result } = renderHook(
      () => usePaidFeatureAccess("ws-1", "private_projects", { hasExistingConfig: false }),
      { wrapper },
    );
    expect(result.current).toMatchObject({ state: null, isPending: true, canAdd: false });
  });

  it("leaves additions to the server when the read fails, without an upgrade prompt", async () => {
    const { wrapper } = setup(async () => {
      throw new Error("offline");
    });
    const { result } = renderHook(
      () => usePaidFeatureAccess("ws-1", "private_projects", { hasExistingConfig: false }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current).toMatchObject({ state: null, canAdd: true });
  });

  it("treats a feature the server did not report as available", async () => {
    const { wrapper } = setup(async () => ({ features: {} }));
    const { result } = renderHook(
      () => usePaidFeatureAccess("ws-1", "private_projects", { hasExistingConfig: true }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.state).toBe("available"));
  });
});

describe("feature_locked errors", () => {
  const locked = new ApiError("locked", 403, "Forbidden", { code: "feature_locked", feature: "private_projects" });

  it("is recognised only from the machine-readable code", () => {
    expect(isFeatureLockedError(locked)).toBe(true);
    expect(isFeatureLockedError(new ApiError("no", 403, "Forbidden", { error: "only admins" }))).toBe(false);
    expect(isFeatureLockedError(new ApiError("no", 400, "Bad Request", { code: "feature_locked" }))).toBe(false);
    expect(isFeatureLockedError(new Error("feature_locked"))).toBe(false);
  });

  it("refetches the workspace's features so the form shows its locked state", () => {
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    refreshFeaturesOnLockedError(qc, "ws-1", new ApiError("no", 403, "Forbidden", { error: "only admins" }));
    expect(invalidate).not.toHaveBeenCalled();
    refreshFeaturesOnLockedError(qc, "ws-1", locked);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: workspaceFeatureKeys.all("ws-1") });
  });
});

describe("WorkspaceFeaturesSchema", () => {
  it("reads known states and falls back to available for anything it cannot read", () => {
    expect(
      WorkspaceFeaturesSchema.parse({
        features: {
          private_projects: { state: "locked" },
          future_feature: { state: "something_new" },
          broken: "locked",
        },
        extra: true,
      }),
    ).toEqual({
      features: { private_projects: "locked", future_feature: "available", broken: "available" },
    });
    expect(WorkspaceFeaturesSchema.parse({ features: [] })).toEqual({ features: {} });
    expect(WorkspaceFeaturesSchema.parse({})).toEqual({ features: {} });
  });
});
