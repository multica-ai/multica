// @vitest-environment node
import { expect, it } from "vitest";

import {
  analyticsPageTitle,
  resolveGoogleAnalyticsId,
  sanitizeAnalyticsUrl,
} from "./google-analytics";

it("enables only a valid GA4 measurement ID", () => {
  expect(resolveGoogleAnalyticsId({})).toBeUndefined();
  expect(resolveGoogleAnalyticsId({ GOOGLE_ANALYTICS_ID: " g-rqvf209ehd " })).toBe("G-RQVF209EHD");
  // Interpolated into an inline script, so anything else is rejected.
  expect(resolveGoogleAnalyticsId({ GOOGLE_ANALYTICS_ID: "G-X'); alert(1); //" })).toBeUndefined();
  expect(resolveGoogleAnalyticsId({ GOOGLE_ANALYTICS_ID: "UA-12345-1" })).toBeUndefined();
});

it("strips credentials from URLs but keeps campaign params", () => {
  expect(sanitizeAnalyticsUrl("https://multica.ai/wecom/bind?token=t")).toBe("https://multica.ai/wecom/bind");
  expect(sanitizeAnalyticsUrl("https://multica.ai/invite/inv_1")).toBe("https://multica.ai/invite/[id]");
  expect(sanitizeAnalyticsUrl("https://multica.ai/?utm_source=x&code=c")).toBe("https://multica.ai/?utm_source=x");
  expect(sanitizeAnalyticsUrl("javascript:alert(1)")).toBe("");
});

it("reports route labels as page_title, never workspace content", () => {
  const t = (path: string) => analyticsPageTitle(`https://multica.ai${path}`);
  expect(t("/")).toBe("Home");
  expect(t("/changelog")).toBe("Changelog");
  expect(t("/contact-sales")).toBe("Contact sales");
  expect(t("/invite/inv_1")).toBe("Invite");
  expect(t("/lark/bind")).toBe("Lark");
  expect(t("/acme")).toBe("Workspace");
  expect(t("/acme/issues")).toBe("Workspace · Issues");
  expect(t("/acme/issues/MUL-123")).toBe("Workspace · Issues · Detail");
  expect(t("/acme/my-issues")).toBe("Workspace · My issues");
  // Unknown segments are not echoed back.
  expect(t("/acme/secret-roadmap")).toBe("Workspace · Page");
  expect(analyticsPageTitle("not a url")).toBe("Multica");
});
