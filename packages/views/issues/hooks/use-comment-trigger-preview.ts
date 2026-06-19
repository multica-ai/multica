"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import type { CommentTriggerPreviewAgent } from "@multica/core/types";

const COMMENT_TRIGGER_PREVIEW_DEBOUNCE_MS = 300;
const MENTION_RE = /\[@?(.+?)\]\(mention:\/\/(member|agent|squad|issue|all)\/([0-9a-fA-F-]+|all)\)/g;
const NOTE_COMMAND_RE = /^\/note(?:$|\s)/i;

export interface UseCommentTriggerPreviewResult {
  agents: CommentTriggerPreviewAgent[];
}

export function isNoteCommentDraft(content: string): boolean {
  return NOTE_COMMAND_RE.test(content.replace(/^[ \t\r\n]+/, ""));
}

export function commentTriggerPreviewSignature(content: string): string {
  if (!content.trim() || isNoteCommentDraft(content)) return "empty";

  const seen = new Set<string>();
  const tokens: string[] = [];
  for (const match of content.matchAll(MENTION_RE)) {
    const type = match[2];
    const id = match[3];
    if (!type || !id || type === "issue") continue;
    const token = `${type}:${id}`;
    if (seen.has(token)) continue;
    seen.add(token);
    tokens.push(token);
  }

  return `nonempty|${tokens.join(",")}`;
}

function useDebouncedSignature(signature: string) {
  const [debouncedSignature, setDebouncedSignature] = useState("empty");

  useEffect(() => {
    if (signature === "empty") {
      setDebouncedSignature("empty");
      return;
    }

    const timer = window.setTimeout(() => {
      setDebouncedSignature(signature);
    }, COMMENT_TRIGGER_PREVIEW_DEBOUNCE_MS);

    return () => window.clearTimeout(timer);
  }, [signature]);

  return debouncedSignature;
}

export function useCommentTriggerPreview({
  issueId,
  parentId,
  editingCommentId,
  content,
}: {
  issueId: string;
  parentId?: string;
  editingCommentId?: string;
  content: string;
}): UseCommentTriggerPreviewResult {
  const signature = useMemo(() => commentTriggerPreviewSignature(content), [content]);
  const debouncedSignature = useDebouncedSignature(signature);
  const contentRef = useRef(content);

  useEffect(() => {
    contentRef.current = content;
  }, [content]);

  const previewQuery = useQuery({
    queryKey: [...issueKeys.commentTriggerPreview(issueId), parentId ?? "", editingCommentId ?? "", debouncedSignature],
    queryFn: () => api.previewCommentTriggers(issueId, contentRef.current, parentId, editingCommentId),
    enabled: signature !== "empty" && debouncedSignature !== "empty",
    retry: false,
    // The answer depends on live queue state (pending-task dedup), not just
    // the mention set, so a cached result must revalidate when its signature
    // reappears — Infinity here once pinned a stale "nobody triggers"
    // snapshot taken while the agent was still queued.
    staleTime: 0,
    // Keep the previous agent list while a new signature is fetching:
    // without it the in-flight gap renders as "no agents", flickering the
    // chips and wiping the composer's suppressed-id set.
    placeholderData: keepPreviousData,
  });

  // Loading and errors intentionally surface as "no agents": the preview is
  // an enhancement, and the composer renders nothing for an empty list.
  if (signature === "empty" || debouncedSignature === "empty") {
    return { agents: [] };
  }

  return { agents: previewQuery.data?.agents ?? [] };
}
