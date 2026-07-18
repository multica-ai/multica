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
    });
  });

  it("keeps daemon controls out of the machine collection", () => {
    render(<DesktopRuntimesPage />);

    expect(runtimesPage).toHaveBeenCalledWith({
      localDaemonId: "daemon-local",
      localMachineName: "Jiayuan's MacBook",
      hasLocalMachine: true,
      bootstrapping: false,
    });
  });
});
