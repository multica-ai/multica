import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import {
  isCloseBehavior,
  type DesktopPreferences,
} from "../shared/desktop-preferences";

export const DEFAULT_DESKTOP_PREFERENCES: DesktopPreferences = {
  closeBehavior: "tray",
};

export function desktopPreferencesPath(userDataPath: string): string {
  return join(userDataPath, "desktop-preferences.json");
}

function parseDesktopPreferences(value: unknown): DesktopPreferences {
  const candidate = value as { closeBehavior?: unknown } | null;
  return typeof value === "object" &&
    value !== null &&
    isCloseBehavior(candidate?.closeBehavior)
    ? { closeBehavior: candidate.closeBehavior }
    : { ...DEFAULT_DESKTOP_PREFERENCES };
}

export async function loadDesktopPreferences(
  filePath: string,
): Promise<DesktopPreferences> {
  try {
    return parseDesktopPreferences(JSON.parse(await readFile(filePath, "utf-8")));
  } catch {
    return { ...DEFAULT_DESKTOP_PREFERENCES };
  }
}

export async function saveDesktopPreferences(
  filePath: string,
  preferences: DesktopPreferences,
): Promise<void> {
  await mkdir(dirname(filePath), { recursive: true });
  const temporaryPath = `${filePath}.tmp`;
  await writeFile(temporaryPath, JSON.stringify(preferences, null, 2), "utf-8");
  await rename(temporaryPath, filePath);
}
