import { Extension } from "@tiptap/core";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import { Plugin } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import {
  commitReferenceUrl,
  findCommitReferences,
  isInlineCommitReference,
} from "../../rich-content/commit-references";

type CommitRange = { start: number; reference: string };

export function createCommitReferencesExtension(getRepository: () => string | null | undefined) {
  const blockMatches = new WeakMap<ProseMirrorNode, CommitRange[]>();
  let lastDoc: ProseMirrorNode | undefined;
  let lastRepository: string | undefined;
  let decorations = DecorationSet.empty;

  return Extension.create({
    name: "commitReferences",
    addProseMirrorPlugins() {
      return [new Plugin({
        props: {
          decorations({ doc }) {
            const repository = getRepository();
            if (!repository) return DecorationSet.empty;
            if (lastDoc === doc && lastRepository === repository) return decorations;

            const links: Decoration[] = [];
            doc.descendants((block, pos) => {
              if (!block.isTextblock) return true;
              if (block.type.spec.code) return false;

              let matches = blockMatches.get(block);
              if (!matches) {
                matches = [];
                const ranges = matches;
                let preceding = "";
                block.forEach((child, offset) => {
                  const value = child.text ?? "";
                  if (child.marks.some((mark) => mark.type.name === "code")) {
                    if (isInlineCommitReference(value, preceding)) {
                      ranges.push({ start: offset, reference: value });
                    }
                    preceding = "";
                  } else if (child.marks.some((mark) => mark.type.name === "link")) {
                    preceding = "";
                  } else {
                    for (const { start, reference } of findCommitReferences(value)) {
                      ranges.push({ start: offset + start, reference });
                    }
                    preceding = value;
                  }
                });
                blockMatches.set(block, matches);
              }

              for (const { start, reference } of matches) {
                const from = pos + 1 + start;
                links.push(Decoration.inline(from, from + reference.length, {
                  nodeName: "a",
                  href: commitReferenceUrl(repository, reference),
                }));
              }
              return false;
            });
            lastDoc = doc;
            lastRepository = repository;
            decorations = DecorationSet.create(doc, links);
            return decorations;
          },
        },
      })];
    },
  });
}
