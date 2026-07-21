import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGetState, logout } = vi.hoisted(() => ({
  mockGetState: vi.fn(),
  logout: vi.fn(),
}));

const { toastError } = vi.hoisted(() => ({ toastError: vi.fn() }));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: { getState: mockGetState },
}));

vi.mock("sonner", () => ({
  toast: { error: toastError },
}));

import { reauthenticateDaemon } from "./daemon-reauth";

const daemonAPI = {
  reauthenticate: vi.fn(),
};

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  (window as unknown as { daemonAPI: typeof daemonAPI }).daemonAPI = daemonAPI;
  mockGetState.mockReturnValue({ user: { id: "user-1" }, logout });
});

describe("reauthenticateDaemon", () => {
  it("re-mints + restarts the daemon when signed in, without logging out", async () => {
    localStorage.setItem("multica_token", "jwt-abc");
    daemonAPI.reauthenticate.mockResolvedValue({ ok: true });

    await reauthenticateDaemon();

    expect(daemonAPI.reauthenticate).toHaveBeenCalledWith("jwt-abc", "user-1");
    expect(logout).not.toHaveBeenCalled();
    expect(toastError).not.toHaveBeenCalled();
  });

  it("logs out only when the session token itself is rejected (401)", async () => {
    localStorage.setItem("multica_token", "jwt-abc");
    daemonAPI.reauthenticate.mockResolvedValue({
      ok: false,
      reason: "session_invalid",
    });

    await reauthenticateDaemon();

    expect(logout).toHaveBeenCalledOnce();
    expect(toastError).not.toHaveBeenCalled();
  });

  // The reviewer's must-fix: a non-401 (transient) failure must NOT log the
  // user out — they stay signed in and can retry.
  it("does NOT log out on a transient failure; shows a retryable toast", async () => {
    localStorage.setItem("multica_token", "jwt-abc");
    daemonAPI.reauthenticate.mockResolvedValue({
      ok: false,
      reason: "transient",
      message: "mint PAT failed: 503 Service Unavailable",
    });

    await reauthenticateDaemon();

    expect(logout).not.toHaveBeenCalled();
    expect(toastError).toHaveBeenCalledOnce();
  });

  it("does NOT log out when the IPC call itself throws unexpectedly", async () => {
    localStorage.setItem("multica_token", "jwt-abc");
    daemonAPI.reauthenticate.mockRejectedValue(new Error("ipc boom"));

    await reauthenticateDaemon();

    expect(logout).not.toHaveBeenCalled();
    expect(toastError).toHaveBeenCalledOnce();
  });

  it("routes to login when there is no session token", async () => {
    await reauthenticateDaemon();

    expect(logout).toHaveBeenCalledOnce();
    expect(daemonAPI.reauthenticate).not.toHaveBeenCalled();
  });

  it("routes to login when there is no signed-in user", async () => {
    localStorage.setItem("multica_token", "jwt-abc");
    mockGetState.mockReturnValue({ user: null, logout });

    await reauthenticateDaemon();

    expect(logout).toHaveBeenCalledOnce();
    expect(daemonAPI.reauthenticate).not.toHaveBeenCalled();
  });
});
