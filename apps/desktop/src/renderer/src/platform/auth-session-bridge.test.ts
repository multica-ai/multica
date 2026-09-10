import { describe, expect, it } from "vitest";
import { authSessionReportValue } from "./auth-session-bridge";

describe("authSessionReportValue", () => {
  it("reports definitive authenticated and unauthenticated transitions", () => {
    expect(authSessionReportValue("authenticated", "user-1")).toBe("user-1");
    expect(authSessionReportValue("unauthenticated", null)).toBeNull();
  });

  it("does not report transient startup states", () => {
    expect(authSessionReportValue("authenticating", null)).toBeUndefined();
    expect(authSessionReportValue("recovering", null)).toBeUndefined();
  });

  it("fails safely when authenticated state has no user", () => {
    expect(authSessionReportValue("authenticated", null)).toBeUndefined();
  });
});
