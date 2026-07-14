import { afterEach, describe, expect, it } from "vitest";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  loadUpdaterPreferences,
  saveUpdaterPreferences,
  updaterPreferencesPath,
} from "./updater-preferences";

const tempDirs: string[] = [];

async function makePreferencesPath(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "multica-updater-preferences-"));
  tempDirs.push(dir);
  return updaterPreferencesPath(dir);
}

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true })),
  );
});

describe("updater preferences", () => {
  it("defaults automatic updates to enabled when the file is missing or invalid", async () => {
    const missingPath = await makePreferencesPath();
    const invalidPath = await makePreferencesPath();
    await writeFile(invalidPath, JSON.stringify({ automaticUpdates: "false" }));

    await expect(loadUpdaterPreferences(missingPath)).resolves.toEqual({
      automaticUpdates: true,
    });
    await expect(loadUpdaterPreferences(invalidPath)).resolves.toEqual({
      automaticUpdates: true,
    });
  });

  it("round-trips a disabled automatic update preference", async () => {
    const filePath = await makePreferencesPath();

    await saveUpdaterPreferences(filePath, { automaticUpdates: false });

    await expect(loadUpdaterPreferences(filePath)).resolves.toEqual({
      automaticUpdates: false,
    });
    expect(JSON.parse(await readFile(filePath, "utf-8"))).toEqual({
      automaticUpdates: false,
    });
  });
});
