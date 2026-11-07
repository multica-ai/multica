import { useEffect, useMemo, useRef, useState } from "react";
import { useRichTextMentionService } from "@tutti-os/ui-rich-text/editor";
import type { RichTextTriggerProvider } from "@tutti-os/ui-rich-text/types";
import type { AgentContextMentionProvider } from "../agentContextMentionProvider";
import {
  AgentMentionSearchController,
  type AgentMentionSearchState
} from "../AgentMentionSearchController";
import { DEFAULT_AGENT_MENTION_FILTER } from "../agentMentionSearchHelpers";
import type { AgentComposerReferenceProvenanceFilters } from "./AgentComposer.types";

const EMPTY_AGENT_CONTEXT_MENTION_PROVIDERS: readonly AgentContextMentionProvider[] =
  [];
const EMPTY_HIDDEN_FILTER_IDS: readonly string[] = [];

export function useAgentMentionSearchController(
  referenceProvenanceFilters: AgentComposerReferenceProvenanceFilters | null,
  hiddenFilterIds: readonly string[] = EMPTY_HIDDEN_FILTER_IDS
): {
  mentionControllerRef: React.RefObject<AgentMentionSearchController | null>;
  mentionSearchState: AgentMentionSearchState;
} {
  const mentionService = useRichTextMentionService();
  const contextMentionProviders = useMemo(
    () =>
      mentionService?.listProviders().filter(isAgentContextMentionProvider) ??
      EMPTY_AGENT_CONTEXT_MENTION_PROVIDERS,
    [mentionService]
  );
  const [mentionSearchState, setMentionSearchState] =
    useState<AgentMentionSearchState>(INITIAL_AGENT_MENTION_SEARCH_STATE);
  const mentionControllerRef = useRef<AgentMentionSearchController | null>(
    null
  );
  const sessionProvenanceFilter =
    referenceProvenanceFilters?.byFilter.session.snapshot.value ?? null;
  const fileProvenanceFilter =
    referenceProvenanceFilters?.byFilter.file.snapshot.value ?? null;
  const issueProvenanceFilter =
    referenceProvenanceFilters?.byFilter.issue.snapshot.value ?? null;
  const agentProvenanceFilter =
    referenceProvenanceFilters?.byFilter.agent.snapshot.value ?? null;
  const appProvenanceFilter =
    referenceProvenanceFilters?.byFilter.app.snapshot.value ?? null;
  const provenanceCatalog =
    referenceProvenanceFilters?.byFilter.session.snapshot.catalog ?? null;

  useEffect(() => {
    const controller = new AgentMentionSearchController({
      contextMentionProviders,
      hiddenFilterIds
    });
    controller.setProvenanceCatalog(provenanceCatalog);
    controller.setProvenanceFilters({
      session: sessionProvenanceFilter,
      file: fileProvenanceFilter,
      issue: issueProvenanceFilter,
      agent: agentProvenanceFilter,
      member: null,
      app: appProvenanceFilter
    });
    mentionControllerRef.current = controller;
    const unsubscribe = controller.subscribe(setMentionSearchState);
    return () => {
      unsubscribe();
      controller.dispose();
      mentionControllerRef.current = null;
    };
  }, [contextMentionProviders, hiddenFilterIds]);

  useEffect(() => {
    mentionControllerRef.current?.setProvenanceCatalog(provenanceCatalog);
    mentionControllerRef.current?.setProvenanceFilters({
      session: sessionProvenanceFilter,
      file: fileProvenanceFilter,
      issue: issueProvenanceFilter,
      agent: agentProvenanceFilter,
      member: null,
      app: appProvenanceFilter
    });
  }, [
    provenanceCatalog,
    sessionProvenanceFilter,
    fileProvenanceFilter,
    issueProvenanceFilter,
    agentProvenanceFilter,
    appProvenanceFilter
  ]);

  return { mentionControllerRef, mentionSearchState };
}

function isAgentContextMentionProvider(
  provider: RichTextTriggerProvider
): provider is AgentContextMentionProvider {
  return provider.trigger === "@";
}

const INITIAL_AGENT_MENTION_SEARCH_STATE: AgentMentionSearchState = {
  status: "idle",
  query: "",
  mode: "browse",
  filter: DEFAULT_AGENT_MENTION_FILTER,
  categories: [],
  groups: [],
  error: null
};
