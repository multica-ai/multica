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
  const token = api.getToken();

  await page.addInitScript((t) => localStorage.setItem("multica_token", t), token);
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "Continue on web");
  await page.getByRole("button", { name: "Continue on web" }).click();

  await page.getByText("Tell us a bit about you.").waitFor();
  await expectFullWidthBlocks(page, "about you");

  await page.getByRole("radio", { name: /Engineer \/ developer/i }).click();
  await page.getByRole("checkbox", { name: /Ship code with AI agents/i }).click();
  await page.getByRole("button", { name: "Continue" }).click();

  await page.getByRole("heading", { name: /Name your workspace/i }).waitFor();
  await expectFullWidthBlocks(page, "workspace");

  await page.getByRole("textbox").first().fill(`Width Guard ${Date.now()}`);
  await page.getByRole("button", { name: /^Create /i }).click();

  await page
    .getByRole("heading", { name: /Connect a computer/i })
    .waitFor({ timeout: 20000 });
  await expectFullWidthBlocks(page, "runtime");
});

// The rail is meant to persist across steps. It did not: every step rendered
// its own <StepShell>, and since each step is a different component type React
// tore the shell down and rebuilt it on each transition — remounting the rail,
// restarting its canvas, and replaying the shell's opacity-0 fade. That
// full-window re-fade was the visible "flash" on every step change.
test("onboarding — the shell survives step changes instead of re-mounting", async ({
  page,
}) => {
  const api = new TestApiClient();
  await api.login(`shell-${Date.now()}@localhost`, "Shell Guard");

  await page.addInitScript(
    (t) => localStorage.setItem("multica_token", t),
    api.getToken(),
  );
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "Continue on web");
  await page.getByRole("button", { name: "Continue on web" }).click();
  await page.getByText("Tell us a bit about you.").waitFor();

  // Tag the live nodes. A remount replaces the elements and drops the marks.
  await page.evaluate(() => {
    document.querySelector("aside")?.setAttribute("data-persist-probe", "1");
    document.querySelector("main")?.setAttribute("data-persist-probe", "1");
  });

  await page.getByRole("radio", { name: /Engineer \/ developer/i }).click();
  await page.getByRole("checkbox", { name: /Ship code with AI agents/i }).click();
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByRole("heading", { name: /Name your workspace/i }).waitFor();

  await expect(
    page.locator("aside[data-persist-probe]"),
    "the rail remounted on a step change",
  ).toHaveCount(1);
  await expect(
    page.locator("main[data-persist-probe]"),
    "the content pane remounted on a step change",
  ).toHaveCount(1);
  // One DotSphere canvas, not one per visited step.
  await expect(page.locator("canvas")).toHaveCount(1);
});
