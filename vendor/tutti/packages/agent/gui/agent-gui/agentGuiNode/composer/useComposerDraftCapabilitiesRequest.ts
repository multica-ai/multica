import { useCallback, useRef } from "react";
import { agentComposerDraftPrompt } from "../model/agentComposerDraft";
import type { AgentComposerRenderProps as AgentComposerProps } from "./AgentComposer.types";

interface UseComposerDraftCapabilitiesRequestOptions {
  agentSessionId: string | null;
  onDraftContentChange: AgentComposerProps["onDraftContentChange"];
  onRetryComposerOptions: AgentComposerProps["onRetryComposerOptions"];
  provider: string;
}

export function useComposerDraftCapabilitiesRequest({
  agentSessionId,
  onDraftContentChange,
  onRetryComposerOptions,
  provider
}: UseComposerDraftCapabilitiesRequestOptions): AgentComposerProps["onDraftContentChange"] {
  const requestedKeyRef = useRef<string | null>(null);

  return useCallback(
    (nextDraft, sourceScopeKey) => {
      const nextPrompt = agentComposerDraftPrompt(nextDraft).trimStart();
      if (onRetryComposerOptions && nextPrompt.startsWith("/")) {
        const requestKey = `${provider}:${agentSessionId ?? "draft"}`;
        if (requestedKeyRef.current !== requestKey) {
          requestedKeyRef.current = requestKey;
          onRetryComposerOptions({ section: "capabilities" });
        }
      }
      onDraftContentChange(nextDraft, sourceScopeKey);
    },
    [agentSessionId, onDraftContentChange, onRetryComposerOptions, provider]
  );
}
