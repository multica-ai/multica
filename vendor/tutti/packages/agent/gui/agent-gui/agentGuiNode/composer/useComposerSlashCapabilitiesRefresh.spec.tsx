import { describe, expect, it, vi } from "vitest";
import { refreshComposerSlashCapabilities } from "./useComposerSlashCapabilitiesRefresh";

describe("refreshComposerSlashCapabilities", () => {
  it("reuses the capabilities cache when slash search is reopened", () => {
    const onRetryComposerOptions = vi.fn();
    const refreshedSessionRef = { current: null as string | null };
    const refresh = (isPaletteOpen: boolean): void =>
      refreshComposerSlashCapabilities({
        agentSessionId: "session-1",
        isPaletteOpen,
        onRetryComposerOptions,
        refreshedSessionRef,
        slashQuery: ""
      });

    refresh(true);
    expect(onRetryComposerOptions).toHaveBeenCalledTimes(1);
    refresh(false);
    refresh(true);
    expect(onRetryComposerOptions).toHaveBeenCalledTimes(2);
    expect(onRetryComposerOptions).toHaveBeenLastCalledWith({
      section: "capabilities"
    });

    refresh(true);
    expect(onRetryComposerOptions).toHaveBeenCalledTimes(2);
  });
});
