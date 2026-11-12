import { readFileSync } from "node:fs";
import { expect, test, type Response } from "@playwright/test";
import { SUPPORTED_LOCALES } from "../packages/core/i18n/types";

function stringsIn(value: unknown): string[] {
  if (typeof value === "string") return [value];
  if (!value || typeof value !== "object") return [];
  return Object.values(value).flatMap(stringsIn);
}

// Runtime setup copy is not rendered on /login. A distinctive phrase from
// each dictionary identifies its downloaded JS even when chunk names change.
const markers = SUPPORTED_LOCALES.map((locale) => {
  const namespace: unknown = JSON.parse(
    readFileSync(
      new URL(`../packages/views/locales/${locale}/runtimes.json`, import.meta.url),
      "utf8",
    ),
  );
  const marker = stringsIn(namespace)
    .filter((text) => text.length > 30 && !/["'\\\n\r]/.test(text))
    .sort((a, b) => b.length - a.length)[0];
  if (!marker) throw new Error(`No distinctive runtime copy for ${locale}`);
  return { locale, marker };
});

test.use({ locale: "ru-RU" });

test("Russian login keeps translations out of HTML and downloads only its language bundle", async ({
  page,
  context,
  baseURL,
}) => {
  if (!baseURL) throw new Error("Locale bundle regression requires a baseURL");
  await context.addCookies([
    { name: "multica-locale", value: "ru", url: baseURL },
  ]);
  // Exercise the real page and providers with a deterministic logged-out API.
  await context.route("**/api/config", (route) => route.fulfill({
    json: { allow_signup: true, google_client_id: "", feature_flags: {} },
  }));
  await context.route("**/api/me", (route) => route.fulfill({
    status: 401,
    json: { error: "Not authenticated" },
  }));
  await context.routeWebSocket((url) => url.pathname === "/ws", (socket) => socket.close());

  const scripts: Response[] = [];
  const errors: string[] = [];
  page.on("response", (response) => {
    if (
      response.url().includes("/_next/static/") &&
      response.request().resourceType() === "script"
    ) scripts.push(response);
  });
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error" && /hydration|hydrating|React.*#418/i.test(message.text())) {
      errors.push(message.text());
    }
  });

  const response = await page.goto("/login", { waitUntil: "load" });
  expect(response?.status()).toBe(200);
  const html = await response!.text();
  expect(html).toContain("Вход в Multica");
  expect(html).not.toContain('\\"resources\\":');
  expect(html).not.toContain('\\"runtimes\\":');
  await expect(page.locator("html")).toHaveAttribute("lang", "ru");
  await expect(page.locator('[data-slot="card-title"]')).toHaveText("Вход в Multica");
  await page.locator("#login-email").fill("locale-regression@example.com");
  await expect(page.locator('button[type="submit"][form="login-form"]')).toBeEnabled();

  const bodies = await Promise.all(scripts.map((script) => script.text()));
  const downloadedLocales = markers
    .filter(({ marker }) => bodies.some((body) => body.includes(marker)))
    .map(({ locale }) => locale);
  expect(downloadedLocales).toEqual(["ru"]);
  const russian = markers.find(({ locale }) => locale === "ru")!;
  expect(bodies.filter((body) => body.includes(russian.marker))).toHaveLength(1);
  expect(errors).toEqual([]);
});
