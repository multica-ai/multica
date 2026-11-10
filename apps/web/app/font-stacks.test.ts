import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const read = (path: string) => readFileSync(resolve(__dirname, path), "utf8");
const webCss = read("./globals.css");
const desktopCss = read("../../desktop/src/renderer/src/globals.css");
const layout = read("./layout.tsx");

const GENERIC = /^(?:serif|sans-serif|monospace|cursive|fantasy|system-ui|math|emoji|fangsong|ui-[a-z-]+)$/;

function rootVariable(css: string, name: string): string {
  const match = css.match(new RegExp(`^\\s*${name}:\\s*([^;]+);`, "m"));
  if (!match?.[1]) throw new Error(`${name} is not declared`);
  return match[1];
}

/** The families next/font writes into `variable`: the face, its metric fallback and `fallback`. */
function nextFontFamilies(variable: string): string[] {
  const call = layout.split(/\n(?=const \w+ = \w+\()/).find((block) => block.includes(`variable: "${variable}"`));
  if (!call) throw new Error(`no next/font call declares ${variable}`);
  const fallback = call.match(/fallback:\s*\[([^\]]*)\]/)?.[1] ?? "";
  return ["<face>", "<face> Fallback", ...[...fallback.matchAll(/"([^"]+)"/g)].map((m) => m[1]!)];
}

function families(stack: string): string[] {
  return stack.split(",").flatMap((part) => {
    const family = part.trim().replace(/^["']|["']$/g, "");
    const variable = family.match(/^var\((--[\w-]+)\)$/)?.[1];
    return variable ? nextFontFamilies(variable) : [family];
  });
}

describe("Appearance Latin faces", () => {
  // The CJK choice follows these in --font-sans and --font-content, so a
  // generic family here would draw Han before the CJK font is reached.
  const names = ["--font-ui-inter", "--font-ui-serif", "--font-ui-mono"];

  it.each(names)("web %s names families only", (name) => {
    expect(families(rootVariable(webCss, name)).filter((f) => GENERIC.test(f))).toEqual([]);
  });

  it.each(names)("desktop %s names families only", (name) => {
    expect(families(rootVariable(desktopCss, name)).filter((f) => GENERIC.test(f))).toEqual([]);
  });

  it("keeps the onboarding serif ending with its generic fallbacks", () => {
    expect(families(rootVariable(webCss, "--font-serif")).slice(-2)).toEqual(["Times New Roman", "serif"]);
    expect(families(rootVariable(webCss, "--font-serif"))).toContain("ui-serif");
  });
});
