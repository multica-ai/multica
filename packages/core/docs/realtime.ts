import type { QueryClient } from "@tanstack/react-query";
import type { Doc, DocTree } from "../types";
import { docKeys } from "./queries";
import { DocSummarySchema } from "./schemas";

const liveDocs = new Map<string, number>();

/**
 * Marks a doc as edited live in this tab until the returned function runs.
 * Its body then comes from the live session, so another save does not need
 * to download it again.
 */
export function holdLiveDoc(id: string): () => void {
  liveDocs.set(id, (liveDocs.get(id) ?? 0) + 1);
  let released = false;
  return () => {
    if (released) return;
    released = true;
    const count = (liveDocs.get(id) ?? 1) - 1;
    if (count > 0) liveDocs.set(id, count);
    else liveDocs.delete(id);
  };
}

/**
 * Applies a `doc:updated` event to the cache. Every live save broadcasts one
 * to the whole workspace every few seconds, so it patches the tree and the
 * doc's metadata in place instead of refetching every doc query: the body is
 * fetched again only by a tab that shows it outside a live session.
 */
export function applyDocUpdated(qc: QueryClient, wsId: string, payload: unknown) {
  const event = payload as { doc?: unknown; version?: unknown } | undefined;
  const parsed = DocSummarySchema.safeParse(event?.doc);
  if (!parsed.success) {
    void qc.invalidateQueries({ queryKey: docKeys.all(wsId) });
    return;
  }
  const summary = parsed.data;

  const tree = qc.getQueryData<DocTree>(docKeys.tree(wsId));
  if (tree) {
    const known = tree.docs.find((doc) => doc.id === summary.id);
    if (!known) void qc.invalidateQueries({ queryKey: docKeys.tree(wsId) });
    else if (summary.revision >= known.revision) {
      qc.setQueryData<DocTree>(docKeys.tree(wsId), {
        ...tree,
        docs: tree.docs.map((doc) => (doc.id === summary.id ? summary : doc)),
      });
    }
  }

  const detailKey = docKeys.detail(wsId, summary.id);
  const detail = qc.getQueryData<Doc>(detailKey);
  if (detail && summary.revision >= detail.revision) {
    if (summary.revision === detail.revision) {
      qc.setQueryData<Doc>(detailKey, { ...detail, ...summary });
    } else if (liveDocs.has(summary.id)) {
      qc.setQueryData<Doc>(detailKey, { ...detail, ...summary });
      // The cached body is now behind its revision: fetch it on next use.
      void qc.invalidateQueries({ queryKey: detailKey, refetchType: "none" });
    } else {
      void qc.invalidateQueries({ queryKey: detailKey });
    }
  }

  // Every write that adds or extends a version names it; a move does not.
  if (typeof event?.version === "number") {
    void qc.invalidateQueries({ queryKey: docKeys.versions(wsId, summary.id) });
    void qc.invalidateQueries({ queryKey: docKeys.version(wsId, summary.id, event.version) });
  }
}
