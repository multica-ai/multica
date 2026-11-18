import { useRef } from "react";
import type { MentionItem } from "../../editor/extensions/mention-suggestion";

const AGENT_MENTION = /\[@?(?:\\.|[^\]\\])+\]\(mention:\/\/agent\/([^)]+)\)/g;

/** The latest explicit choice controls routing for each mentioned agent. */
export function useCommentRuntimeOriginator() {
  const requester = useRef(new Set<string>());
  return {
    select(item: MentionItem) {
      if (item.type !== "agent") return;
      if (item.agentRuntimeGroup === "requester") requester.current.add(item.id);
      else requester.current.delete(item.id);
    },
    prune(markdown: string) {
      const mentioned = new Set(Array.from(markdown.matchAll(AGENT_MENTION), (match) => match[1]));
      for (const id of requester.current) {
        if (!mentioned.has(id)) requester.current.delete(id);
      }
    },
    requestArgs(markdown: string): [] | [string[]] {
      const mentioned = new Set(Array.from(markdown.matchAll(AGENT_MENTION), (match) => match[1]));
      const ids = [...requester.current].filter((id) => mentioned.has(id));
      return ids.length ? [ids] : [];
    },
    clear() {
      requester.current.clear();
    },
  };
}
