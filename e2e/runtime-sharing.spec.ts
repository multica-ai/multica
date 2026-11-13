import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";

test("runtime owner can save specific members and revoke their access", async ({ page }, testInfo) => {
  const api = new TestApiClient();
  const member = new TestApiClient();
  const run = `${Date.now()}-${process.pid}`;
  await api.login(`runtime-owner-${run}@localhost`, "Runtime Owner");
  const workspace = await api.ensureWorkspace("Runtime sharing", `runtime-sharing-${run}`);
  await api.markUserOnboarded();
  try {
    await member.login(`runtime-member-${run}@localhost`, "Alice");
    const memberId = await api.addMemberByEmail(`runtime-member-${run}@localhost`);
    const runtimeId = await api.seedRuntime("Studio Mac");
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto(`/${workspace.slug}/runtimes/${runtimeId}`);
    await page.getByRole("row", { name: /Studio Mac Built-in/ }).click();
    const scope = (name: string) => page.getByRole("button", { name, exact: true });
    const patched = () => page.waitForResponse((response) => response.url().endsWith(`/api/runtimes/${runtimeId}`) && response.request().method() === "PATCH");
    await expect(scope("Private")).toHaveAttribute("aria-pressed", "true");
    await page.screenshot({ path: testInfo.outputPath("runtime-private.png"), fullPage: true });

    // Choosing members opens a draft picker; nothing persists until Save.
    await scope("Members").click();
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
    await page.getByRole("checkbox", { name: "Alice" }).check();
    await page.screenshot({ path: testInfo.outputPath("runtime-member-picker.png"), fullPage: true });
    const saved = patched();
    await page.getByRole("button", { name: "Save", exact: true }).click();
    expect((await saved).status()).toBe(200);
    await page.reload();
    await expect(scope("Members")).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByRole("button", { name: /Alice.*Edit/ })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("runtime-shared.png"), fullPage: true });

    // Private applies in one click.
    const revoked = patched();
    await scope("Private").click();
    expect((await revoked).status()).toBe(200);
    await page.reload();
    await expect(scope("Private")).toHaveAttribute("aria-pressed", "true");

    await scope("Members").click();
    await page.getByRole("checkbox", { name: "Alice" }).check();
    const sharedAgain = patched();
    await page.getByRole("button", { name: "Save", exact: true }).click();
    expect((await sharedAgain).status()).toBe(200);
    await api.removeMember(memberId);
    await expect(scope("Private")).toHaveAttribute("aria-pressed", "true", { timeout: 15000 });
    await api.addMemberByEmail(`runtime-member-${run}@localhost`);
    await page.reload();
    await expect(scope("Private")).toHaveAttribute("aria-pressed", "true");
  } finally {
    await api.deleteWorkspace();
  }
});
