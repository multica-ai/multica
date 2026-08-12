import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const runtimesPage = vi.fn<(props: Record<string, unknown>) => null>(() => null);
const useDesktopRuntimeContext = vi.fn();

vi.mock("@multica/views/runtimes", () => ({
  RuntimesPage: (props: Record<string, unknown>) => runtimesPage(props),
}));

vi.mock("./use-desktop-runtime-context", () => ({
  useDesktopRuntimeContext: () => useDesktopRuntimeContext(),
}));

import { DesktopRuntimesPage } from "./desktop-runtimes-page";

describe("DesktopRuntimesPage", () => {
  beforeEach(() => {
    runtimesPage.mockClear();
    useDesktopRuntimeContext.mockReturnValue({
      localDaemonId: "daemon-local",
      localMachineName: "Jiayuan's MacBook",
      bootstrapping: false,
      managedRuntimeSetup: null,
    });
  });

  it("keeps daemon controls out of the machine collection", () => {
    render(<DesktopRuntimesPage />);

    expect(runtimesPage).toHaveBeenCalledWith(
      expect.objectContaining({
        localDaemonId: "daemon-local",
        localMachineName: "Jiayuan's MacBook",
        hasLocalMachine: true,
        bootstrapping: false,
      }),
    );
    // Lifecycle belongs to the machine detail page, not the collection.
    const props = runtimesPage.mock.calls[0]?.[0] ?? {};
    for (const control of ["onStart", "onStop", "onRestart", "localMachineActions"]) {
      expect(props).not.toHaveProperty(control);
    }
  });

  it("offers the built-in runtime so skipping it in onboarding is recoverable", () => {
    render(<DesktopRuntimesPage />);

    const props = runtimesPage.mock.calls[0]?.[0] ?? {};
    expect(typeof props.onInstallBuiltInRuntime).toBe("function");
  });

  it("forwards the live install state so a failure can show its reason", () => {
    useDesktopRuntimeContext.mockReturnValue({
      localDaemonId: "daemon-local",
      localMachineName: "Jiayuan's MacBook",
      bootstrapping: false,
      managedRuntimeSetup: {
        provider: "pi",
        phase: "failed",
        startedAt: "2026-08-12T00:00:00Z",
        error: "download failed: 403",
      },
    });

    render(<DesktopRuntimesPage />);

    expect(runtimesPage).toHaveBeenCalledWith(
      expect.objectContaining({
        managedRuntimeSetup: expect.objectContaining({
          phase: "failed",
          error: "download failed: 403",
        }),
      }),
    );
  });
});
