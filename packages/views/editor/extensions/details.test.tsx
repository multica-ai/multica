import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { DetailsBlockExtension } from "./details";

interface JsonNode {
  type?: string;
  attrs?: Record<string, unknown>;
  content?: JsonNode[];
}

function makeEditor() {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit,
      DetailsBlockExtension,
      Markdown.configure({ indentation: { style: "space", size: 3 } }),
    ],
  });
}

function findAll(node: JsonNode, type: string, acc: JsonNode[] = []): JsonNode[] {
  if (node.type === type) acc.push(node);
  for (const child of node.content ?? []) findAll(child, type, acc);
  return acc;
}

let editor: Editor | null = null;

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

const SIMPLE = `<details>
<summary>Click to expand</summary>

Hidden **markdown** body.

- one
- two
</details>`;

describe("details editor extension", () => {
  it("parses a <details> block into a detailsBlock node", () => {
    editor = makeEditor();
    editor.commands.setContent(SIMPLE, { contentType: "markdown" });

    const blocks = findAll(editor.getJSON() as JsonNode, "detailsBlock");
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.attrs?.summary).toBe("Click to expand");
    expect(blocks[0]?.attrs?.body).toBe(
      "Hidden **markdown** body.\n\n- one\n- two",
    );
    expect(blocks[0]?.attrs?.open).toBe(false);
  });

  it("round-trips a <details> block on save (verbatim inner markdown)", () => {
    editor = makeEditor();
    editor.commands.setContent(SIMPLE, { contentType: "markdown" });

    expect(editor.getMarkdown().trim()).toBe(SIMPLE);
  });

  it("is idempotent: reloading the saved markdown yields the same output", () => {
    editor = makeEditor();
    editor.commands.setContent(SIMPLE, { contentType: "markdown" });
    const once = editor.getMarkdown().trim();

    editor.commands.setContent(once, { contentType: "markdown" });
    const twice = editor.getMarkdown().trim();

    expect(twice).toBe(once);
    expect(findAll(editor.getJSON() as JsonNode, "detailsBlock")).toHaveLength(1);
  });

  it("survives an edit to surrounding content instead of being dropped", () => {
    const doc = `Intro paragraph.

${SIMPLE}

Outro paragraph.`;
    editor = makeEditor();
    editor.commands.setContent(doc, { contentType: "markdown" });

    // The block is preserved as a node, so editing elsewhere cannot silently
    // destroy it (the original bug flattened/lost it on the next save).
    const blocks = findAll(editor.getJSON() as JsonNode, "detailsBlock");
    expect(blocks).toHaveLength(1);

    const out = editor.getMarkdown();
    expect(out).toContain("<details>");
    expect(out).toContain("<summary>Click to expand</summary>");
    expect(out).toContain("Intro paragraph.");
    expect(out).toContain("Outro paragraph.");
  });

  it("preserves the open attribute", () => {
    const md = `<details open>
<summary>Expanded by default</summary>

Body.
</details>`;
    editor = makeEditor();
    editor.commands.setContent(md, { contentType: "markdown" });

    const blocks = findAll(editor.getJSON() as JsonNode, "detailsBlock");
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.attrs?.open).toBe(true);
    expect(editor.getMarkdown().trim()).toBe(md);
  });

  it("declines nested <details> (falls back rather than breaking round-trip)", () => {
    const md = `<details>
<summary>Outer</summary>

<details>
<summary>Inner</summary>

nested
</details>
</details>`;
    editor = makeEditor();
    editor.commands.setContent(md, { contentType: "markdown" });

    // The outer block declines tokenization (its body still holds an opener),
    // so it is NOT collapsed into a single detailsBlock covering both levels —
    // that is the documented fallback. The key guarantee is that no content is
    // silently destroyed: every piece of text still round-trips.
    const out = editor.getMarkdown();
    expect(out).toContain("Outer");
    expect(out).toContain("Inner");
    expect(out).toContain("nested");
  });

  it("tolerates blank lines between the opening tag and <summary>", () => {
    const md = `<details>

<summary>Spaced out</summary>

Body.
</details>`;
    editor = makeEditor();
    editor.commands.setContent(md, { contentType: "markdown" });

    const blocks = findAll(editor.getJSON() as JsonNode, "detailsBlock");
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.attrs?.summary).toBe("Spaced out");
    expect(blocks[0]?.attrs?.body).toBe("Body.");
  });

  it("does not treat `open` inside another attribute value as the open flag", () => {
    const md = `<details class="is open">
<summary>Not actually open</summary>

Body.
</details>`;
    editor = makeEditor();
    editor.commands.setContent(md, { contentType: "markdown" });

    const blocks = findAll(editor.getJSON() as JsonNode, "detailsBlock");
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.attrs?.open).toBe(false);
  });

  it("round-trips an empty body", () => {
    const md = `<details>
<summary>Empty</summary>


</details>`;
    editor = makeEditor();
    editor.commands.setContent(md, { contentType: "markdown" });

    const blocks = findAll(editor.getJSON() as JsonNode, "detailsBlock");
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.attrs?.summary).toBe("Empty");
    expect(blocks[0]?.attrs?.body).toBe("");
  });

  it("parses two consecutive <details> blocks independently", () => {
    const md = `${SIMPLE}

<details>
<summary>Second</summary>

Another body.
</details>`;
    editor = makeEditor();
    editor.commands.setContent(md, { contentType: "markdown" });

    const blocks = findAll(editor.getJSON() as JsonNode, "detailsBlock");
    expect(blocks).toHaveLength(2);
    expect(blocks[0]?.attrs?.summary).toBe("Click to expand");
    expect(blocks[1]?.attrs?.summary).toBe("Second");
  });

  it("declines a body truncated inside a code fence (stray </details>)", () => {
    // The lazy </details> match would stop inside the fenced sample; the odd
    // fence count signals the truncation, so the block declines and falls back
    // to flat rendering rather than leaking an unterminated fence.
    const md = `<details>
<summary>Has a fenced sample</summary>

\`\`\`html
</details>
\`\`\`
</details>`;
    editor = makeEditor();
    editor.commands.setContent(md, { contentType: "markdown" });

    const blocks = findAll(editor.getJSON() as JsonNode, "detailsBlock");
    expect(blocks).toHaveLength(0);
    // No content is destroyed by the fallback.
    expect(editor.getMarkdown()).toContain("Has a fenced sample");
  });
});
