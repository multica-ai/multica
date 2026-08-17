"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import { parseMentions } from "@multica/core/issues/comment-trigger-outcomes";
import type { CommentTriggerPreviewAgent, CommentTriggerOutcome } from "@multica/core/types";

const COMMENT_TRIGGER_PREVIEW_DEBOUNCE_MS = 300;
const NOTE_COMMAND_RE = /^\/note(?:$|\s)/i;

export interface UseCommentTriggerPreviewResult {
  agents: CommentTriggerPreviewAgent[];
  // Explicit @agent / @squad mentions that will NOT trigger if posted as-is
  // (MUL-4525 §2), so the composer can warn before sending.
  blocked: CommentTriggerOutcome[];
}

export function isNoteCommentDraft(content: string): boolean {
  return NOTE_COMMAND_RE.test(content.replace(/^[ \t\r\n]+/, ""));
}

export function commentTriggerPreviewSignature(content: string): string {
  if (!content.trim() || isNoteCommentDraft(content)) return "empty";

  const seen = new Set<string>();
  const tokens: string[] = [];
  for (const { type, id } of parseMentions(content)) {
    if (type === "issue") continue;
    const token = `${type}:${id}`;
    if (seen.has(token)) continue;
    seen.add(token);
    tokens.push(token);
  }

  return `nonempty|${tokens.join(",")}`;
}

function queryKeyMatchesPreviewContext(
  queryKey: readonly unknown[] | undefined,
  issueId: string,
  parentId: string,
  editingCommentId: string,
) {
  if (!queryKey) return false;
  const prefix = issueKeys.commentTriggerPreview(issueId);
  return (
    prefix.every((part, index) => queryKey[index] === part) &&
    queryKey[prefix.length] === parentId &&
    queryKey[prefix.length + 1] === editingCommentId
  );
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
  const parentKey = parentId ?? "";
  const editingKey = editingCommentId ?? "";

  useEffect(() => {
    contentRef.current = content;
  }, [content]);

  const previewQuery = useQuery({
    queryKey: [...issueKeys.commentTriggerPreview(issueId), parentKey, editingKey, debouncedSignature],
    queryFn: () => api.previewCommentTriggers(issueId, contentRef.current, parentId, editingCommentId),
    enabled: signature !== "empty" && debouncedSignature !== "empty",
    retry: false,
    // The answer depends on live queue state (pending-task dedup), not just
    // the mention set, so a cached result must revalidate when its signature
    // reappears — Infinity here once pinned a stale "nobody triggers"
    // snapshot taken while the agent was still queued.
    staleTime: 0,
    // Keep the previous agent list only while the same composer context is
    // re-fetching. Crossing issue/parent/edit context must not display stale
    // chips from another composer.
    placeholderData: (previousData, previousQuery) =>
      queryKeyMatchesPreviewContext(previousQuery?.queryKey, issueId, parentKey, editingKey)
        ? keepPreviousData(previousData)
        : undefined,
  });

  // Loading and errors intentionally surface as "no agents": the preview is
  // an enhancement, and the composer renders nothing for an empty list.
  if (signature === "empty" || debouncedSignature === "empty") {
    return { agents: [], blocked: [] };
  }

  return {
    agents: previewQuery.data?.agents ?? [],
    blocked: previewQuery.data?.blocked ?? [],
  };
}
