import { describe, expect, it, vi } from "vitest";
import {
  AuthSessionCoordinator,
  parseAuthSessionUserId,
} from "./auth-session-coordinator";

describe("AuthSessionCoordinator", () => {
  it("keeps an issue window whose account matches the main window", () => {
    const close = vi.fn();
    const coordinator = new AuthSessionCoordinator<string>(close);
    coordinator.reportMain("user-a");
    coordinator.registerIssueWindow("issue-1");
    coordinator.reportIssue("issue-1", "user-a");

    expect(close).not.toHaveBeenCalled();
    expect(coordinator.hasActiveMainSession()).toBe(true);
    expect(coordinator.isCurrentIssueSession("issue-1")).toBe(true);
  });

  it("closes every issue window on main logout or account switch", () => {
    const close = vi.fn();
    const coordinator = new AuthSessionCoordinator<string>(close);
    coordinator.reportMain("user-a");
    coordinator.registerIssueWindow("issue-1");
    coordinator.registerIssueWindow("issue-2");

    coordinator.reportMain("user-b");

    expect(close).toHaveBeenCalledTimes(2);
    expect(close).toHaveBeenCalledWith("issue-1");
    expect(close).toHaveBeenCalledWith("issue-2");
  });

  it("invalidates account-scoped work once per session transition", () => {
    const coordinator = new AuthSessionCoordinator<string>(() => {});

    expect(coordinator.reportMain("user-a")).toBe(false);
    expect(coordinator.reportMain("user-a")).toBe(false);
    expect(coordinator.reportMain(null)).toBe(true);
    expect(coordinator.reportMain(null)).toBe(false);
    expect(coordinator.reportMain("user-b")).toBe(true);
  });

  it("closes an issue renderer that reports a stale or missing session", () => {
    const close = vi.fn();
    const coordinator = new AuthSessionCoordinator<string>(close);
    coordinator.reportMain("user-a");
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
