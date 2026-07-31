/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it } from "vitest";
import { cleanup } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { useValidateNewFilePath } from "./skill-detail-page";

afterEach(cleanup);

/**
 * The validator is a hook because its messages are translated, so it is
 * exercised through a probe rather than called directly. FileTree delegates to
 * it instead of carrying a second copy of what counts as a legal path, which is
 * why these cases live here and not in the tree's own tests.
 */
async function check(path: string, existing: string[]) {
  let result = "unset";
  function Probe() {
    result = useValidateNewFilePath()(path, existing);
    return null;
  }
  await renderWithI18n(<Probe />);
  return result;
}

describe("useValidateNewFilePath", () => {
  it("accepts a nested path, which is the only way a folder gets made", async () => {
    // Directories are not stored; they exist because a path contains a slash.
    // Typing one is the whole folder-creation flow.
    expect(await check("references/api.md", ["notes.txt"])).toBe("");
  });

  it("refuses a name that existing files are already nested under", async () => {
    // buildTree merges a file of this name into the folder's node, so the file
    // drops off the rail while still sitting in the draft — saved, invisible.
    expect(await check("references", ["references/api.md"])).not.toBe("");
  });

  it("refuses nesting under a path that is a file", async () => {
    expect(await check("notes.txt/extra.md", ["notes.txt"])).not.toBe("");
  });

  it("refuses the reserved primary path", async () => {
    // SKILL.md maps to skill.Content; the server drops it from the files list.
    expect(await check("SKILL.md", [])).not.toBe("");
  });

  it("refuses absolute paths, escapes, duplicates and blanks", async () => {
    expect(await check("/abs.md", [])).not.toBe("");
    expect(await check("../escape.md", [])).not.toBe("");
    expect(await check("notes.txt", ["notes.txt"])).not.toBe("");
    expect(await check("", [])).not.toBe("");
  });
});
