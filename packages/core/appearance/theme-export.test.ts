// @vitest-environment node
import { describe, expect, it } from "vitest";
import { BUILT_IN_THEMES, findBuiltInTheme } from "./built-in-themes";
import { readZip, writeZip } from "./theme-archive";
import { exportTheme, themeCopyTarget } from "./theme-export";
import { themeCss, themeFromFiles } from "./theme-package";
import { themeModeVariables, themeOptionAttributes, type Theme } from "./themes";

/** The CSS as the browser applies it: comments and layout whitespace removed. */
const normalized = (css: string) =>
  css
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/\s+/g, " ")
    .replace(/\s*([{};])\s*/g, "$1")
    .trim();

async function reimport(theme: Theme): Promise<Theme> {
  const files = exportTheme(theme, themeCopyTarget(theme, []));
  const archive = await readZip(writeZip(files), 1024 * 1024);
  if (!archive.ok) throw new Error(archive.error);
  const result = themeFromFiles(archive.files);
  if (!result.ok) throw new Error(result.errors.join("\n"));
  return result.theme;
}

describe("exportTheme", () => {
  it.each(BUILT_IN_THEMES.map((theme) => [theme.name, theme] as const))(
    "a duplicate of %s imports and renders the same",
    async (_name, theme) => {
      const copy = await reimport(theme);
      expect(copy.id).toBe(`${theme.id}-copy`);
      expect(normalized(themeCss(copy))).toBe(normalized(themeCss(theme)));
      expect(themeModeVariables(copy.css)).toEqual(themeModeVariables(theme.css));
      expect(copy.options).toEqual(theme.options);
      expect(themeOptionAttributes(copy, undefined)).toEqual(themeOptionAttributes(theme, undefined));
    },
  );

  it("writes the token tables one declaration per line", () => {
    const github = findBuiltInTheme("github")!;
    const css = new TextDecoder().decode(exportTheme(github, { id: "mine", name: "Mine" })[1]!.data);
    expect(css).toMatch(/^\/\* Mine, started from GitHub\./);
    expect(css).toContain(".theme-light {\n  --");
    expect(css).toContain("beta");
  });

  it("writes an installed theme's assets back as files", () => {
    const theme: Theme = {
      apiVersion: 1,
      id: "paper",
      name: "Paper",
      css: "@font-face { src: url(fonts/a.woff2); }",
      assets: { "fonts/a.woff2": "data:font/woff2;base64,d09GMg==" },
    };
    const files = exportTheme(theme, { id: "paper-copy", name: "Paper copy" });
    expect(files.map((f) => f.path)).toEqual(["manifest.json", "theme.css", "fonts/a.woff2"]);
    expect(new TextDecoder().decode(files[2]!.data)).toBe("wOF2");
  });
});

describe("themeCopyTarget", () => {
  it("picks the first free id", () => {
    const pop = findBuiltInTheme("pop")!;
    expect(themeCopyTarget(pop, [])).toEqual({ id: "pop-copy", name: "Pop copy" });
    expect(themeCopyTarget(pop, ["pop-copy", "pop-copy-2"])).toEqual({ id: "pop-copy-3", name: "Pop copy 3" });
  });
});
