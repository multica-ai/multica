import type {
  Dispatch,
  MutableRefObject,
  RefObject,
  SetStateAction
} from "react";
import type { AgentMentionSearchController } from "../AgentMentionSearchController";
import type { AgentRichTextEditorHandle } from "../agentRichText/AgentRichTextEditor";
import type { AgentComposerProps } from "./AgentComposer.types";
import type { useComposerDraftAttachmentsWithConnectors } from "./useComposerDraftAttachmentsWithConnectors";
import type { useComposerFocusAndDrop } from "./useComposerFocusAndDrop";
import type { useComposerLayout } from "./useComposerLayout";
import type { useComposerMentionActions } from "./useComposerMentionActions";
import type { useComposerPaletteCatalog } from "./useComposerPaletteCatalog";
import type { useComposerPresentation } from "./useComposerPresentation";
import type { useComposerProviderTargets } from "./useComposerProviderTargets";
import type { useComposerSlashActions } from "./useComposerSlashActions";
import type { useMentionPaletteFrame } from "./useMentionPaletteFrame";
import type { SessionWorktreeLaunchState } from "./useSessionWorktreeLaunch";
import type { useAgentQuickPromptLibrary } from "./quickPrompts/useAgentQuickPromptLibrary";

export interface AgentComposerViewProps {
  props: AgentComposerProps;
  paletteCatalog: ReturnType<typeof useComposerPaletteCatalog>;
  mentionFrame: ReturnType<typeof useMentionPaletteFrame>;
  slashActions: ReturnType<typeof useComposerSlashActions>;
  mentionActions: ReturnType<typeof useComposerMentionActions>;
  attachments: ReturnType<typeof useComposerDraftAttachmentsWithConnectors>;
  providerState: ReturnType<typeof useComposerProviderTargets>;
  focusAndDrop: ReturnType<typeof useComposerFocusAndDrop>;
  layout: ReturnType<typeof useComposerLayout>;
  presentation: ReturnType<typeof useComposerPresentation>;
  composerRef: RefObject<HTMLFormElement | null>;
  inputShellRef: RefObject<HTMLDivElement | null>;
  promptInputAreaRef: RefObject<HTMLDivElement | null>;
  paletteContentRef: RefObject<HTMLDivElement | null>;
  promptTipRef: RefObject<HTMLSpanElement | null>;
  editorHandleRef: RefObject<AgentRichTextEditorHandle | null>;
  mentionControllerRef: MutableRefObject<AgentMentionSearchController | null>;
  externalPromptEntriesSupported: boolean;
  addExternalPromptEntries: (files: readonly File[]) => void;
  onDismissProjectMenuAutoFocus?: (event: Event) => void;
  paletteDraftPrompt: string;
  showFileMentionPalette: boolean;
  showSlashPalette: boolean;
  activeHighlight: number;
  mentionSearchState: Parameters<
    typeof import("../AgentFileMentionPalette").AgentFileMentionPalette
  >[0]["state"];
  quickPromptLibrary: ReturnType<typeof useAgentQuickPromptLibrary>;
  mentionHighlightedKey: string | null;
  shouldCenterMentionHighlight: boolean;
  isSlashStatusPanelOpen: boolean;
  isReviewPickerOpen: boolean;
  isSelectedProjectMissing: boolean;
  setIsSelectedProjectMissing: (value: boolean) => void;
  setIsPaletteOpen: Dispatch<SetStateAction<boolean>>;
  setHighlightedIndex: Dispatch<SetStateAction<number>>;
  isPlanModeActive: boolean;
  onClearPlanMode: () => void;
  isPromptTipOverflowing: boolean;
  onHistoryNavigation: (direction: "older" | "newer") => boolean;
  sessionWorktreeLaunch: SessionWorktreeLaunchState;
}
