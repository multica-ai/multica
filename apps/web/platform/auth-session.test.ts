import { beforeEach, describe, expect, it } from "vitest";
import {
  clearStoredSession,
  getUnauthorizedRedirectPath,
} from "./auth-session";

describe("auth-session", () => {
  beforeEach(() => {
    localStorage.clear();
    document.cookie = "multica_logged_in=; path=/; max-age=0";
  });

  it("clears stored auth state", () => {
    localStorage.setItem("multica_token", "token");
    localStorage.setItem("multica_workspace_id", "workspace");
    document.cookie = "multica_logged_in=1; path=/";

    clearStoredSession();

    expect(localStorage.getItem("multica_token")).toBeNull();
    expect(localStorage.getItem("multica_workspace_id")).toBeNull();
    expect(document.cookie).toBe("");
  });

  it("keeps login routes in place on unauthorized responses", () => {
    expect(getUnauthorizedRedirectPath("/")).toBeNull();
    expect(getUnauthorizedRedirectPath("/login")).toBeNull();
    expect(getUnauthorizedRedirectPath("/auth/callback")).toBeNull();
  });

  it("redirects protected routes back to the landing page", () => {
    expect(getUnauthorizedRedirectPath("/issues")).toBe("/");
    expect(getUnauthorizedRedirectPath("/settings")).toBe("/");
  });
});
