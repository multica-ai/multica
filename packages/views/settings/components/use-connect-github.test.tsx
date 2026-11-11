import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  connect: vi.fn(),
  open: vi.fn(),
  desktop: false,
  handler: (_payload: unknown) => {},
}));
vi.mock("@multica/core/api", () => ({ api: { getGitHubConnectURL: mocks.connect } }));
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (_event: string, handler: typeof mocks.handler) => { mocks.handler = handler; },
}));
vi.mock("../../platform", () => ({
  isDesktopShell: () => mocks.desktop,
  openExternal: mocks.open,
}));
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "Connection failed" }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

import { useConnectGitHub } from "./use-connect-github";

beforeEach(() => {
  vi.clearAllMocks();
  mocks.desktop = false;
  mocks.connect.mockResolvedValue({ configured: true, url: "https://github.com/apps/test/installations/new", connection_id: "desktop-nonce" });
});

it("uses the same browser tab from the shared connection action", async () => {
  const { result } = renderHook(() => useConnectGitHub("ws-1"));
  await act(() => result.current.connect());
  expect(mocks.connect).toHaveBeenCalledWith("ws-1", { desktop: false });
  expect(mocks.open).toHaveBeenCalledWith("https://github.com/apps/test/installations/new", { webTarget: "same-tab" });
  act(() => mocks.handler({ installation: { id: "install-1", workspace_id: "ws-1" }, connection_id: "desktop-nonce" }));
  expect(result.current.connectedInstallationID).toBe("");
});

it("completes only the desktop connection initiated in this workspace and window", async () => {
  mocks.desktop = true;
  const { result } = renderHook(() => useConnectGitHub("ws-1"));
  const event = { installation: { id: "install-1", workspace_id: "ws-1" }, connection_id: "desktop-nonce" };
  act(() => mocks.handler(event));
  expect(result.current.connectedInstallationID).toBe("");
  await act(() => result.current.connect());
  act(() => mocks.handler({ ...event, connection_id: "another-window" }));
  act(() => mocks.handler({ ...event, installation: { ...event.installation, workspace_id: "ws-2" } }));
  act(() => mocks.handler({ malformed: true }));
  expect(result.current.connectedInstallationID).toBe("");
  act(() => mocks.handler(event));
  await waitFor(() => expect(result.current.connectedInstallationID).toBe("install-1"));
  act(() => result.current.reset());
  act(() => mocks.handler(event));
  expect(result.current.connectedInstallationID).toBe("");
});

it("does not open a browser when GitHub is unconfigured", async () => {
  mocks.connect.mockResolvedValue({ configured: false });
  const { result } = renderHook(() => useConnectGitHub("ws-1"));
  await act(() => result.current.connect());
  expect(mocks.open).not.toHaveBeenCalled();
  expect(result.current.connecting).toBe(false);
});
