import { useRef } from "react";
import type { MentionItem } from "../../editor/extensions/mention-suggestion";
import { useCommentDraftStore, type CommentDraftKey, type CommentRuntimeRouting } from "@multica/core/issues/stores/comment-draft-store";

const AGENT_MENTION = /\[@?(?:\\.|[^\]\\])+\]\(mention:\/\/agent\/([^)]+)\)/g;

/**
 * The latest explicit choice controls routing for each mentioned agent: a
 * "my runtime" row records the requester override; a "current task" row
 * records the run it continues (agent id → that run's task id). When a draft
 * key is supplied the picks persist with the draft, so a reload restores the
 * member's explicit choice instead of silently re-resolving sticky routing
 * to the newest run at send time.
 */
export type CommentRoutingArgs = [] | [ids: string[]] | [ids: string[], sources: Record<string, string>];

function routingOf(
  requester: ReadonlySet<string>,
  sourceTasks: ReadonlyMap<string, string>,
): CommentRuntimeRouting | undefined {
  if (requester.size === 0 && sourceTasks.size === 0) return undefined;
  return { requesterAgentIds: [...requester], sourceTaskIds: Object.fromEntries(sourceTasks) };
}

export function useCommentRuntimeOriginator(draftKey?: CommentDraftKey) {
  const requester = useRef(new Set<string>());
  const sourceTasks = useRef(new Map<string, string>());
  // Persist asynchronously (merged per tick): the popup's prune runs inside
  // the editor's onUpdate, and a synchronous store write there would re-render
  // the composer mid-change and roll the controlled editor back to the stale
  // value — swallowing the keystroke that triggered the update.
  const pendingRouting = useRef<CommentRuntimeRouting | undefined>(undefined);
  const persistScheduled = useRef(false);
  const persist = useRef(() => {
    pendingRouting.current = routingOf(requester.current, sourceTasks.current);
    if (persistScheduled.current) return;
    persistScheduled.current = true;
    queueMicrotask(() => {
      persistScheduled.current = false;
      if (draftKey) useCommentDraftStore.getState().setRuntimeRouting(draftKey, pendingRouting.current);
    });
  });
  // A reload restores the picks the draft persisted — before the composer's
  // first prune runs, so the restored mentions keep their routing.
  if (draftKey) {
    const saved = useCommentDraftStore.getState().getRuntimeRouting(draftKey);
    if (saved && requester.current.size === 0 && sourceTasks.current.size === 0) {
      for (const id of saved.requesterAgentIds) requester.current.add(id);
      for (const [id, task] of Object.entries(saved.sourceTaskIds)) sourceTasks.current.set(id, task);
    }
  }
  return {
    select(item: MentionItem) {
      if (item.type !== "agent") return;
      if (item.agentRuntimeGroup === "requester") {
        requester.current.add(item.id);
        sourceTasks.current.delete(item.id);
      } else if (item.agentRuntimeGroup === "issue" && item.sourceTaskId) {
        requester.current.delete(item.id);
        sourceTasks.current.set(item.id, item.sourceTaskId);
      } else {
        requester.current.delete(item.id);
        sourceTasks.current.delete(item.id);
      }
      persist.current();
    },
    prune(markdown: string) {
      const mentioned = new Set(Array.from(markdown.matchAll(AGENT_MENTION), (match) => match[1]));
      for (const id of requester.current) {
        if (!mentioned.has(id)) requester.current.delete(id);
      }
      for (const id of [...sourceTasks.current.keys()]) {
        if (!mentioned.has(id)) sourceTasks.current.delete(id);
      }
      persist.current();
    },
    requestArgs(markdown: string): CommentRoutingArgs {
      const mentioned = new Set(Array.from(markdown.matchAll(AGENT_MENTION), (match) => match[1]));
      const ids = [...requester.current].filter((id) => mentioned.has(id));
      const sourceEntries = [...sourceTasks.current].filter(([id]) => mentioned.has(id));
      // Conditional arity on purpose: a plain sticky comment spreads to ZERO
      // extra arguments, so existing submission arity (and its tests) stay
      // byte-identical. Only an explicit pick adds positional arguments.
      if (sourceEntries.length) {
        return [ids, Object.fromEntries(sourceEntries)];
      }
      if (ids.length) {
        return [ids];
      }
      return [];
    },
    clear() {
      // No persist: clearing happens on accepted submission, and the routing
      // is consumed with the draft the store's clearDraft removes. Writing
      // here would clone the draft object and break the stale-submit guard's
      // reference comparison (which gates the post-send blur).
      requester.current.clear();
      sourceTasks.current.clear();
    },
  };
}
