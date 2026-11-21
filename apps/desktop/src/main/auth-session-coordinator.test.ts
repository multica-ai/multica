// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import {
  AuthSessionCoordinator,
  parseAuthSessionUserId,
} from "./auth-session-coordinator";

function createCoordinator(mainWindows: string[] = ["main"]) {
  const close = vi.fn();
  const coordinator = new AuthSessionCoordinator<string>(close);
  for (const window of mainWindows) coordinator.registerMainWindow(window);
  return { close, coordinator };
}

describe("AuthSessionCoordinator", () => {
  it("keeps an issue window whose account matches the main window", () => {
    const { close, coordinator } = createCoordinator();
    coordinator.reportMain("main", "user-a");
    coordinator.registerIssueWindow("issue-1");
    coordinator.reportIssue("issue-1", "user-a");

    expect(close).not.toHaveBeenCalled();
    expect(coordinator.hasActiveMainSession("main")).toBe(true);
    expect(coordinator.isCurrentIssueSession("issue-1")).toBe(true);
  });

  it("closes every issue window on main logout or account switch", () => {
    const { close, coordinator } = createCoordinator();
    coordinator.reportMain("main", "user-a");
    coordinator.registerIssueWindow("issue-1");
    coordinator.registerIssueWindow("issue-2");

    coordinator.reportMain("main", "user-b");

    expect(close).toHaveBeenCalledTimes(2);
    expect(close).toHaveBeenCalledWith("issue-1");
    expect(close).toHaveBeenCalledWith("issue-2");
  });

  it("invalidates account-scoped work once per session transition", () => {
    const { coordinator } = createCoordinator();

    expect(coordinator.reportMain("main", "user-a")).toBe(false);
    expect(coordinator.reportMain("main", "user-a")).toBe(false);
    expect(coordinator.reportMain("main", null)).toBe(true);
    expect(coordinator.reportMain("main", null)).toBe(false);
    expect(coordinator.reportMain("main", "user-b")).toBe(true);
  });

  it("keeps main windows that share the account", () => {
    const { close, coordinator } = createCoordinator(["work", "personal"]);

    coordinator.reportMain("work", "user-a");
    coordinator.reportMain("personal", "user-a");

    expect(close).not.toHaveBeenCalled();
    expect(coordinator.hasActiveMainSession("work")).toBe(true);
    expect(coordinator.hasActiveMainSession("personal")).toBe(true);
  });

  it("closes every other window when any main window logs out", () => {
    const { close, coordinator } = createCoordinator(["work", "personal"]);
    coordinator.reportMain("work", "user-a");
    coordinator.reportMain("personal", "user-a");
    coordinator.registerIssueWindow("issue-1");

    expect(coordinator.reportMain("personal", null)).toBe(true);

    expect(close).toHaveBeenCalledTimes(2);
    expect(close).toHaveBeenCalledWith("work");
    expect(close).toHaveBeenCalledWith("issue-1");
    expect(coordinator.hasActiveMainSession("work")).toBe(false);
  });

  it("closes a main window that has not reported yet when another logs out", () => {
    const { close, coordinator } = createCoordinator(["work", "restoring"]);

    coordinator.reportMain("work", null);

    expect(close).toHaveBeenCalledWith("restoring");
  });

  it("ignores reports from windows it does not track as main windows", () => {
    const { close, coordinator } = createCoordinator();
    coordinator.reportMain("main", "user-a");

    expect(coordinator.reportMain("unknown", null)).toBe(false);
    expect(close).not.toHaveBeenCalled();
    expect(coordinator.hasActiveMainSession("main")).toBe(true);
  });

  it("closes an issue renderer that reports a stale or missing session", () => {
    const { close, coordinator } = createCoordinator();
    coordinator.reportMain("main", "user-a");
    coordinator.registerIssueWindow("stale");
    coordinator.registerIssueWindow("logged-out");

    coordinator.reportIssue("stale", "user-b");
    coordinator.reportIssue("logged-out", null);

    expect(close).toHaveBeenCalledTimes(2);
    expect(coordinator.isCurrentIssueSession("stale")).toBe(false);
  });
});

describe("parseAuthSessionUserId", () => {
  it("accepts a bounded user id or explicit logged-out state", () => {
    expect(parseAuthSessionUserId(" user-a ")).toBe("user-a");
    expect(parseAuthSessionUserId(null)).toBeNull();
    expect(parseAuthSessionUserId(" ")).toBeUndefined();
    expect(parseAuthSessionUserId(42)).toBeUndefined();
    expect(parseAuthSessionUserId("x".repeat(257))).toBeUndefined();
  });
});
