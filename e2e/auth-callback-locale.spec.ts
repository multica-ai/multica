import { expect, test } from "@playwright/test";

test.use({ locale: "zh-CN" });

for (const scenario of [
  {
    name: "different account language",
    language: "ja",
    htmlLang: "ja-JP",
    continueLabel: "ウェブで続ける",
  },
  {
    name: "same account language",
    language: "zh-Hans",
    htmlLang: "zh-CN",
    continueLabel: "在 web 端继续",
  },
]) {
  test(`Google callback keeps its single-use code while synchronizing ${scenario.name}`, async ({
    page,
    context,
    baseURL,
  }) => {
    if (!baseURL) {
      throw new Error("The callback regression requires a configured baseURL");
    }
    const origin = new URL(baseURL).origin;
    const code = "locale-regression-code";
    const user = {
      id: "66666666-6666-4666-8666-666666666666",
      name: "Callback locale test",
      email: "callback-locale@example.com",
      avatar_url: null,
      language: scenario.language,
      onboarded_at: null,
      onboarding_questionnaire: {},
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    let authenticated = false;
    let workspaceRequests = 0;
    let callbackDocuments = 0;
    const exchanges: unknown[] = [];
    const unexpectedApiRequests: string[] = [];
    const googleRequests: string[] = [];
    const pageErrors: string[] = [];
    let completeInitialAuthCheck!: () => void;
    const initialAuthCheck = new Promise<void>((resolve) => {
      completeInitialAuthCheck = resolve;
    });
    let releaseWorkspaces!: () => void;
    const workspaceGate = new Promise<void>((resolve) => {
      releaseWorkspaces = resolve;
    });

    await context.addCookies([
      { name: "multica-locale", value: "zh-Hans", url: origin },
    ]);
    // Isolate application WebSockets without intercepting Next.js HMR.
    await context.routeWebSocket(
      (url) => url.pathname === "/ws",
      (socket) => socket.close(),
    );
    page.on("pageerror", (error) => pageErrors.push(error.message));
    page.on("request", (request) => {
      if (
        request.isNavigationRequest() &&
        request.frame() === page.mainFrame() &&
        new URL(request.url()).pathname === "/auth/callback"
      ) {
        callbackDocuments += 1;
      }
    });

    // Keep the real Next.js page, providers, auth store, and API client. Only
    // the HTTP boundary is controlled; no Google account or backend rows are used.
    await context.route("**/*", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (url.origin !== origin) {
        if (
          /(^|\.)(google\.com|googleapis\.com|googleusercontent\.com)$/.test(url.hostname)
        ) {
          googleRequests.push(request.url());
        }
        return route.abort();
      }
      const json = (status: number, body: unknown) => route.fulfill({ status, json: body });
      if (url.pathname === "/auth/google" && request.method() === "POST") {
        exchanges.push(request.postDataJSON());
        // A replay must fail, otherwise an accidental callback reload can look successful.
        if (exchanges.length > 1) {
          return json(400, {
            code: "oauth_code_invalid",
            error: "The test code has already been used",
          });
        }
        await initialAuthCheck;
        authenticated = true;
        return json(200, { token: "synthetic-callback-token", user });
      }
      if (url.pathname === "/api/config") {
        return json(200, {
          allow_signup: true,
          google_client_id: "callback-test-only",
          feature_flags: {},
        });
      }
      if (url.pathname === "/api/me") {
        if (authenticated) return json(200, user);
        await json(401, { error: "Not authenticated" });
        completeInitialAuthCheck();
        return;
      }
      if (url.pathname === "/api/workspaces") {
        workspaceRequests += 1;
        await workspaceGate;
        return json(200, []);
      }
      if (url.pathname === "/api/invitations") return json(200, []);
      if (url.pathname === "/api/client-usage") return route.fulfill({ status: 204 });
      if (url.pathname.startsWith("/api/") || url.pathname === "/auth/google") {
        unexpectedApiRequests.push(`${request.method()} ${url.pathname}`);
        return json(500, { error: "Unexpected API request in callback locale test" });
      }
      return route.continue();
    });

    try {
      await page.goto(`/auth/callback?code=${code}`, { waitUntil: "domcontentloaded" });
      await expect.poll(() => workspaceRequests).toBeGreaterThan(0);
      // Let the login render and its passive effects finish while navigation is
      // blocked by the workspace response, instead of racing a fixed network delay.
      await page.evaluate(async () => {
        await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
        await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
      });
      await expect(page).toHaveURL(`${origin}/auth/callback?code=${code}`);
      expect(exchanges).toEqual([{ code, redirect_uri: `${origin}/auth/callback` }]);
      expect(callbackDocuments).toBe(1);
      expect(
        (await context.cookies(origin)).find((cookie) => cookie.name === "multica-locale")?.value,
      ).toBe("zh-Hans");
      releaseWorkspaces();

      await expect(page).toHaveURL(`${origin}/onboarding`, { timeout: 15000 });
      await expect(page.locator("html")).toHaveAttribute("lang", scenario.htmlLang);
      await expect(
        page.getByRole("button", { name: scenario.continueLabel, exact: true }),
      ).toBeVisible();
      expect(
        (await context.cookies(origin)).find((cookie) => cookie.name === "multica-locale")?.value,
      ).toBe(scenario.language);
      expect(await page.evaluate(() => localStorage.getItem("multica_token"))).toBeNull();
      expect(exchanges).toHaveLength(1);
      expect(callbackDocuments).toBe(1);
      expect(unexpectedApiRequests).toEqual([]);
      expect(googleRequests).toEqual([]);
      expect(pageErrors).toEqual([]);
    } finally {
      releaseWorkspaces();
      await context.unrouteAll({ behavior: "wait" });
    }
  });
}
