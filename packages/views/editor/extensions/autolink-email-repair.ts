/**
 * AutolinkEmailRepair — fixes split email links caused by TipTap autolink.
 *
 * Problem: when a user types `contact@example.co`, TipTap's autolink fires
 * because `.co` is a valid TLD. With `inclusive: false` on the Link extension,
 * the next character typed (e.g. `m` to complete `.com`) lands outside the link
 * mark. The markdown output becomes `[contact@example.co](mailto:…)m` instead
 * of `[contact@example.com](mailto:…)`.
 *
 * Fix: an `appendTransaction` plugin that runs after every doc change. It scans
 * for `mailto:` link marks that are immediately followed by plain text. If the
 * combined string (link text + trailing text) forms a valid email according to
 * the shared link detector, the plugin extends the link mark to cover the full
 * address and updates the href.
 *
 * Only `mailto:` links are affected — regular URL links are left untouched.
 */
import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { detectLinks } from "@multica/ui/markdown/linkify";

export const AutolinkEmailRepairExtension = Extension.create({
  name: "autolinkEmailRepair",

  addProseMirrorPlugins() {
    const linkType = this.editor.schema.marks.link;
    if (!linkType) return [];

    return [
      new Plugin({
        key: new PluginKey("autolinkEmailRepair"),
        appendTransaction(_transactions, _oldState, newState) {
          const { tr } = newState;
          let modified = false;

          newState.doc.descendants((node, pos) => {
            if (!node.isTextblock) return;

            // Walk children of this text block looking for link-mark boundaries.
            for (let i = 0; i < node.childCount - 1; i++) {
              const child = node.child(i);
              const nextChild = node.child(i + 1);

              // Find a text node with a mailto: link mark followed by a plain
              // text node (no link mark).
              const linkMark = child.marks.find(
                (m) =>
                  m.type === linkType &&
                  typeof m.attrs.href === "string" &&
                  m.attrs.href.startsWith("mailto:"),
              );
              if (!linkMark) continue;
              if (!nextChild.isText || !nextChild.text) continue;
              if (nextChild.marks.some((m) => m.type === linkType)) continue;

              const linkText = child.text || "";
              const trailingText = nextChild.text!;

              // Try progressively longer slices of the trailing text to find the
              // longest valid email extension (e.g. `.co` + `.uk` → `.co.uk`).
              let bestLen = 0;
              let bestHref = "";
              for (let len = 1; len <= trailingText.length; len++) {
                const candidate = linkText + trailingText.slice(0, len);
                const matches = detectLinks(candidate);
                const match = matches[0];
                if (
                  matches.length === 1 &&
                  match &&
                  match.type === "email" &&
                  match.text === candidate
                ) {
                  bestLen = len;
                  bestHref = match.url;
                }
              }

              if (bestLen > 0) {
                // Compute absolute positions. `pos` is the position before the
                // text block node; content starts at pos + 1. We need the offset
                // of `child` within the text block.
                let childOffset = 0;
                for (let j = 0; j < i; j++) {
                  childOffset += node.child(j).nodeSize;
                }
                const linkFrom = pos + 1 + childOffset;
                const linkEnd = linkFrom + linkText.length;
                const extendTo = linkEnd + bestLen;

                const newMark = linkType.create({ href: bestHref });
                tr.addMark(linkFrom, extendTo, newMark);
                modified = true;
              }
            }
          });

          return modified ? tr : undefined;
        },
      }),
    ];
  },
});
