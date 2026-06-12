import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

/**
 * Regression guard for the recurring "long URLs / wide email content push the
 * message column wider than the phone, so text gets clipped off the right edge"
 * bug (JEH — inbox/issue message thread on mobile).
 *
 * The invariant we lock in: on a phone-width screen, NOTHING inside a rendered
 * message body (`.rich-text-editor`) may stick out past the viewport UNLESS it
 * lives in a horizontally scrollable box (a wide data table is allowed to
 * scroll inside its own `.tableWrapper`; everything else must wrap/clip-free).
 *
 * Runs on BOTH Chromium and WebKit. WebKit matters: the iPhone uses the Safari
 * engine, and earlier fixes that "looked fine" in Chrome stayed broken there.
 */

// iPhone 13/14 logical viewport.
test.use({ viewport: { width: 390, height: 844 } });

// Email-style content is the worst case: a fixed-width layout table, an
// unbreakable token, a long bare URL, and a fixed-width div — all of which a
// real email thread can carry.
const HARD_DESCRIPTION = [
  "Du skal bare søge i drive. Der hvor jeg fandt det hele. Alt var her",
  "https://drive.google.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456789QwErTyUiOpAsDfGhJkLzXcVbNmY2gXlcqTfyIHeY07I8fHI?dmr=1&ec=wgc-drive-globalnav-goto&authuser=0",
  "og en uafbrudt reference REF1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ0987654321ZZZZZZZZZZZZZZZZ.",
  "",
  '<table style="width:680px;border-collapse:collapse"><tr><td style="width:680px">Opgraderingsaftalen henviser til bilag, som beskriver licensbetingelserne i detaljer.</td></tr></table>',
  "",
  '<div style="width:680px">Fast bredde fra en email-klient: dette skal kunne læses på en telefon uden at blive klippet væk i højre side.</div>',
].join("\n");

test.describe("Message body never overflows the phone width", () => {
  let api: TestApiClient;
  let issueId: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    const issue = await api.createIssue("E2E message overflow " + Date.now(), {
      description: HARD_DESCRIPTION,
    });
    issueId = issue.id;
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("long URLs and wide email content wrap to fit the phone (never cut off the right edge)", async ({ page }) => {
    const issueLink = page.locator(`a[href$="/issues/${issueId}"]`);
    await expect(issueLink).toBeVisible({ timeout: 10000 });
    await issueLink.click();
    await page.waitForURL(/\/issues\/[\w-]+/);

    // The rendered description body.
    const body = page.locator(".rich-text-editor").first();
    await expect(body).toBeVisible({ timeout: 10000 });

    // The page itself must never scroll horizontally on a phone.
    const pageOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(pageOverflow, "page scrolls horizontally on mobile").toBeLessThanOrEqual(1);

    // On a phone, message-body content must FIT the screen — wrap, not require a
    // sideways scroll. So nothing inside `.rich-text-editor` may stick past the
    // viewport. The only allowed exception is a `<pre>` code block, where keeping
    // code on one line and scrolling it is the intended behaviour. (A horizontal
    // scroll box does NOT excuse overflow here: a wide email layout table sits
    // inside a scrollable `.tableWrapper`, yet the user reads it as "text cut off"
    // — which is exactly the bug. The fix makes it wrap to fit instead.)
    const overflowing = await page.evaluate(() => {
      const vw = document.documentElement.clientWidth;
      const out: { tag: string; right: number; text: string }[] = [];
      document.querySelectorAll(".rich-text-editor *").forEach((node) => {
        const el = node as HTMLElement;
        const cs = getComputedStyle(el);
        if (cs.position === "absolute" || cs.position === "fixed") return;
        if (el.closest("pre")) return; // code blocks may scroll
        const rect = el.getBoundingClientRect();
        if (rect.width === 0 || rect.right <= vw + 1) return;
        out.push({ tag: el.tagName, right: Math.round(rect.right), text: (el.textContent || "").slice(0, 40) });
      });
      return out;
    });

    expect(
      overflowing,
      `message content sticks past the phone's right edge (must wrap to fit): ${JSON.stringify(overflowing)}`,
    ).toEqual([]);
  });
});
