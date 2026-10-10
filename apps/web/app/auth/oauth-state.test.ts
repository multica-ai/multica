// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildGoogleOAuthState, parseGoogleOAuthState } from "./oauth-state";

describe("Google OAuth state", () => {
  it("round-trips a next path that contains a comma", () => {
    const nextUrl = "/acme/issues?status=todo,doing";
    const state = buildGoogleOAuthState({
      platform: null,
      nextUrl,
      cliCallback: null,
      cliState: "",
    });

    expect(state).toBeDefined();
    expect(state).not.toContain(",doing");
    expect(parseGoogleOAuthState(state!).nextRaw).toBe(nextUrl);
    expect(parseGoogleOAuthState(state!).cliCallbackRaw).toBeNull();
    expect(parseGoogleOAuthState(state!).isDesktop).toBe(false);
  });

  it("does not let a comma inside next become a cli_callback field", () => {
    const nextUrl =
      "/invite/abc,cli_callback:http://192.168.1.50:9999/callback";
    const state = buildGoogleOAuthState({
      platform: null,
      nextUrl,
      cliCallback: null,
      cliState: "",
    });

    const parsed = parseGoogleOAuthState(state!);
    expect(parsed.cliCallbackRaw).toBeNull();
    expect(parsed.nextRaw).toBe(nextUrl);
  });

  it("still reads an older unencoded next path", () => {
    expect(parseGoogleOAuthState("next:/invite/abc123").nextRaw).toBe(
      "/invite/abc123",
    );
  });

  it("keeps platform, next, and CLI fields distinct", () => {
    const state = buildGoogleOAuthState({
      platform: "desktop",
      nextUrl: "/invite/abc",
      cliCallback: "http://127.0.0.1:46233/callback",
      cliState: "abc 123",
    });

    expect(parseGoogleOAuthState(state!)).toEqual({
      isDesktop: true,
      nextRaw: "/invite/abc",
      cliCallbackRaw: "http://127.0.0.1:46233/callback",
      cliState: "abc 123",
    });
  });

  it("returns undefined when there is nothing to carry", () => {
    expect(
      buildGoogleOAuthState({
        platform: null,
        nextUrl: null,
        cliCallback: null,
        cliState: "",
      }),
    ).toBeUndefined();
  });
});
