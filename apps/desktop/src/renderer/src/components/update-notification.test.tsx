import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { UpdateNotification } from "./update-notification";
import type { UpdateInstallState } from "../../../shared/updater-types";

const mocks = vi.hoisted(() => ({
  installUpdate: vi.fn(),
  openExternal: vi.fn(),
  getInstallState: vi.fn(),
}));

type UpdateDownloadedListener = (info: {
  version: string;
  releaseNotes?: string;
}) => void;

describe("UpdateNotification", () => {
  let updateDownloaded: UpdateDownloadedListener;
  let installStateChanged: (state: UpdateInstallState) => void;

  beforeEach(() => {
    mocks.installUpdate.mockReset().mockResolvedValue({ status: "ready", version: "0.4.27" });
    mocks.getInstallState.mockReset().mockResolvedValue({ status: "idle" });
    mocks.openExternal.mockReset().mockResolvedValue(undefined);

    Object.defineProperty(window, "desktopAPI", {
      configurable: true,
      value: { openExternal: mocks.openExternal },
    });
    Object.defineProperty(window, "updater", {
      configurable: true,
      value: {
        installRequiresStoppedRuntime: false,
        onInstallStateChanged: (listener: (state: UpdateInstallState) => void) => {
          installStateChanged = listener;
          updateDownloaded = (info) => listener({ status: "ready", version: info.version });
          return vi.fn();
        },
        installUpdate: mocks.installUpdate,
        getInstallState: mocks.getInstallState,
      },
    });
  });

  it("opens the downloaded version's changelog from the update prompt", () => {
    render(<UpdateNotification />);
    act(() => updateDownloaded({ version: "0.4.27" }));

    expect(screen.queryByRole("button", { name: "Later" })).not.toBeInTheDocument();
    expect(screen.getByText("v0.4.27 will be applied on next launch.")).toBeInTheDocument();
    expect(screen.queryByText(/Installation waits/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "See changelog" }));

    expect(mocks.openExternal).toHaveBeenCalledWith(
      "https://multica.ai/changelog#release-0-4-27",
    );
  });

  it("requests installation from the primary action", async () => {
    render(<UpdateNotification />);
    act(() => updateDownloaded({ version: "0.4.27" }));

    fireEvent.click(screen.getByRole("button", { name: "Restart now" }));

    expect(mocks.installUpdate).toHaveBeenCalledOnce();
    await waitFor(() => expect(screen.getByRole("button", { name: "Restart now" })).not.toBeDisabled());
  });

  it("rehydrates a deferred download on remount without needing another event", async () => {
    mocks.getInstallState.mockResolvedValue({ status: "deferred", version: "0.4.37", allowed: false, reason: "runtime_running" });
    const view = render(<UpdateNotification />);
    await screen.findByRole("button", { name: "Retry installation" });
    expect(screen.getByRole("status")).toHaveTextContent("Finish active runs");
    expect(screen.getByRole("status")).toHaveTextContent("Runtimes");
    fireEvent.click(screen.getByRole("button", { name: "Dismiss update notification" }));
    expect(screen.queryByRole("button", { name: "Retry installation" })).not.toBeInTheDocument();
    view.unmount();
    render(<UpdateNotification />);
    await screen.findByRole("button", { name: "Retry installation" });
  });

  it("prevents duplicate clicks while checking and surfaces a bounded retry reason", async () => {
    let finish!: (state: UpdateInstallState) => void;
    mocks.installUpdate.mockImplementation(() => new Promise<UpdateInstallState>((resolve) => { finish = resolve; }));
    render(<UpdateNotification />);
    act(() => updateDownloaded({ version: "0.4.37" }));
    fireEvent.click(screen.getByRole("button", { name: "Restart now" }));
    const pending = screen.getByRole("button", { name: "Checking runtime…" });
    expect(pending).toBeDisabled();
    fireEvent.click(pending);
    expect(mocks.installUpdate).toHaveBeenCalledOnce();
    await act(async () => finish({ status: "deferred", version: "0.4.37", allowed: false, reason: "probe_failed", diagnostic: "timed_out" }));
    expect(screen.getByRole("status")).toHaveTextContent("Runtime status could not be checked");
    expect(screen.getByRole("button", { name: "Retry installation" })).not.toBeDisabled();
  });

  it("offers manual recovery when the runtime probe cannot run without starting an installer", async () => {
    mocks.getInstallState.mockResolvedValue({
      status: "deferred", version: "0.4.37", allowed: false,
      reason: "probe_failed", diagnostic: "launch_failed",
    });
    render(<UpdateNotification />);
    const download = await screen.findByRole("button", { name: "Download installer" });
    expect(screen.getByRole("status")).toHaveTextContent("Finish active runs");
    expect(screen.getByRole("status")).toHaveTextContent("Stop");
    expect(screen.getByRole("status")).toHaveTextContent("quit Multica");
    fireEvent.click(download);
    expect(mocks.openExternal).toHaveBeenCalledWith("https://multica.ai/download");
    expect(mocks.installUpdate).not.toHaveBeenCalled();
    act(() => installStateChanged({ status: "ready", version: "0.4.38" }));
    expect(screen.queryByRole("button", { name: "Download installer" })).not.toBeInTheDocument();
  });

  it("keeps a newer event when the initial state snapshot resolves late", async () => {
    let finish!: (state: UpdateInstallState) => void;
    mocks.getInstallState.mockImplementation(() => new Promise<UpdateInstallState>((resolve) => { finish = resolve; }));
    render(<UpdateNotification />);
    act(() => installStateChanged({ status: "ready", version: "0.4.38" }));
    await act(async () => finish({ status: "idle" }));
    expect(screen.getByText("v0.4.38 will be applied on next launch.")).toBeInTheDocument();
  });

  it("shows IPC failure and allows retry instead of a silent disabled action", async () => {
    mocks.installUpdate.mockRejectedValue(new Error("disconnected"));
    render(<UpdateNotification />);
    act(() => updateDownloaded({ version: "0.4.37" }));
    fireEvent.click(screen.getByRole("button", { name: "Restart now" }));
    await screen.findByRole("alert");
    expect(screen.getByRole("button", { name: "Restart now" })).not.toBeDisabled();
  });

  it("explains runtime deferral without changing the official changelog link", () => {
    Object.defineProperty(window.updater, "installRequiresStoppedRuntime", { value: true });
    render(<UpdateNotification />);
    act(() => updateDownloaded({ version: "0.4.37" }));
    expect(screen.getByText(/Installation waits until the bundled runtime is stopped/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "See changelog" }));
    expect(mocks.openExternal).toHaveBeenCalledWith(
      "https://multica.ai/changelog#release-0-4-37",
    );
  });
});
