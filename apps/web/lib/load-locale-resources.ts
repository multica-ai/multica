import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import type { LocaleResources, SupportedLocale } from "@multica/core/i18n";

const LOCALES_DIR = resolve(process.cwd(), "../../packages/views/locales");

const NAMESPACES = [
  "common",
  "auth",
  "settings",
  "issues",
  "agents",
  "editor",
  "onboarding",
  "invite",
  "labels",
  "members",
  "my-issues",
  "search",
  "inbox",
  "workspace",
  "projects",
  "autopilots",
  "skills",
  "chat",
  "modals",
  "runtimes",
  "layout",
  "usage",
  "ui",
  "squads",
  "billing",
] as const;

export async function loadLocaleResources(
  locale: SupportedLocale,
): Promise<LocaleResources> {
  const entries = await Promise.all(
    NAMESPACES.map(async (namespace) => {
      const filePath = resolve(LOCALES_DIR, locale, `${namespace}.json`);
      const content = await readFile(filePath, "utf8");
      return [namespace, JSON.parse(content)] as const;
    }),
  );

  return Object.fromEntries(entries) as LocaleResources;
}
