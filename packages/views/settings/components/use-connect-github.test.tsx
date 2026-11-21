import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { useGitHubConnectionStore } from "@multica/core/github";

const mocks = vi.hoisted(() => ({
  connect: vi.fn(),
  open: vi.fn(),
  desktop: false,
}));
vi.mock("@multica/core/api", () => ({ api: { getGitHubConnectURL: mocks.connect } }));
vi.mock("../../platform", () => ({
  isDesktopShell: () => mocks.desktop,
  openExternal: mocks.open,
}));
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "Connection failed" }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

import { useConnectGitHub } from "./use-connect-github";

beforeEach(() => {
  vi.clearAllMocks();
  useGitHubConnectionStore.getState().reset();
  mocks.desktop = false;
  mocks.connect.mockResolvedValue({ configured: true, url: "https://github.com/apps/test/installations/new", connection_id: "desktop-nonce" });
});

it("uses the same browser tab from the shared connection action", async () => {
  const { result } = renderHook(() => useConnectGitHub("ws-1"));
  await act(() => result.current.connect());
  expect(mocks.connect).toHaveBeenCalledWith("ws-1", { desktop: false });
  expect(mocks.open).toHaveBeenCalledWith("https://github.com/apps/test/installations/new", { webTarget: "same-tab" });
  act(() => useGitHubConnectionStore.getState().complete({ installation: { id: "install-1", workspace_id: "ws-1" }, connection_id: "desktop-nonce" }));
  expect(result.current.connectedInstallationID).toBe("");
});

it("completes only the desktop connection initiated in this workspace and window", async () => {
  mocks.desktop = true;
  const { result } = renderHook(() => useConnectGitHub("ws-1"));
  const event = { installation: { id: "install-1", workspace_id: "ws-1" }, connection_id: "desktop-nonce" };
  act(() => useGitHubConnectionStore.getState().complete(event));
  expect(result.current.connectedInstallationID).toBe("");
  await act(() => result.current.connect());
  act(() => useGitHubConnectionStore.getState().complete({ ...event, connection_id: "another-window" }));
  act(() => useGitHubConnectionStore.getState().complete({ ...event, installation: { ...event.installation, workspace_id: "ws-2" } }));
  act(() => useGitHubConnectionStore.getState().complete({ malformed: true }));
  expect(result.current.connectedInstallationID).toBe("");
  act(() => useGitHubConnectionStore.getState().complete(event));
  await waitFor(() => expect(result.current.connectedInstallationID).toBe("install-1"));
  act(() => result.current.reset());
  act(() => useGitHubConnectionStore.getState().complete(event));
  expect(result.current.connectedInstallationID).toBe("");
});

it("does not open a browser when GitHub is unconfigured", async () => {
  mocks.connect.mockResolvedValue({ configured: false });
  const { result } = renderHook(() => useConnectGitHub("ws-1"));
  await act(() => result.current.connect());
  expect(mocks.open).not.toHaveBeenCalled();
  expect(result.current.connecting).toBe(false);
});

it("continues a desktop connection after leaving and returning to Code settings", async () => {
  mocks.desktop = true;
  const original = renderHook(() => useConnectGitHub("ws-1"));
  await act(() => original.result.current.connect());
  original.unmount();

  const returned = renderHook(() => useConnectGitHub("ws-1"));
  act(() => useGitHubConnectionStore.getState().complete({
    installation: { id: "install-1", workspace_id: "ws-1" },
    connection_id: "desktop-nonce",
  }));
  expect(returned.result.current.connectedInstallationID).toBe("install-1");
});

it("retains completion received while Code settings is unmounted and consumes it only once", async () => {
  mocks.desktop = true;
  const original = renderHook(() => useConnectGitHub("ws-1"));
  await act(() => original.result.current.connect());
  original.unmount();
  act(() => useGitHubConnectionStore.getState().complete({
    installation: { id: "install-1", workspace_id: "ws-1" },
    connection_id: "desktop-nonce",
  }));

  const otherWorkspace = renderHook(() => useConnectGitHub("ws-2"));
  expect(otherWorkspace.result.current.connectedInstallationID).toBe("");
  act(() => otherWorkspace.result.current.reset());
  otherWorkspace.unmount();
  const returned = renderHook(() => useConnectGitHub("ws-1"));
  expect(returned.result.current.connectedInstallationID).toBe("install-1");
  act(() => returned.result.current.reset());
  returned.unmount();
  const reopened = renderHook(() => useConnectGitHub("ws-1"));
  expect(reopened.result.current.connectedInstallationID).toBe("");
});
