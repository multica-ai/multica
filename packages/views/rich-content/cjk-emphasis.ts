import type { PhrasingContent, Root, Strong, Text } from "mdast";
import { visit } from "unist-util-visit";

const CJK_SCRIPT = /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]/u;

function text(value: string): Text {
  return { type: "text", value };
}

function strong(value: string): Strong {
  return { type: "strong", children: [text(value)] };
}

function repairTextValue(value: string): PhrasingContent[] | null {
  const replacement: PhrasingContent[] = [];
  let consumed = 0;
  let cursor = 0;

  while (cursor < value.length - 1) {
    const opening = value.indexOf("**", cursor);
    if (opening === -1) break;

    const firstInner = value[opening + 2];
    if (firstInner == null || /\s|\*/u.test(firstInner)) {
      cursor = opening + 2;
      continue;
    }

    // Do not look across another delimiter run: that would reinterpret valid
    // or deliberately literal Markdown beyond the one malformed span.
    const closing = value.indexOf("**", opening + 2);
    if (closing === -1) break;

    const afterClosing = value[closing + 2];
    if (
      afterClosing == null ||
      /\s/u.test(afterClosing) ||
      (value[closing - 1] !== " " && value[closing - 1] !== "\t")
    ) {
      cursor = opening + 2;
      continue;
    }

    let whitespaceStart = closing - 1;
    while (
      whitespaceStart > opening + 2 &&
      (value[whitespaceStart - 1] === " " || value[whitespaceStart - 1] === "\t")
    ) {
      whitespaceStart--;
    }

    const inner = value.slice(opening + 2, whitespaceStart);
    if (!CJK_SCRIPT.test(inner) || !/\S/u.test(inner)) {
      cursor = opening + 2;
      continue;
    }

    if (opening > consumed) replacement.push(text(value.slice(consumed, opening)));
    replacement.push(strong(inner), text(value.slice(whitespaceStart, closing)));
    consumed = closing + 2;
    cursor = consumed;
  }

  if (replacement.length === 0) return null;
  if (consumed < value.length) replacement.push(text(value.slice(consumed)));
  return replacement;
}

/**
 * Recover the screenshot's common AI-authored form after Markdown parsing:
 * `**中文。 **next` becomes the equivalent of `**中文。** next`.
 *
 * This transformer is deliberately narrower than a raw string rewrite. It
 * only visits parsed text nodes containing CJK script characters, so code,
 * valid Markdown nodes and non-CJK prose keep their original semantics.
 * Source/value equality also excludes escaped Markdown whose AST text no
 * longer matches the author's literal input.
 */
export function remarkRepairCjkStrongTrailingWhitespace() {
  return (tree: Root, file: { value: unknown }) => {
    const source = String(file.value ?? "");

    visit(tree, "text", (node, index, parent) => {
      if (index == null || parent == null || parent.type === "strong") return;

      const start = node.position?.start.offset;
      const end = node.position?.end.offset;
      if (start == null || end == null || source.slice(start, end) !== node.value) {
        return;
      }

      const replacement = repairTextValue(node.value);
      if (!replacement) return;
      parent.children.splice(index, 1, ...replacement);
    });
  };
}
