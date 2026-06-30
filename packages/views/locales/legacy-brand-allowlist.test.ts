import { readdirSync, readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const LOCALES_DIR = `${process.cwd()}/locales`;

const ALLOWED_LEGACY_BRAND_PATHS = [
  "en/auth.json:web.desktop_handoff.opening_description",
  "en/auth.json:web.desktop_handoff.opening_title",
  "en/auth.json:web.desktop_handoff.open_button",
  "en/onboarding.json:cli_install.step1_label",
  "en/onboarding.json:step_workspace.next_agent",
  "en/runtimes.json:connect.step1_label",
  "en/runtimes.json:update.managed_by_desktop_title",
  "zh-Hans/auth.json:web.desktop_handoff.opening_description",
  "zh-Hans/auth.json:web.desktop_handoff.opening_title",
  "zh-Hans/auth.json:web.desktop_handoff.open_button",
  "zh-Hans/onboarding.json:cli_install.step1_label",
  "zh-Hans/onboarding.json:step_workspace.next_agent",
  "zh-Hans/runtimes.json:connect.step1_label",
  "zh-Hans/runtimes.json:update.managed_by_desktop_title",
].sort();

function collectLegacyBrandPaths(
  value: unknown,
  file: string,
  path: string[] = [],
): string[] {
  if (typeof value === "string") {
    return /\bMultica\b/.test(value) ? [`${file}:${path.join(".")}`] : [];
  }
  if (Array.isArray(value)) {
    return value.flatMap((item, index) =>
      collectLegacyBrandPaths(item, file, [...path, String(index)]),
    );
  }
  if (value && typeof value === "object") {
    return Object.entries(value).flatMap(([key, item]) =>
      collectLegacyBrandPaths(item, file, [...path, key]),
    );
  }
  return [];
}

describe("legacy visible brand allowlist", () => {
  it("keeps every remaining shared Multica label intentional", () => {
    const found = ["en", "zh-Hans"]
      .flatMap((locale) =>
        readdirSync(`${LOCALES_DIR}/${locale}`)
          .filter((file) => file.endsWith(".json"))
          .flatMap((file) => {
            const relativeFile = `${locale}/${file}`;
            const value: unknown = JSON.parse(
              readFileSync(`${LOCALES_DIR}/${relativeFile}`, "utf8"),
            );
            return collectLegacyBrandPaths(value, relativeFile);
          }),
      )
      .sort();

    expect(found).toEqual(ALLOWED_LEGACY_BRAND_PATHS);
  });
});
