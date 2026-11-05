import { useCallback, useMemo, useRef, useState } from "react";
import { useOptionalAgentHostApi } from "../../../../agentActivityHost";
import type {
  AgentHostQuickPrompt,
  AgentHostQuickPromptSnapshot
} from "../../../../host/agentHostApi";
import { useExternalStoreSelector } from "../../../../shared/useExternalStoreSelector";
import type { AgentGUIQuickPromptType } from "../../engagement/agentGUIEngagement.types";
import type { AgentQuickPromptLabels } from "./agentQuickPromptLabels";

const unavailableSnapshot: AgentHostQuickPromptSnapshot = {
  enabled: false,
  status: "idle",
  prompts: [],
  error: null,
  revision: 0,
  pendingMutationIds: []
};
const unavailableQuickPromptStore = {
  getSnapshot: (): AgentHostQuickPromptSnapshot => unavailableSnapshot,
  subscribe:
    (
      _listener: (snapshot: AgentHostQuickPromptSnapshot) => void
    ): (() => void) =>
    () => {}
};

type AgentQuickPromptMode = "closed" | "popover" | "create" | "edit" | "delete";
type AgentQuickPromptMutationError = "conflict" | "generic" | null;

export interface AgentQuickPromptDraft {
  content: string;
  title: string;
}

export interface AgentQuickPromptCreateOptions {
  insertIntoComposerAfterSave?: boolean;
  usagePromptType?: AgentGUIQuickPromptType;
}

export interface AgentQuickPromptLibraryController {
  capabilityAvailable: boolean;
  canReorder: boolean;
  close: () => void;
  closeDialog: () => void;
  deletePrompt: (prompt: AgentHostQuickPrompt) => void;
  filteredPrompts: readonly AgentHostQuickPrompt[];
  isDeleting: boolean;
  isEditorOpen: boolean;
  isPopoverOpen: boolean;
  isSaving: boolean;
  isInteractionLocked: boolean;
  isReordering: boolean;
  initialDraft: AgentQuickPromptDraft | null;
  insertionError: boolean;
  labels: AgentQuickPromptLabels;
  mode: AgentQuickPromptMode;
  mutationError: AgentQuickPromptMutationError;
  openCreate: (
    draft?: AgentQuickPromptDraft,
    options?: AgentQuickPromptCreateOptions
  ) => void;
  openEdit: (prompt: AgentHostQuickPrompt) => void;
  openPopover: () => void;
  promptToDelete: AgentHostQuickPrompt | null;
  retry: () => void;
  reorderCapabilityAvailable: boolean;
  reorderError: AgentQuickPromptMutationError;
  reorderPrompts: (
    promptId: string,
    beforePromptId: string | null
  ) => Promise<boolean>;
  saveDraft: (draft: AgentQuickPromptDraft) => Promise<boolean>;
  searchQuery: string;
  showReorderHandles: boolean;
  selectPrompt: (prompt: AgentHostQuickPrompt) => void;
  selectedPrompt: AgentHostQuickPrompt | null;
  setPopoverOpen: (open: boolean) => void;
  setSearchQuery: (query: string) => void;
  snapshot: AgentHostQuickPromptSnapshot;
  submitDelete: () => Promise<boolean>;
}

export function useAgentQuickPromptLibrary(input: {
  disabled: boolean;
  labels: AgentQuickPromptLabels;
  onBeforeOpen: () => void;
  onInsertPrompt: (content: string) => boolean;
  onQuickPromptPanelOpened?: () => void;
  onQuickPromptUsed?: (promptType: AgentGUIQuickPromptType) => void;
}): AgentQuickPromptLibraryController {
  const {
    disabled,
    labels,
    onBeforeOpen,
    onInsertPrompt,
    onQuickPromptPanelOpened,
    onQuickPromptUsed
  } = input;
  const hostApi = useOptionalAgentHostApi();
  const quickPrompts = hostApi?.quickPrompts;
  const snapshot = useExternalStoreSelector(
    quickPrompts ?? unavailableQuickPromptStore,
    selectQuickPromptSnapshot
  );
  const [mode, setMode] = useState<AgentQuickPromptMode>("closed");
  const modeRef = useRef(mode);
  modeRef.current = mode;
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedPrompt, setSelectedPrompt] =
    useState<AgentHostQuickPrompt | null>(null);
  const [initialDraft, setInitialDraft] =
    useState<AgentQuickPromptDraft | null>(null);
  const createOptionsRef = useRef<AgentQuickPromptCreateOptions | null>(null);
  const [promptToDelete, setPromptToDelete] =
    useState<AgentHostQuickPrompt | null>(null);
  const [mutationError, setMutationError] =
    useState<AgentQuickPromptMutationError>(null);
  const [insertionError, setInsertionError] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isReordering, setIsReordering] = useState(false);
  const [reorderError, setReorderError] =
    useState<AgentQuickPromptMutationError>(null);
  const capabilityAvailable = Boolean(quickPrompts && snapshot.enabled);
  const disclosureAvailable = capabilityAvailable && !disabled;
  const [previousDisclosureAvailable, setPreviousDisclosureAvailable] =
    useState(disclosureAvailable);
  const disclosureAvailableRef = useRef(disclosureAvailable);
  disclosureAvailableRef.current = disclosureAvailable;
  if (previousDisclosureAvailable !== disclosureAvailable) {
    setPreviousDisclosureAvailable(disclosureAvailable);
    modeRef.current = "closed";
    setMode("closed");
    setSelectedPrompt(null);
    setInitialDraft(null);
    createOptionsRef.current = null;
    setPromptToDelete(null);
    setMutationError(null);
    setInsertionError(false);
    setReorderError(null);
  }
  const effectiveMode =
    disclosureAvailable && previousDisclosureAvailable === disclosureAvailable
      ? mode
      : "closed";

  const filteredPrompts = useMemo(() => {
    const query = searchQuery.trim().toLocaleLowerCase();
    if (!query) {
      return snapshot.prompts;
    }
    return snapshot.prompts.filter(
      (prompt) =>
        prompt.title.toLocaleLowerCase().includes(query) ||
        prompt.content.toLocaleLowerCase().includes(query)
    );
  }, [searchQuery, snapshot.prompts]);
  const isInteractionLocked =
    isSaving ||
    isDeleting ||
    isReordering ||
    Boolean(snapshot.orderMutationPending) ||
    snapshot.pendingMutationIds.length > 0;
  const reorderCapabilityAvailable = Boolean(
    quickPrompts?.move && capabilityAvailable
  );
  const canReorder = Boolean(
    reorderCapabilityAvailable &&
      !disabled &&
      !searchQuery.trim() &&
      filteredPrompts.length > 1 &&
      !isInteractionLocked
  );
  const showReorderHandles = Boolean(
    reorderCapabilityAvailable &&
      !searchQuery.trim() &&
      filteredPrompts.length > 1
  );

  const openPopover = useCallback(() => {
    if (!capabilityAvailable || disabled) {
      return;
    }
    onBeforeOpen();
    onQuickPromptPanelOpened?.();
    setMutationError(null);
    setInsertionError(false);
    setReorderError(null);
    modeRef.current = "popover";
    setMode("popover");
    if (snapshot.status === "idle") {
      void quickPrompts?.ensureLoaded().catch(() => undefined);
    }
  }, [
    capabilityAvailable,
    disabled,
    onBeforeOpen,
    onQuickPromptPanelOpened,
    quickPrompts,
    snapshot.status
  ]);

  const close = useCallback(() => {
    modeRef.current = "closed";
    setMode("closed");
    setSelectedPrompt(null);
    setInitialDraft(null);
    createOptionsRef.current = null;
    setPromptToDelete(null);
    setMutationError(null);
    setInsertionError(false);
    setReorderError(null);
  }, []);

  const closeDialog = useCallback(() => {
    const nextMode = capabilityAvailable && !disabled ? "popover" : "closed";
    modeRef.current = nextMode;
    setMutationError(null);
    setReorderError(null);
    setSelectedPrompt(null);
    setInitialDraft(null);
    createOptionsRef.current = null;
    setPromptToDelete(null);
    setInsertionError(false);
    setMode(nextMode);
  }, [capabilityAvailable, disabled]);

  const setPopoverOpen = useCallback(
    (open: boolean) => {
      if (open) {
        openPopover();
      } else if (modeRef.current === "popover") {
        close();
      }
    },
    [close, openPopover]
  );

  const openCreate = useCallback(
    (
      draft?: AgentQuickPromptDraft,
      options?: AgentQuickPromptCreateOptions
    ) => {
      if (isInteractionLocked) return;
      modeRef.current = "create";
      setSelectedPrompt(null);
      setInitialDraft(draft ?? null);
      createOptionsRef.current = options ?? null;
      setMutationError(null);
      setInsertionError(false);
      setMode("create");
    },
    [isInteractionLocked]
  );

  const openEdit = useCallback(
    (prompt: AgentHostQuickPrompt) => {
      if (isInteractionLocked) return;
      modeRef.current = "edit";
      setSelectedPrompt(prompt);
      setInitialDraft(null);
      createOptionsRef.current = null;
      setMutationError(null);
      setInsertionError(false);
      setMode("edit");
    },
    [isInteractionLocked]
  );

  const deletePrompt = useCallback(
    (prompt: AgentHostQuickPrompt) => {
      if (isInteractionLocked) return;
      modeRef.current = "delete";
      setPromptToDelete(prompt);
      setMutationError(null);
      setMode("delete");
    },
    [isInteractionLocked]
  );

  const saveDraft = useCallback(
    async (draft: AgentQuickPromptDraft): Promise<boolean> => {
      if (!quickPrompts || !capabilityAvailable || isInteractionLocked) {
        return false;
      }
      setIsSaving(true);
      setMutationError(null);
      setInsertionError(false);
      const createOptions = selectedPrompt ? null : createOptionsRef.current;
      let saved: AgentHostQuickPrompt;
      try {
        saved = selectedPrompt
          ? await quickPrompts.update({
              id: selectedPrompt.id,
              title: draft.title,
              content: draft.content,
              expectedVersion: selectedPrompt.version
            })
          : await quickPrompts.create(draft);
      } catch (error) {
        const conflict = isVersionConflict(error);
        setMutationError(conflict ? "conflict" : "generic");
        if (conflict && selectedPrompt) {
          try {
            await quickPrompts.ensureLoaded({ force: true });
            const refreshedPrompt = quickPrompts
              .getSnapshot()
              .prompts.find((prompt) => prompt.id === selectedPrompt.id);
            if (refreshedPrompt) setSelectedPrompt(refreshedPrompt);
          } catch {
            setMutationError("conflict");
          }
        }
        return false;
      } finally {
        setIsSaving(false);
      }

      const insertIntoComposerAfterSave = Boolean(
        createOptions?.insertIntoComposerAfterSave &&
          createOptionsRef.current === createOptions &&
          disclosureAvailableRef.current
      );
      if (insertIntoComposerAfterSave) {
        let inserted = false;
        try {
          inserted = onInsertPrompt(saved.content);
        } catch {
          inserted = false;
        }
        if (inserted) {
          if (createOptions?.usagePromptType) {
            onQuickPromptUsed?.(createOptions.usagePromptType);
          }
          close();
          return true;
        }
        setInsertionError(true);
      }

      setSelectedPrompt(saved);
      setInitialDraft(null);
      createOptionsRef.current = null;
      const nextMode = disclosureAvailableRef.current ? "popover" : "closed";
      modeRef.current = nextMode;
      setMode(nextMode);
      return true;
    },
    [
      capabilityAvailable,
      close,
      isInteractionLocked,
      onInsertPrompt,
      onQuickPromptUsed,
      quickPrompts,
      selectedPrompt
    ]
  );

  const submitDelete = useCallback(async (): Promise<boolean> => {
    if (
      !quickPrompts ||
      !capabilityAvailable ||
      !promptToDelete ||
      isInteractionLocked
    ) {
      return false;
    }
    setIsDeleting(true);
    setMutationError(null);
    try {
      await quickPrompts.remove({
        id: promptToDelete.id,
        expectedVersion: promptToDelete.version
      });
      setPromptToDelete(null);
      const nextMode = disclosureAvailableRef.current ? "popover" : "closed";
      modeRef.current = nextMode;
      setMode(nextMode);
      return true;
    } catch (error) {
      const conflict = isVersionConflict(error);
      setMutationError(conflict ? "conflict" : "generic");
      if (conflict) {
        try {
          await quickPrompts.ensureLoaded({ force: true });
          const refreshedPrompt = quickPrompts
            .getSnapshot()
            .prompts.find((prompt) => prompt.id === promptToDelete.id);
          if (refreshedPrompt) setPromptToDelete(refreshedPrompt);
        } catch {
          setMutationError("conflict");
        }
      }
      return false;
    } finally {
      setIsDeleting(false);
    }
  }, [capabilityAvailable, isInteractionLocked, promptToDelete, quickPrompts]);

  const retry = useCallback(() => {
    if (!quickPrompts || !capabilityAvailable || isInteractionLocked) {
      return;
    }
    setReorderError(null);
    void quickPrompts.ensureLoaded({ force: true }).catch(() => undefined);
  }, [capabilityAvailable, isInteractionLocked, quickPrompts]);

  const reorderPrompts = useCallback(
    async (
      promptId: string,
      beforePromptId: string | null
    ): Promise<boolean> => {
      if (!quickPrompts?.move || !canReorder) return false;
      const prompt = snapshot.prompts.find((item) => item.id === promptId);
      if (!prompt) return false;
      setIsReordering(true);
      setReorderError(null);
      try {
        await quickPrompts.move({
          promptId,
          beforePromptId,
          expectedVersion: prompt.version
        });
        return true;
      } catch (error) {
        setReorderError(isOrderConflict(error) ? "conflict" : "generic");
        return false;
      } finally {
        setIsReordering(false);
      }
    },
    [canReorder, quickPrompts, snapshot.prompts]
  );

  const insertPromptContent = useCallback(
    (content: string) => {
      if (disabled || !capabilityAvailable || isInteractionLocked) {
        return;
      }
      let inserted = false;
      try {
        inserted = onInsertPrompt(content);
      } catch {
        inserted = false;
      }
      if (inserted) {
        onQuickPromptUsed?.("saved");
        close();
      } else {
        setInsertionError(true);
      }
    },
    [
      capabilityAvailable,
      close,
      disabled,
      isInteractionLocked,
      onInsertPrompt,
      onQuickPromptUsed
    ]
  );

  const updateSearchQuery = useCallback(
    (query: string) => {
      if (isInteractionLocked) return;
      setSearchQuery(query);
    },
    [isInteractionLocked]
  );
  const selectPrompt = useCallback(
    (prompt: AgentHostQuickPrompt) => {
      insertPromptContent(prompt.content);
    },
    [insertPromptContent]
  );

  return {
    capabilityAvailable,
    canReorder,
    close,
    closeDialog,
    deletePrompt,
    filteredPrompts,
    isDeleting,
    isEditorOpen: effectiveMode === "create" || effectiveMode === "edit",
    isPopoverOpen: effectiveMode === "popover",
    isSaving,
    isInteractionLocked,
    isReordering,
    initialDraft,
    insertionError,
    labels,
    mode: effectiveMode,
    mutationError,
    openCreate,
    openEdit,
    openPopover,
    promptToDelete,
    retry,
    reorderCapabilityAvailable,
    reorderError,
    reorderPrompts,
    saveDraft,
    searchQuery,
    showReorderHandles,
    selectPrompt,
    selectedPrompt,
    setPopoverOpen,
    setSearchQuery: updateSearchQuery,
    snapshot,
    submitDelete
  };
}

function isOrderConflict(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    error.code === "agent_quick_prompt_conflict" &&
    "reason" in error &&
    (error.reason === "agent_quick_prompt_order_conflict" ||
      error.reason === "agent_quick_prompt_version_conflict")
  );
}

function selectQuickPromptSnapshot(
  snapshot: AgentHostQuickPromptSnapshot
): AgentHostQuickPromptSnapshot {
  return snapshot;
}

function isVersionConflict(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    error.code === "agent_quick_prompt_conflict" &&
    "reason" in error &&
    error.reason === "agent_quick_prompt_version_conflict"
  );
}
