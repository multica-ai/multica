import { render, screen } from "@testing-library/react";
import { act } from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { UpdateNotification } from "./update-notification";

type UpdateAvailableCallback = (info: { version: string; releaseNotes?: string }) => void;
type DownloadProgressCallback = (progress: { percent: number }) => void;
type UpdateDownloadedCallback = () => void;

const callbacks = {
  updateAvailable: null as UpdateAvailableCallback | null,
  downloadProgress: null as DownloadProgressCallback | null,
  updateDownloaded: null as UpdateDownloadedCallback | null,
};

beforeEach(() => {
  callbacks.updateAvailable = null;
  callbacks.downloadProgress = null;
  callbacks.updateDownloaded = null;

  window.desktopAPI = {
    appInfo: { version: "0.2.24", os: "macos" },
    onAuthToken: vi.fn(() => vi.fn()),
    onInviteOpen: vi.fn(() => vi.fn()),
    openExternal: vi.fn(() => Promise.resolve()),
    setImmersiveMode: vi.fn(() => Promise.resolve()),
    showNotification: vi.fn(),
    setUnreadBadge: vi.fn(),
    onInboxOpen: vi.fn(() => vi.fn()),
  };

  window.updater = {
    onUpdateAvailable: vi.fn((callback: UpdateAvailableCallback) => {
      callbacks.updateAvailable = callback;
      return vi.fn();
    }),
    onDownloadProgress: vi.fn((callback: DownloadProgressCallback) => {
      callbacks.downloadProgress = callback;
      return vi.fn();
    }),
    onUpdateDownloaded: vi.fn((callback: UpdateDownloadedCallback) => {
      callbacks.updateDownloaded = callback;
      return vi.fn();
    }),
    downloadUpdate: vi.fn(() => Promise.resolve()),
    installUpdate: vi.fn(() => Promise.resolve()),
    checkForUpdates: vi.fn(() =>
      Promise.resolve({
        ok: true,
        currentVersion: "0.2.24",
        latestVersion: "0.2.25",
        available: true,
      } as const),
    ),
  };
});

describe("UpdateNotification", () => {
  it("shows immediate loading feedback after clicking Restart now", async () => {
    render(<UpdateNotification />);

    act(() => {
      callbacks.updateDownloaded?.();
    });

    const restartButton = await screen.findByRole("button", { name: "Restart now" });
    act(() => {
      restartButton.click();
    });

    expect(window.updater.installUpdate).toHaveBeenCalledOnce();
    expect(
      screen.getByRole("button", { name: /restarting/i }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "See changes" })).toBeDisabled();
  });
});
