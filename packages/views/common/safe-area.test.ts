// @vitest-environment node
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import ts from "typescript";
import { describe, expect, it } from "vitest";

/**
 * The installed web app draws under the iPhone camera and home indicator
 * (`viewport-fit=cover`), so keeping content out of those areas is on us, and
 * a mistake only shows on a real phone. These read the source for the two
 * ways a layer slips under them.
 */

const REPO = join(__dirname, "../../..");
const WEB_ROOTS = ["packages/views", "packages/ui/components", "apps/web/app", "apps/web/components"];
const DESKTOP_ROOTS = ["apps/desktop/src/renderer"];

function sourceFiles(dir: string, out: string[] = []): string[] {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const entry of entries) {
    if (entry === "node_modules" || entry.startsWith(".")) continue;
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) sourceFiles(path, out);
    else if (/\.tsx$/.test(entry) && !/\.test\.tsx$/.test(entry)) out.push(path);
  }
  return out;
}

interface JsxTag {
  file: string;
  line: number;
  name: string;
  node: ts.JsxOpeningLikeElement;
  /** Every string a prop's value can produce, following same-file constants and helpers. */
  strings: (prop: string) => string;
}

function jsxTags(roots: string[], mentions: RegExp): JsxTag[] {
  const tags: JsxTag[] = [];
  for (const path of roots.flatMap((root) => sourceFiles(join(REPO, root)))) {
    const text = readFileSync(path, "utf8");
    if (!mentions.test(text)) continue;
    const source = ts.createSourceFile(path, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);

    const declarations = new Map<string, ts.Node[]>();
    const declare = (name: string, node: ts.Node) => declarations.set(name, [...(declarations.get(name) ?? []), node]);
    const strings = (root: ts.Node): string[] => {
      const out: string[] = [];
      const seen = new Set<ts.Node>();
      const visit = (node: ts.Node) => {
        if (seen.has(node)) return;
        seen.add(node);
        if (ts.isStringLiteralLike(node) || ts.isTemplateHead(node) || ts.isTemplateMiddle(node) || ts.isTemplateTail(node)) {
          out.push(node.text);
        } else if (ts.isIdentifier(node)) {
          declarations.get(node.text)?.forEach(visit);
        }
        ts.forEachChild(node, visit);
      };
      visit(root);
      return out;
    };

    const walk = (node: ts.Node) => {
      if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.initializer) declare(node.name.text, node.initializer);
      if (ts.isFunctionDeclaration(node) && node.name && node.body) declare(node.name.text, node.body);
      if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
        const attributes = node.attributes.properties.filter(ts.isJsxAttribute);
        tags.push({
          file: relative(REPO, path),
          line: source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1,
          name: node.tagName.getText(source),
          node,
          strings: (prop) =>
            attributes
              .filter((attribute) => attribute.name.getText(source) === prop && attribute.initializer)
              .flatMap((attribute) => strings(attribute.initializer!))
              .join(" "),
        });
      }
      ts.forEachChild(node, walk);
    };
    walk(source);
  }
  return tags;
}

/** `100vw`, `85vh`, `90dvh`, `50svw`, `10vmin`… and Tailwind's `h-screen`, `max-h-dvh`, `w-svw`… */
const VIEWPORT_UNIT =
  /(?<![\w.])\d*\.?\d+(?:[dsl]?v[wh]|v(?:min|max|[ib]))(?![a-z])|(?<![\w-])(?:min-|max-)?(?:w|h|size)-(?:screen|[dsl]v[wh])(?![\w-])/gi;

describe("safe area", () => {
  it("sizes centred dialogs against the safe area, not the screen", () => {
    const offending = jsxTags([...WEB_ROOTS, ...DESKTOP_ROOTS], /<(?:Alert)?DialogContent\b/)
      .filter((tag) => tag.name === "DialogContent" || tag.name === "AlertDialogContent")
      .flatMap((tag) => {
        const units = [...`${tag.strings("className")} ${tag.strings("style")}`.matchAll(VIEWPORT_UNIT)].map((m) => m[0]);
        return units.length ? [`${tag.file}:${tag.line} <${tag.name}> uses ${[...new Set(units)].join(", ")}`] : [];
      });

    expect(
      offending,
      "Dialog and AlertDialog popups sit inside SafeAreaLayer (packages/ui/components/ui/dialog.tsx), which is the safe area " +
        "of the screen. Size them with %, e.g. w-[calc(100%-2rem)] or max-h-[85%]: vw, vh, dvh and svh still measure the " +
        "whole screen, so on an iPhone the dialog reaches under the camera or the home indicator.",
    ).toEqual([]);
  });

  it("pads full-screen layers that hold content by the safe-area insets", () => {
    const offending = jsxTags(WEB_ROOTS, /\binset-0\b/)
      .filter((tag) => {
        const classes = tag.strings("className");
        if (!/(?<![\w-])fixed(?![\w-])/.test(classes) || !/(?<![\w-])inset-0(?![\w-])/.test(classes)) return false;
        if (classes.includes("safe-area-inset")) return false;
        // Backdrops and pointer shields draw nothing a camera could hide.
        if (ts.isJsxSelfClosingElement(tag.node)) return false;
        const element = tag.node.parent as ts.JsxElement;
        return !element.children.every((child) => ts.isJsxText(child) && child.containsOnlyTriviaWhiteSpaces);
      })
      .map((tag) => `${tag.file}:${tag.line} <${tag.name}>`);

    expect(
      offending,
      "A `fixed inset-0` layer with content covers the whole iPhone screen, camera and home indicator included. Pad it with " +
        "pt-[env(safe-area-inset-top)] pr-[env(safe-area-inset-right)] pb-[env(safe-area-inset-bottom)] " +
        "pl-[env(safe-area-inset-left)] (see attachment-preview-modal.tsx), or put a centred popup in a Dialog, " +
        "whose SafeAreaLayer does this for you.",
    ).toEqual([]);
  });
});
