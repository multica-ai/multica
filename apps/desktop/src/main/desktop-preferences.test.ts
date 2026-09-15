// @vitest-environment node

import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  DEFAULT_DESKTOP_PREFERENCES,
  desktopPreferencesPath,
  loadDesktopPreferences,
  saveDesktopPreferences,
} from "./desktop-preferences";

const temporaryDirectories: string[] = [];

async function makePath(): Promise<string> {
  const directory = await mkdtemp(join(tmpdir(), "multica-desktop-preferences-"));
  temporaryDirectories.push(directory);
  return desktopPreferencesPath(directory);
}

afterEach(async () => {
  await Promise.all(
    temporaryDirectories.splice(0).map((directory) =>
      rm(directory, { recursive: true, force: true }),
    ),
  );
});

describe("desktop preferences", () => {
  it("defaults missing or malformed preferences to close-to-tray", async () => {
    const missing = await makePath();
    const malformed = await makePath();
    await writeFile(malformed, JSON.stringify({ closeBehavior: "destroy" }));

    await expect(loadDesktopPreferences(missing)).resolves.toEqual(
      DEFAULT_DESKTOP_PREFERENCES,
    );
    await expect(loadDesktopPreferences(malformed)).resolves.toEqual(
      DEFAULT_DESKTOP_PREFERENCES,
    );
  });

  it.each(["tray", "taskbar", "quit"] as const)(
    "round-trips the %s close behavior",
    async (closeBehavior) => {
      const filePath = await makePath();
      await saveDesktopPreferences(filePath, { closeBehavior });

      await expect(loadDesktopPreferences(filePath)).resolves.toEqual({
        closeBehavior,
      });
      expect(JSON.parse(await readFile(filePath, "utf-8"))).toEqual({
        closeBehavior,
      });
    },
  );
});
