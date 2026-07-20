/**
 * Import boundary guard (MUL-4922, Howard's contract #3).
 *
 * The point of this sweep is that there is exactly ONE product-level readonly
 * renderer. That property is not self-enforcing: the cheapest way to add a
 * feature to Chat will always look like "render this bit with the generic
 * Markdown component" or "pass a custom code renderer here", and each of those
 * quietly recreates the second chain we just deleted.
 *
 * These tests read the actual source tree, so they fail on the commit that
 * introduces the fork rather than months later when the surfaces have visibly
 * diverged again.
 */
import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const VIEWS_ROOT = join(__dirname, "..");

// Product surfaces that render user/agent-authored content. Any of these
// reaching for a generic Markdown renderer is the regression.
const PRODUCT_SURFACES = ["chat", "issues", "skills", "autopilots", "inbox"];

function walk(dir: string, out: string[] = []): string[] {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const entry of entries) {
    if (entry === "node_modules" || entry.startsWith(".")) continue;
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (/\.tsx?$/.test(full) && !/\.test\.tsx?$/.test(full)) out.push(full);
  }
  return out;
}

/**
 * Drop comments before matching. These guards look for real imports and real
 * language branches; prose that merely *mentions* one (a doc comment
 * explaining which renderer mounts a leaf) is not a violation, and treating it
 * as one would train the next person to delete the guard.
 */
function stripComments(text: string): string {
  return text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");
}

function sourceFiles(subdirs: string[]): { path: string; text: string }[] {
  return subdirs
    .flatMap((d) => walk(join(VIEWS_ROOT, d)))
    .map((path) => ({
      path: relative(VIEWS_ROOT, path),
      text: stripComments(readFileSync(path, "utf8")),
    }));
}

describe("RichContent import boundary", () => {
  it("no product surface imports the generic ui Markdown renderer", () => {
    const offenders = sourceFiles(PRODUCT_SURFACES)
      .filter(({ text }) =>
        /from\s+["']@multica\/ui\/markdown["']/.test(text) &&
        /\bMarkdown\b|\bMemoizedMarkdown\b|\bStreamingMarkdown\b/.test(text),
      )
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it("the deleted chat markdown bridge has not come back", () => {
    const offenders = sourceFiles([...PRODUCT_SURFACES, "common", "editor"])
      .filter(({ text }) => /common\/markdown/.test(text))
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it("no product surface builds its own react-markdown pipeline", () => {
    // react-markdown may be imported only by the canonical renderer. A second
    // import means a second pipeline with its own sanitize + components map.
    const offenders = sourceFiles([...PRODUCT_SURFACES, "common", "editor"])
      .filter(({ text }) => /from\s+["']react-markdown["']/.test(text))
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it("only the canonical renderer configures the sanitize schema", () => {
    const offenders = sourceFiles([...PRODUCT_SURFACES, "common", "editor"])
      .filter(({ text }) => /rehype-sanitize|markdownSanitizeSchema/.test(text))
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it("only rich-code-block dispatches on a fence language", () => {
    // A `lang === "mermaid"` / `"html"` comparison outside the dispatcher is a
    // per-surface language branch — architecture constraint 2.
    //
    // The Tiptap NodeView is the one sanctioned exception (constraint 6): the
    // EDITABLE code block owns its own preview-toggle lifecycle while the user
    // types, and reuses the same leaf components rather than the readonly
    // renderer. Rewriting the editor is explicitly out of scope for MUL-4922.
    // Narrow, named, and justified — not a general loophole.
    const TIPTAP_NODEVIEW = "editor/extensions/code-block-view.tsx";

    const offenders = sourceFiles([...PRODUCT_SURFACES, "common", "editor"])
      .filter(({ path }) => path !== TIPTAP_NODEVIEW)
      .filter(({ text }) =>
        /(?:lang|language)\w*\s*===\s*["'](?:mermaid|html)["']/.test(text),
      )
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it("Tiptap reuses the leaf components but never imports RichContent", () => {
    // Constraint 6: the editor keeps its own parser/lifecycle. Pulling the
    // readonly renderer into the editor is the classic way this collapses.
    const offenders = walk(join(VIEWS_ROOT, "editor"))
      .filter((p) => !/readonly-content\.tsx$/.test(p))
      .map((path) => ({
        path: relative(VIEWS_ROOT, path),
        // stripComments, consistently with the other checks: an editor file may
        // *mention* RichContent in a doc comment (explaining why a helper is
        // shaped the way it is). Only real code references are violations.
        text: stripComments(readFileSync(path, "utf8")),
      }))
      .filter(({ text }) => /\bRichContent\b/.test(text))
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it("the canonical renderer stays in views, not ui", () => {
    // Mention/Attachment/navigation are product concerns; moving this into
    // packages/ui would break the ui -> no-core/views boundary.
    const uiRoot = join(VIEWS_ROOT, "..", "ui");
    const offenders = walk(uiRoot)
      .map((path) => ({ path, text: stripComments(readFileSync(path, "utf8")) }))
      .filter(({ text }) => /\bRichContent\b|from\s+["']@multica\/views/.test(text))
      .map(({ path }) => relative(uiRoot, path));

    expect(offenders).toEqual([]);
  });
});

describe("chat renders every text entry through RichContent", () => {
  const chatList = readFileSync(
    join(VIEWS_ROOT, "chat/components/chat-message-list.tsx"),
    "utf8",
  );

  it("uses RichContent and no other markdown renderer", () => {
    expect(chatList).toMatch(/\bRichContent\b/);
    expect(chatList).not.toMatch(/MemoizedMarkdown|<Markdown\b/);
  });

  it("keys the live row and the persisted assistant row on the task", () => {
    // The identity contract in source form: if someone reverts to `msg.id`,
    // the parity test's remount assertion and this both fail.
    expect(chatList).toMatch(/task:\$\{/);
    expect(chatList).not.toMatch(/computeItemKey=\{\(_, msg\) => msg\.id\}/);
  });
});
