import { render } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";

import { GoogleAnalytics } from "./google-analytics";

// Guards the privacy gate in lib/google-analytics.ts. A regression here would
// not show up as an error; credentials would silently reach Google.
const ID = "G-TEST1234";
const w = window as unknown as Record<string, unknown>;
const disabled = () => w[`ga-disable-${ID}`];
const scripts = () => document.querySelectorAll('script[src*="googletagmanager.com/gtag/js"]').length;
const { pushState, replaceState } = window.history;

afterEach(() => {
  Object.assign(window.history, { pushState, replaceState });
  for (const k of ["__multicaGoogleAnalytics", `ga-disable-${ID}`, "dataLayer", "gtag"]) delete w[k];
  document.querySelectorAll("script").forEach((s) => s.remove());
  window.history.replaceState(null, "", "/");
});

const start = (path: string) => {
  window.history.replaceState(null, "", path);
  render(<GoogleAnalytics measurementId={ID} />);
};

const click = (href: string) => {
  const a = document.createElement("a");
  a.href = href;
  a.addEventListener("click", (e) => e.preventDefault()); // jsdom can't navigate
  document.body.appendChild(a);
  a.click();
  a.remove();
};

it("loads only once on a clean URL, never on a sensitive landing", () => {
  start("/wecom/bind?token=secret");
  expect(scripts()).toBe(0);
  window.history.pushState(null, "", "/acme/issues");
  window.history.pushState(null, "", "/acme/projects");
  expect(scripts()).toBe(1);
  expect(disabled()).toBe(false);
});

it("stops for the document after visiting a sensitive URL", () => {
  start("/about");
  window.history.pushState(null, "", "/auth/callback?code=c");
  window.history.pushState(null, "", "/privacy");
  window.history.replaceState(null, "", "/privacy");
  expect(disabled()).toBe(true);
});

it("stops before a signed download click, but not for clean or blob links", () => {
  start("/about");
  click("https://github.com/multica-ai/multica");
  click("blob:https://app.example.invalid/uuid");
  expect(disabled()).toBe(false);
  click("https://cdn.example.invalid/a.pdf?Signature=s");
  expect(disabled()).toBe(true);
});
