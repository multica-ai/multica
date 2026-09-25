// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import type { DaemonStatus } from "../../../shared/daemon-types";

describe("useDesktopRuntimeContext", () => {
  beforeEach(() => {
    vi.resetModules();
  });

  it("keeps daemon identity when the hook remounts on another route", async () => {
    const getStatus = vi.fn<() => Promise<DaemonStatus>>().mockResolvedValue({
      state: "running",
      daemonId: "daemon-local",
      deviceName: "Studio Mac",
    });
    const daemonAPI = {
      getStatus,
      getHostName: vi.fn().mockResolvedValue("host.local"),
      onStatusChange: vi.fn(() => () => {}),
    };
    Object.defineProperty(window, "daemonAPI", {
      configurable: true,
      value: daemonAPI,
    });

    const { useDesktopRuntimeContext } = await import(
      "./use-desktop-runtime-context"
    );
    const firstRoute = renderHook(() => useDesktopRuntimeContext());
    await waitFor(() =>
      expect(firstRoute.result.current.localDaemonId).toBe("daemon-local"),
    );
    firstRoute.unmount();

    getStatus.mockResolvedValue({ state: "stopped" });
    const secondRoute = renderHook(() => useDesktopRuntimeContext());
    await waitFor(() =>
      expect(secondRoute.result.current.localMachineName).toBe("Studio Mac"),
    );
    expect(secondRoute.result.current.localDaemonId).toBe("daemon-local");
  });
});
