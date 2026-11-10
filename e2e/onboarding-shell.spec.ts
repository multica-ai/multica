import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";
import { waitForPageText } from "./helpers";

// The onboarding column is one measure and everything structural inside it
// runs that full width. This caught a real regression: the workspace form
// carried its own narrower cap, so the fields ended 64px short of the heading
// and description directly above them.
//
// It asserts on rendered geometry rather than class names because the bug was
// a computed-width mismatch — the classes involved all looked reasonable on
// their own.

test.use({ viewport: { width: 1440, height: 900 } });

async function expectFullWidthBlocks(
  page: import("@playwright/test").Page,
  label: string,
) {
  const result = await page.evaluate(() => {
    const column = document.querySelector<HTMLElement>("main > div");
    if (!column) return null;
    const columnRect = column.getBoundingClientRect();

    // Structural blocks: the heading, any field or field group, and the
    // action footer. Deliberately not every node — buttons, inputs and
    // `w-fit` labels are meant to size to their content.
    const selector = 'h1, [data-slot="field-group"], [data-slot="field"]';
    const offenders: Array<{ tag: string; w: number; text: string }> = [];
    for (const el of Array.from(document.querySelectorAll<HTMLElement>(selector))) {
      const r = el.getBoundingClientRect();
      if (r.width === 0) continue;
      if (Math.abs(r.width - columnRect.width) > 1) {
        offenders.push({
          tag: el.tagName.toLowerCase(),
          w: Math.round(r.width),
          text: (el.textContent ?? "").trim().slice(0, 40),
        });
      }
    }
    return { columnWidth: Math.round(columnRect.width), offenders };
  });

  expect(result, `${label}: no onboarding column found`).not.toBeNull();
  expect(
    result!.offenders,
    `${label}: blocks not matching the ${result!.columnWidth}px column`,
  ).toEqual([]);
}

test("onboarding — structural blocks match the column width on every step", async ({
  page,
}) => {
  const api = new TestApiClient();
  await api.login(`widths-${Date.now()}@localhost`, "Width Guard");

  await page.addInitScript((t) => localStorage.setItem("multica_token", t), api.getToken());
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "Your AI teammates, in one workspace.");
  await expectFullWidthBlocks(page, "welcome");

  await page.getByRole("button", { name: "Start" }).click();
  await page
    .getByRole("heading", { name: "Connect a computer" })
    .waitFor({ timeout: 20000 });
  await expectFullWidthBlocks(page, "connect");
});
