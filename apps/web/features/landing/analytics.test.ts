import { beforeEach, describe, expect, it, vi } from "vitest";

const { captureEvent } = vi.hoisted(() => ({ captureEvent: vi.fn() }));
vi.mock("@multica/core/analytics", () => ({ captureEvent }));

import {
  QUALIFIED_LANDING_VIEW,
  SIGNUP_OR_DOWNLOAD_START,
  acquisitionDimensions,
  captureQualifiedLandingView,
  captureSignupOrDownloadStart,
  claimQualifiedLandingView,
  isQualifiedLandingContext,
} from "./analytics";

beforeEach(() => {
  captureEvent.mockClear();
});

describe("landing funnel analytics", () => {
  it("qualifies only visible human browser contexts", () => {
    expect(
      isQualifiedLandingContext({
        visibilityState: "visible",
        webdriver: false,
        userAgent: "Mozilla/5.0",
      }),
    ).toBe(true);
    expect(
      isQualifiedLandingContext({
        visibilityState: "hidden",
        webdriver: false,
        userAgent: "Mozilla/5.0",
      }),
    ).toBe(false);
    expect(
      isQualifiedLandingContext({
        visibilityState: "visible",
        webdriver: true,
        userAgent: "Mozilla/5.0",
      }),
    ).toBe(false);
    expect(
      isQualifiedLandingContext({
        visibilityState: "visible",
        webdriver: false,
        userAgent: "ExampleBot/1.0",
      }),
    ).toBe(false);
  });

  it("claims a qualified view once per tab session", () => {
    const values = new Map<string, string>();
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => void values.set(key, value),
    };

    expect(claimQualifiedLandingView(storage)).toBe(true);
    expect(claimQualifiedLandingView(storage)).toBe(false);
  });

  it("keeps campaign fields and only the referrer host", () => {
    expect(
      acquisitionDimensions(
        "?utm_source=github&utm_medium=community&utm_campaign=launch-2026",
        "https://news.ycombinator.com/item?id=secret",
        "https://multica.ai",
      ),
    ).toEqual({
      source: "github",
      medium: "community",
      campaign: "launch-2026",
      referrer_host: "news.ycombinator.com",
    });
  });

  it("uses explicit direct/none buckets when attribution is absent", () => {
    expect(acquisitionDimensions("", "", "https://multica.ai")).toEqual({
      source: "direct",
      medium: "none",
      campaign: "none",
    });
  });

  it("rejects URL- and email-shaped campaign values", () => {
    expect(
      acquisitionDimensions(
        "?utm_source=https%3A%2F%2Fexample.com&utm_campaign=person%40example.com",
        "",
        "https://multica.ai",
      ),
    ).toEqual({ source: "direct", medium: "none", campaign: "none" });
  });

  it("does not capture an IP referrer host", () => {
    expect(
      acquisitionDimensions("", "https://192.0.2.10/private", "https://multica.ai"),
    ).toEqual({ source: "direct", medium: "none", campaign: "none" });
  });

  it("emits the two acquisition events with bounded dimensions", () => {
    vi.stubGlobal("window", {
      location: {
        search: "?utm_source=github&utm_campaign=launch",
        origin: "https://multica.ai",
      },
    });
    vi.stubGlobal("document", { referrer: "https://example.com/private/path" });

    captureQualifiedLandingView();
    captureSignupOrDownloadStart("download", "hero");

    expect(captureEvent).toHaveBeenNthCalledWith(
      1,
      QUALIFIED_LANDING_VIEW,
      expect.objectContaining({
        source: "github",
        campaign: "launch",
        referrer_host: "example.com",
        platform: "web",
        landing_path: "/",
      }),
    );
    expect(captureEvent).toHaveBeenNthCalledWith(
      2,
      SIGNUP_OR_DOWNLOAD_START,
      expect.objectContaining({ intent: "download", placement: "hero" }),
    );

    vi.unstubAllGlobals();
  });
});
