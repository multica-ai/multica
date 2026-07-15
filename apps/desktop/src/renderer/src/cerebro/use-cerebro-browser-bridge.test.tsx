import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  enabled: false,
  ensureControlServer: vi.fn<() => Promise<void>>(() => Promise.resolve()),
  onOpenTab: vi.fn<() => () => void>(() => vi.fn()),
  push: vi.fn(),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => mocks.enabled,
}));

vi.mock("@multica/core/platform", () => ({
  getCurrentSlug: () => "workspace",
  subscribeToCurrentSlug: () => () => undefined,
}));

vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => ({ push: mocks.push }),
}));

import { useCerebroBrowserBridge } from "./use-cerebro-browser-bridge";

describe("useCerebroBrowserBridge", () => {
  beforeEach(() => {
    mocks.enabled = false;
    mocks.ensureControlServer.mockClear();
    mocks.onOpenTab.mockClear();
    mocks.push.mockClear();
    Object.defineProperty(window, "cerebroBrowser", {
      configurable: true,
      value: {
        ensureControlServer: mocks.ensureControlServer,
        onOpenTab: mocks.onOpenTab,
      },
    });
  });

  it("starts the diagnostic control server while the Browser feature is disabled", async () => {
    renderHook(() => useCerebroBrowserBridge());

    await waitFor(() => expect(mocks.ensureControlServer).toHaveBeenCalledOnce());
    expect(mocks.onOpenTab).not.toHaveBeenCalled();
  });
});
