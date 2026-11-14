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
  /** Every string a prop's value can produce, following constants and helpers in scope. */
  strings: (prop: string) => string;
}

function binds(name: ts.BindingName, text: string): boolean {
  if (ts.isIdentifier(name)) return name.text === text;
  return name.elements.some((element) => !ts.isOmittedExpression(element) && binds(element.name, text));
}

/** The initializer or body `reference` names, looked up through the scopes around it. */
function declarationOf(reference: ts.Identifier): ts.Node | undefined {
  for (let scope: ts.Node | undefined = reference.parent; scope; scope = scope.parent) {
    if (ts.isFunctionLike(scope) && scope.parameters.some((parameter) => binds(parameter.name, reference.text))) return undefined;
    if (!ts.isBlock(scope) && !ts.isSourceFile(scope)) continue;
    for (const statement of scope.statements) {
      if (ts.isFunctionDeclaration(statement) && statement.name?.text === reference.text) return statement.body;
      if (!ts.isVariableStatement(statement)) continue;
      const declaration = statement.declarationList.declarations.find((d) => binds(d.name, reference.text));
      if (declaration) return ts.isIdentifier(declaration.name) ? declaration.initializer : undefined;
    }
  }
  return undefined;
}

function stringsIn(root: ts.Node): string[] {
  const out: string[] = [];
  const seen = new Set<ts.Node>();
  const visit = (node: ts.Node) => {
    if (seen.has(node)) return;
    seen.add(node);
    if (ts.isStringLiteralLike(node) || ts.isTemplateHead(node) || ts.isTemplateMiddle(node) || ts.isTemplateTail(node)) {
      out.push(node.text);
    } else if (ts.isIdentifier(node) && !(ts.isPropertyAccessExpression(node.parent) && node.parent.name === node)) {
      const declaration = declarationOf(node);
      if (declaration) visit(declaration);
    }
    ts.forEachChild(node, visit);
  };
  visit(root);
  return out;
}

function tagsIn(path: string, text: string): JsxTag[] {
  const source = ts.createSourceFile(path, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const tags: JsxTag[] = [];
  const walk = (node: ts.Node) => {
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
            .flatMap((attribute) => stringsIn(attribute.initializer!))
            .join(" "),
      });
    }
    ts.forEachChild(node, walk);
  };
  walk(source);
  return tags;
}

function jsxTags(roots: string[], mentions: RegExp): JsxTag[] {
  return roots
    .flatMap((root) => sourceFiles(join(REPO, root)))
    .flatMap((path) => {
      const text = readFileSync(path, "utf8");
      return mentions.test(text) ? tagsIn(path, text) : [];
    });
}

/** `100vw`, `85vh`, `90dvh`, `50svw`, `10vmin`… and Tailwind's `h-screen`, `max-h-dvh`, `w-svw`… */
const VIEWPORT_UNIT =
  /(?<![\w.])\d*\.?\d+(?:[dsl]?v[wh]|v(?:min|max|[ib]))(?![a-z])|(?<![\w-])(?:min-|max-)?(?:w|h|size)-(?:screen|[dsl]v[wh])(?![\w-])/gi;

function dialogsSizedByScreen(tags: JsxTag[]): string[] {
  return tags
    .filter((tag) => tag.name === "DialogContent" || tag.name === "AlertDialogContent")
    .flatMap((tag) => {
      const units = [...`${tag.strings("className")} ${tag.strings("style")}`.matchAll(VIEWPORT_UNIT)].map((m) => m[0]);
      return units.length ? [`${tag.file}:${tag.line} <${tag.name}> uses ${[...new Set(units)].join(", ")}`] : [];
    });
}

function unpaddedFullScreenLayers(tags: JsxTag[]): string[] {
  return tags
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
}

describe("safe area", () => {
  it("sizes centred dialogs against the safe area, not the screen", () => {
    expect(
      dialogsSizedByScreen(jsxTags([...WEB_ROOTS, ...DESKTOP_ROOTS], /<(?:Alert)?DialogContent\b/)),
      "Dialog and AlertDialog popups sit inside SafeAreaLayer (packages/ui/components/ui/dialog.tsx), which is the safe area " +
        "of the screen. Size them with %, e.g. w-[calc(100%-2rem)] or max-h-[85%]: vw, vh, dvh and svh still measure the " +
        "whole screen, so on an iPhone the dialog reaches under the camera or the home indicator.",
    ).toEqual([]);
  });

  it("reads a dialog's classes from the constant in its own scope", () => {
    const source = (dialogWidth: string) => `
      function Menu() {
        const classes = "w-[calc(100vw-2rem)]";
        return <PopoverContent className={classes}>Menu</PopoverContent>;
      }
      function Editor({ wide }: { wide: boolean }) {
        const classes = "${dialogWidth}";
        return <DialogContent className={cn(classes, wide && size(classes))}>Editor</DialogContent>;
      }
      function size(classes: string) {
        return classes;
      }
    `;
    expect(dialogsSizedByScreen(tagsIn(join(REPO, "fixture.tsx"), source("w-[calc(100%-2rem)]")))).toEqual([]);
    expect(dialogsSizedByScreen(tagsIn(join(REPO, "fixture.tsx"), source("w-[calc(100vw-2rem)]")))).toEqual([
      "fixture.tsx:8 <DialogContent> uses 100vw",
    ]);
  });

  it("pads full-screen layers that hold content by the safe-area insets", () => {
    expect(
      unpaddedFullScreenLayers(jsxTags(WEB_ROOTS, /\binset-0\b/)),
      "A `fixed inset-0` layer with content covers the whole iPhone screen, camera and home indicator included. Pad it with " +
        "pt-[env(safe-area-inset-top)] pr-[env(safe-area-inset-right)] pb-[env(safe-area-inset-bottom)] " +
        "pl-[env(safe-area-inset-left)] (see attachment-preview-modal.tsx), or put a centred popup in a Dialog, " +
        "whose SafeAreaLayer does this for you.",
    ).toEqual([]);
  });
});
