// @vitest-environment node
import { expect, it } from "vitest";

import { resolveGoogleAnalyticsId, sanitizeAnalyticsUrl } from "./google-analytics";

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
