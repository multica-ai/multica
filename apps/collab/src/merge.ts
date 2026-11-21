import { diff3Merge, diffIndices } from "node-diff3";
import type { DocumentCodec, JSONContent } from "@multica/views/editor/schema/codec";

export interface MergeConflict {
  /** The live document's version of the contested blocks, as Markdown. */
  current: string;
  /** The writer's version of the same blocks, as Markdown. */
  incoming: string;
}

export type MergeResult =
  | { ok: true; doc: JSONContent }
  | { ok: false; conflicts: MergeConflict[] };

/**
 * Three-way merge of whole documents at top-level block granularity.
 *
 * `base` is what the writer read, `current` is the live document, `incoming`
 * is what the writer wants. Blocks only one side changed are taken from that
 * side; a block both sides changed differently is a conflict, and the writer
 * has to re-read and retry. Each block is compared by its own serialized
 * Markdown, not line by line: the live document and freshly parsed Markdown
 * describe the same block with different field order and default attributes,
 * and formatting the serializer normalizes away is not a change.
 */
export function mergeDocuments(
  codec: DocumentCodec,
  base: JSONContent,
  current: JSONContent,
  incoming: JSONContent,
): MergeResult {
  const blocks = new Map<string, JSONContent>();
  const keys = (doc: JSONContent) =>
    (doc.content ?? []).map((block) => {
      const key = codec.serialize({ type: "doc", content: [block] });
      blocks.set(key, block);
      return key;
    });
  const regions = diff3Merge(keys(current), keys(base), keys(incoming));
  const merged: JSONContent[] = [];
  const conflicts: MergeConflict[] = [];
  const toMarkdown = (part: string[]) =>
    codec.serialize({ type: "doc", content: part.map((key) => blocks.get(key)!) });
  for (const region of regions) {
    if (region.ok) {
      merged.push(...region.ok.map((key) => blocks.get(key)!));
    } else {
      conflicts.push({
        current: toMarkdown(region.conflict.a),
        incoming: toMarkdown(region.conflict.b),
      });
    }
  }
  if (conflicts.length > 0) return { ok: false, conflicts };
  return { ok: true, doc: { type: "doc", content: merged } };
}

const isPendingUpload = (block: JSONContent) =>
  (block.type === "image" || block.type === "fileCard") && block.attrs?.uploading === true;

/**
 * Prepares a document parsed from Markdown to replace the live one.
 *
 * Markdown does not carry everything the live document holds: image size,
 * file size, and placeholders for uploads still in flight. A writer working
 * from Markdown never saw them, so its write must not remove them. Every
 * top-level block whose Markdown is unchanged keeps its live copy. A
 * placeholder serializes like an empty paragraph, so it pairs with the one
 * its gap parses back into; inside a region the writer changed it is kept,
 * on the side of the changed blocks it was on.
 */
export function keepLiveBlocks(codec: DocumentCodec, live: JSONContent, next: JSONContent): JSONContent {
  const key = (block: JSONContent) => codec.serialize({ type: "doc", content: [block] });
  const current = live.content ?? [];
  const incoming = next.content ?? [];
  const content: JSONContent[] = [];
  let i = 0;
  for (const { buffer1: [start, length], buffer2: [from, count] } of diffIndices(current.map(key), incoming.map(key))) {
    content.push(...current.slice(i, start));
    const replaced = current.slice(start, start + length);
    const firstContent = replaced.findIndex((block) => !isPendingUpload(block));
    const before = firstContent === -1 ? replaced : replaced.slice(0, firstContent);
    content.push(...before, ...incoming.slice(from, from + count), ...replaced.slice(before.length).filter(isPendingUpload));
    i = start + length;
  }
  content.push(...current.slice(i));
  return { ...next, content };
}
