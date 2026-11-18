import { useRef } from "react";
import type { MentionItem } from "../../editor/extensions/mention-suggestion";

const AGENT_MENTION = /\[@?(?:\\.|[^\]\\])+\]\(mention:\/\/agent\/([^)]+)\)/g;

/**
 * The latest explicit choice controls routing for each mentioned agent: a
 * "my runtime" row records the requester override; a "current task" row
 * records the run it continues (agent id → that run's task id).
 */
export function useCommentRuntimeOriginator() {
  const requester = useRef(new Set<string>());
  const sourceTasks = useRef(new Map<string, string>());
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
    },
    prune(markdown: string) {
      const mentioned = new Set(Array.from(markdown.matchAll(AGENT_MENTION), (match) => match[1]));
      for (const id of requester.current) {
        if (!mentioned.has(id)) requester.current.delete(id);
      }
      for (const id of [...sourceTasks.current.keys()]) {
        if (!mentioned.has(id)) sourceTasks.current.delete(id);
      }
    },
    requestArgs(markdown: string): [string[], Record<string, string>] {
      const mentioned = new Set(Array.from(markdown.matchAll(AGENT_MENTION), (match) => match[1]));
      const ids = [...requester.current].filter((id) => mentioned.has(id));
      const sourceEntries = [...sourceTasks.current].filter(([id]) => mentioned.has(id));
      // Empty values are harmless: the client omits both payload fields when
      // the arrays/maps are empty, so a plain sticky comment stays sticky.
      return [ids, Object.fromEntries(sourceEntries)];
    },
    clear() {
      requester.current.clear();
      sourceTasks.current.clear();
    },
  };
}
