import { Slot } from "radix-ui";
import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
  Spinner
} from "@tutti-os/ui-system";
import { FileText, MessageSquareText, X } from "lucide-react";
import { observer } from "mobx-react-lite";
import { cn } from "../../../app/renderer/lib/utils";
import { translate } from "../../../i18n/index";
import { pastedTextPreview } from "../model/agentComposerDraft";
import type {
  AgentComposerDraftImage,
  AgentComposerDraftLargeText,
  AgentComposerQuoteBlock
} from "../model/agentGuiNodeTypes";
import { AgentComposerDraftImagePreview } from "./AgentComposerDraftPreview";
import { AGENT_COMPOSER_PASTED_TEXT_FILE_PREFIX } from "./composerDraftUtils";

interface Props {
  draftImages: AgentComposerDraftImage[];
  draftLargeTexts: AgentComposerDraftLargeText[];
  draftQuotes: AgentComposerQuoteBlock[];
  removeLabel: string;
  onRemoveImage: (id: string) => void;
  onRemoveLargeText: (id: string) => void;
  onExpandLargeText: (id: string) => void;
  onRemoveQuotes: () => void;
}

export const ComposerDraftAttachments = observer(
  function ComposerDraftAttachments({
    draftImages,
    draftLargeTexts: visibleDraftLargeTexts,
    draftQuotes,
    removeLabel,
    onRemoveImage: removeDraftImage,
    onRemoveLargeText: removeDraftLargeText,
    onExpandLargeText: expandDraftLargeTextToPrompt,
    onRemoveQuotes
  }: Props) {
    const labels = useMemo(
      () => ({ removeMention: removeLabel }),
      [removeLabel]
    );
    const quoteCountLabel = translate(
      draftQuotes.length === 1
        ? "agentHost.agentGui.selectionReferenceCountOne"
        : "agentHost.agentGui.selectionReferenceCountMany",
      { count: draftQuotes.length }
    );
    const hasAttachments =
      draftQuotes.length > 0 ||
      draftImages.length > 0 ||
      visibleDraftLargeTexts.length > 0;
    if (!hasAttachments) return null;
    return (
      <div
        className="mb-2 flex min-h-0 max-w-full flex-col gap-2"
        data-testid="agent-gui-composer-attachment-drafts"
      >
        {draftQuotes.length > 0 ? (
          <div
            className="flex max-w-full items-center"
            data-testid="agent-gui-composer-quote-drafts"
          >
            <Popover>
              <div className="group inline-flex max-w-full items-center rounded-[10px] border border-[var(--line-1)] bg-[var(--background-fronted)] text-sm font-medium text-[var(--text-primary)]">
                <PopoverTrigger asChild>
                  <button
                    type="button"
                    className="inline-flex min-w-0 items-center gap-2 rounded-l-[9px] py-2 pl-3 pr-2 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[color:color-mix(in_srgb,var(--text-primary)_34%,transparent)]"
                    data-testid="agent-gui-composer-quote-trigger"
                  >
                    <MessageSquareText
                      aria-hidden
                      className="shrink-0 text-[var(--text-secondary)]"
                      size={16}
                      strokeWidth={2}
                    />
                    <span className="truncate">{quoteCountLabel}</span>
                  </button>
                </PopoverTrigger>
                <button
                  type="button"
                  className="mr-1.5 inline-flex size-5 shrink-0 items-center justify-center rounded-full text-[var(--text-secondary)] transition hover:bg-[var(--transparency-hover)] hover:text-[var(--text-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:color-mix(in_srgb,var(--text-primary)_34%,transparent)]"
                  aria-label={labels.removeMention}
                  title={labels.removeMention}
                  onClick={onRemoveQuotes}
                >
                  <X size={12} strokeWidth={2.4} aria-hidden />
                </button>
              </div>
              <PopoverContent
                side="top"
                align="start"
                aria-label={quoteCountLabel}
                className="max-h-[min(320px,var(--radix-popover-content-available-height))] w-auto max-w-[min(520px,calc(100vw-32px))] gap-2 overflow-y-auto overscroll-contain text-left text-sm leading-5 whitespace-pre-wrap [overflow-wrap:anywhere]"
                data-testid="agent-gui-composer-quote-preview"
                tabIndex={0}
              >
                {draftQuotes.map((quote) => (
                  <p key={quote.id}>“{quote.text.trim()}”</p>
                ))}
              </PopoverContent>
            </Popover>
          </div>
        ) : null}
        {draftImages.length > 0 ? (
          <div
            className="flex w-full max-w-full flex-wrap items-start gap-2"
            data-testid="agent-gui-composer-image-drafts"
          >
            {draftImages.map((image) => (
              <AgentComposerDraftImagePreview
                key={image.id}
                image={image}
                removeLabel={labels.removeMention}
                onRemove={removeDraftImage}
              />
            ))}
          </div>
        ) : null}
        {visibleDraftLargeTexts.length > 0 ? (
          <div
            className="flex max-w-[520px] flex-wrap gap-2"
            data-testid="agent-gui-composer-file-drafts"
          >
            {visibleDraftLargeTexts.map((item, index) => {
              const displayName = `${AGENT_COMPOSER_PASTED_TEXT_FILE_PREFIX}-${index + 1}.txt`;
              const preview = pastedTextPreview(item.text) || displayName;
              const attachmentTitle = translate(
                "agentHost.agentGui.pastedTextAttachmentTitle"
              );
              const attachmentStatus = item.uploadError
                ? translate("agentHost.agentGui.pastedTextAttachmentFailed")
                : attachmentTitle;
              const restoreLabel = translate(
                "agentHost.agentGui.pastedTextRestoreToComposer"
              );
              const canRestore = !item.uploading && item.text.trim() !== "";
              return (
                <div
                  key={item.id}
                  className={cn(
                    "group relative inline-flex max-w-full items-center gap-2 rounded-[10px] border border-[var(--line-1)] bg-[var(--background-fronted)] py-1.5 pl-1.5 pr-8 text-xs text-[var(--text-primary)]",
                    item.uploadError &&
                      "border-[color:color-mix(in_srgb,var(--danger)_55%,var(--line-1))]"
                  )}
                  data-testid="agent-gui-composer-large-text-draft"
                  data-uploading={item.uploading ? "true" : undefined}
                  data-upload-error={item.uploadError ? "true" : undefined}
                >
                  <ButtonBinding1
                    canRestore={canRestore}
                    restoreLabel={restoreLabel}
                    item={item}
                    preview={preview}
                    expandDraftLargeTextToPrompt={expandDraftLargeTextToPrompt}
                    attachmentStatus={attachmentStatus}
                  />
                  <ButtonBinding2
                    labels={labels}
                    removeDraftLargeText={removeDraftLargeText}
                    item={item}
                  />
                </div>
              );
            })}
          </div>
        ) : null}
      </div>
    );
  }
);

const ButtonBinding1 = observer(function ButtonBinding1({
  canRestore,
  restoreLabel,
  item,
  preview,
  expandDraftLargeTextToPrompt,
  attachmentStatus,
  ...slotProps
}: ButtonBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding1Props>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => expandDraftLargeTextToPrompt(item.id));
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className="flex min-w-0 items-center gap-2 rounded-[8px] text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:color-mix(in_srgb,var(--text-primary)_34%,transparent)] disabled:cursor-default"
        disabled={!canRestore}
        aria-label={restoreLabel}
        title={item.uploadError ?? (canRestore ? restoreLabel : preview)}
        onClick={onClickProp1}
      >
        <span className="flex size-9 shrink-0 items-center justify-center rounded-[8px] bg-[var(--transparency-hover)] text-[var(--text-secondary)]">
          {item.uploading ? (
            <Spinner
              size={16}
              strokeWidth={2.4}
              trackColor="var(--transparency-hover)"
              testId="agent-gui-composer-large-text-upload-spinner"
            />
          ) : (
            <FileText size={16} strokeWidth={2} aria-hidden />
          )}
        </span>
        <span className="flex min-w-0 flex-col">
          <span className="max-w-[200px] truncate font-medium text-[var(--text-primary)]">
            {preview}
          </span>
          <span className="max-w-[200px] truncate text-[11px] text-[var(--text-tertiary)]">
            {attachmentStatus}
          </span>
        </span>
      </button>
    </Slot.Root>
  );
});

const ButtonBinding2 = observer(function ButtonBinding2({
  labels,
  removeDraftLargeText,
  item,
  ...slotProps
}: {
  labels: { removeMention: string };
  removeDraftLargeText: NonNullable<Props>["onRemoveLargeText"];
  item: AgentComposerDraftLargeText;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    labels: { removeMention: string };
    removeDraftLargeText: NonNullable<Props>["onRemoveLargeText"];
    item: AgentComposerDraftLargeText;
  }
>) {
  const onClickProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => removeDraftLargeText(item.id));
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className="absolute right-1.5 top-1.5 inline-flex size-5 shrink-0 items-center justify-center rounded-full text-[var(--text-secondary)] transition hover:bg-[var(--transparency-hover)] hover:text-[var(--text-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:color-mix(in_srgb,var(--text-primary)_34%,transparent)]"
        aria-label={labels.removeMention}
        title={labels.removeMention}
        onClick={onClickProp2}
      >
        <X size={12} strokeWidth={2.4} aria-hidden />
      </button>
    </Slot.Root>
  );
});

type ButtonBinding1Props = {
  canRestore: boolean;
  restoreLabel: string;
  item: AgentComposerDraftLargeText;
  preview: string;
  expandDraftLargeTextToPrompt: NonNullable<Props>["onExpandLargeText"];
  attachmentStatus: string;
};
