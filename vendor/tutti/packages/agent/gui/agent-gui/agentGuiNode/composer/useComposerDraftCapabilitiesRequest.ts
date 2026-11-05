import { useCallback, useEffect, useRef } from "react";
import { agentComposerDraftPrompt } from "../model/agentComposerDraft";
import type { AgentComposerRenderProps as AgentComposerProps } from "./AgentComposer.types";

interface UseComposerDraftCapabilitiesRequestOptions {
  agentSessionId: string | null;
  draftScopeKey: string;
  isActive: boolean;
  onDraftContentChange: AgentComposerProps["onDraftContentChange"];
  onRetryComposerOptions: AgentComposerProps["onRetryComposerOptions"];
  provider: string;
}

export function useComposerDraftCapabilitiesRequest({
  agentSessionId,
  draftScopeKey,
  isActive,
  onDraftContentChange,
  onRetryComposerOptions,
  provider
}: UseComposerDraftCapabilitiesRequestOptions): AgentComposerProps["onDraftContentChange"] {
  const requestedKeyRef = useRef<string | null>(null);

  useEffect(() => {
    if (!isActive || !onRetryComposerOptions) return;
    const prefetch = () => onRetryComposerOptions({ section: "capabilities" });
    if (typeof window.requestIdleCallback === "function") {
      const id = window.requestIdleCallback(prefetch, { timeout: 500 });
      return () => window.cancelIdleCallback(id);
    }
    const id = globalThis.setTimeout(prefetch, 120);
    return () => globalThis.clearTimeout(id);
  }, [agentSessionId, draftScopeKey, isActive, onRetryComposerOptions, provider]);

  return useCallback(
    (nextDraft, sourceScopeKey) => {
      const nextPrompt = agentComposerDraftPrompt(nextDraft).trimStart();
      if (onRetryComposerOptions && nextPrompt.startsWith("/")) {
        const requestKey = JSON.stringify([provider, agentSessionId, draftScopeKey]);
        if (requestedKeyRef.current !== requestKey) {
          requestedKeyRef.current = requestKey;
          onRetryComposerOptions({ section: "capabilities" });
        }
      }
      onDraftContentChange(nextDraft, sourceScopeKey);
    },
    [agentSessionId, draftScopeKey, onDraftContentChange, onRetryComposerOptions, provider]
  );
}
