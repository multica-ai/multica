"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "@multica/core/api";
import type { CommentTriggerPreviewAgent } from "@multica/core/types";

const COMMENT_TRIGGER_PREVIEW_DEBOUNCE_MS = 300;
const MENTION_RE = /\[@?(.+?)\]\(mention:\/\/(member|agent|squad|issue|all)\/([0-9a-fA-F-]+|all)\)/g;

export type CommentTriggerPreviewStatus = "idle" | "loading" | "error";

export interface UseCommentTriggerPreviewResult {
  agents: CommentTriggerPreviewAgent[];
  status: CommentTriggerPreviewStatus;
}

export function commentTriggerPreviewSignature(content: string): string {
  if (!content.trim()) return "empty";

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

export function useCommentTriggerPreview({
  issueId,
  parentId,
  content,
}: {
  issueId: string;
  parentId?: string;
  content: string;
}): UseCommentTriggerPreviewResult {
  const signature = useMemo(() => commentTriggerPreviewSignature(content), [content]);
  const cacheRef = useRef(new Map<string, CommentTriggerPreviewAgent[]>());
  const contentRef = useRef(content);
  const requestIdRef = useRef(0);
  const [agents, setAgents] = useState<CommentTriggerPreviewAgent[]>([]);
  const [status, setStatus] = useState<CommentTriggerPreviewStatus>("idle");

  useEffect(() => {
    contentRef.current = content;
  }, [content]);

  useEffect(() => {
    const cacheKey = `${issueId}:${parentId ?? ""}:${signature}`;
    requestIdRef.current += 1;
    const requestId = requestIdRef.current;

    if (signature === "empty") {
      setAgents([]);
      setStatus("idle");
      return;
    }

    const cached = cacheRef.current.get(cacheKey);
    if (cached) {
      setAgents(cached);
      setStatus("idle");
      return;
    }

    setStatus("loading");
    const timer = window.setTimeout(() => {
      api.previewCommentTriggers(issueId, contentRef.current, parentId)
        .then((preview) => {
          if (requestIdRef.current !== requestId) return;
          const nextAgents = preview.agents ?? [];
          cacheRef.current.set(cacheKey, nextAgents);
          setAgents(nextAgents);
          setStatus("idle");
        })
        .catch(() => {
          if (requestIdRef.current !== requestId) return;
          setAgents([]);
          setStatus("error");
        });
    }, COMMENT_TRIGGER_PREVIEW_DEBOUNCE_MS);

    return () => window.clearTimeout(timer);
  }, [issueId, parentId, signature]);

  return { agents, status };
}
