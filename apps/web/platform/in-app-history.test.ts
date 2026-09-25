import { afterEach, describe, expect, it } from "vitest";
import { canGoBackInApp } from "./in-app-history";

const win = window as unknown as { navigation?: unknown };

function withNavigationApi(navigation: unknown) {
  win.navigation = navigation;
}

afterEach(() => {
  delete win.navigation;
});

describe("canGoBackInApp", () => {
  it("reports the Navigation API's answer when the browser has one", () => {
    withNavigationApi({ canGoBack: true });

    expect(canGoBackInApp()).toBe(true);
  });

  // Not "nothing was pushed yet" — arriving from an external site leaves the
  // same-origin run empty, which is the whole reason we ask the browser.
  it("reports false when the Navigation API says there is no same-origin entry", () => {
    withNavigationApi({ canGoBack: false });

    expect(canGoBackInApp()).toBe(false);
  });

  // jsdom ships no `window.navigation`. Callers then take their fallback path,
  // which is what these browsers did before any of this existed — deliberately
  // no guessing, because a wrong `true` walks the user out of the app.
  it("reports false when the browser has no Navigation API", () => {
    expect(canGoBackInApp()).toBe(false);
  });

  it("reports false for a `navigation` global that is not the Navigation API", () => {
    withNavigationApi({ somethingElse: true });

    expect(canGoBackInApp()).toBe(false);
  });

  it("reports false for a non-boolean canGoBack", () => {
    withNavigationApi({ canGoBack: "yes" });

    expect(canGoBackInApp()).toBe(false);
  });
});
