import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, act } from "@testing-library/react";

type DownloadedCb = (info: { version: string }) => void;
type AvailableCb = (info: { version: string }) => void;

const handlers = vi.hoisted(() => ({
  available: null as AvailableCb | null,
  downloaded: null as DownloadedCb | null,
  check: vi.fn(),
}));

// Keep the heavy workspace modules out of the test graph (core/paths pulls in
// i18next), mirroring tab-bar.test.tsx.
vi.mock("@multica/core/paths", () => ({
  paths: {
    workspace: (slug: string) => ({
      settings: () => `/${slug}/settings`,
    }),
  },
}));

vi.mock("@/stores/tab-store", () => {
  const store = { activeWorkspaceSlug: "acme" as string | null };
  const useTabStore = Object.assign(() => store, { getState: () => store });
  return { useTabStore };
});

import { UpdateButton } from "./update-button";

function installUpdaterMock() {
  Object.defineProperty(window, "updater", {
    configurable: true,
    value: {
      checkForUpdates: handlers.check,
      onUpdateAvailable: (cb: AvailableCb) => {
        handlers.available = cb;
        return () => {
          handlers.available = null;
        };
      },
      onUpdateDownloaded: (cb: DownloadedCb) => {
        handlers.downloaded = cb;
        return () => {
          handlers.downloaded = null;
        };
      },
      onDownloadProgress: () => () => {},
      downloadUpdate: vi.fn(),
      installUpdate: vi.fn(),
    },
  });
}

describe("UpdateButton", () => {
  beforeEach(() => {
    handlers.available = null;
    handlers.downloaded = null;
    handlers.check = vi.fn().mockResolvedValue({ ok: true, available: false });
    installUpdaterMock();
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("renders nothing while the app is up to date", async () => {
    render(<UpdateButton />);
    await waitFor(() => expect(handlers.check).toHaveBeenCalled());
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("shows the button when the on-mount check reports an update", async () => {
    handlers.check = vi
      .fn()
      .mockResolvedValue({ ok: true, available: true, latestVersion: "1.2.3" });
    installUpdaterMock();

    render(<UpdateButton />);

    expect(
      await screen.findByRole("button", { name: /update available: v1\.2\.3/i }),
    ).toBeInTheDocument();
  });

  it("shows the button when a background update-available event fires", async () => {
    render(<UpdateButton />);
    await waitFor(() => expect(handlers.check).toHaveBeenCalled());
    expect(screen.queryByRole("button")).toBeNull();

    act(() => handlers.available?.({ version: "2.0.0" }));

    expect(
      await screen.findByRole("button", { name: /update available: v2\.0\.0/i }),
    ).toBeInTheDocument();
  });

  it("offers a restart once the update has downloaded", async () => {
    render(<UpdateButton />);
    await waitFor(() => expect(handlers.check).toHaveBeenCalled());

    act(() => handlers.downloaded?.({ version: "3.1.0" }));

    expect(
      await screen.findByRole("button", { name: /restart to update to v3\.1\.0/i }),
    ).toBeInTheDocument();
  });
});
