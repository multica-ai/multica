import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { type PointerEvent, useRef, useState } from "react";
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Spinner,
  Textarea
} from "@tutti-os/ui-system";
import type { AgentHostQuickPrompt } from "../../../../host/agentHostApi";
import type {
  AgentQuickPromptDraft,
  AgentQuickPromptLibraryController
} from "./useAgentQuickPromptLibrary";
import { observer } from "mobx-react-lite";
export const AgentQuickPromptEditorDialog = observer(
  AgentQuickPromptEditorDialogRender
);

const MAX_TITLE_CODE_POINTS = 80;
const MAX_CONTENT_BYTES = 32 * 1024;

function AgentQuickPromptEditorDialogRender({
  controller
}: {
  controller: AgentQuickPromptLibraryController;
}): React.JSX.Element {
  const { labels } = controller;
  const source = controller.mode === "edit" ? controller.selectedPrompt : null;
  const [draft, setDraft] = useState<AgentQuickPromptDraft>(
    () => controller.initialDraft ?? promptDraft(source)
  );
  const [validationError, setValidationError] = useState<string | null>(null);
  const cancelRequestedOnPointerDownRef = useRef(false);

  const submit = async (): Promise<void> => {
    const error = validateDraft(draft, labels);
    if (error) {
      setValidationError(error);
      return;
    }
    setValidationError(null);
    await controller.saveDraft({
      title: draft.title.trim(),
      content: draft.content
    });
  };
  const mutationError =
    controller.mutationError === "conflict"
      ? labels.conflict
      : controller.mutationError === "generic"
        ? labels.mutationError
        : null;

  const onOpenChangeProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Dialog>["onOpenChange"]>
  >((open) => {
    if (!open && !controller.isSaving) {
      controller.closeDialog();
    }
  });
  const onKeyDownCaptureProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof DialogContent>["onKeyDownCapture"]>
  >((event) => {
    // The dialog is portalled but still participates in the Composer's
    // React event tree. Keep Enter inside the form so a keyboard save
    // cannot reach the Composer submit shortcut after the dialog closes.
    if (event.key === "Enter") event.stopPropagation();
  });
  const onEscapeKeyDownProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof DialogContent>["onEscapeKeyDown"]>
  >((event) => {
    if (controller.isSaving) event.preventDefault();
  });
  const onInteractOutsideProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof DialogContent>["onInteractOutside"]>
  >((event) => {
    if (controller.isSaving) event.preventDefault();
  });
  const onSubmitProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<"form">["onSubmit"]>
  >((event) => {
    event.preventDefault();
    void submit();
  });
  const onChangeProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Input>["onChange"]>
  >((event) =>
    setDraft((current) => ({
      ...current,
      title: event.target.value
    }))
  );
  const onChangeProp7 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Textarea>["onChange"]>
  >((event) =>
    setDraft((current) => ({
      ...current,
      content: event.target.value
    }))
  );
  const onPointerDownProp8 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onPointerDown"]>
  >((event: PointerEvent<HTMLButtonElement>) => {
    if (event.button !== 0) return;
    cancelRequestedOnPointerDownRef.current = true;
    event.preventDefault();
    controller.closeDialog();
  });
  const onClickProp9 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    if (cancelRequestedOnPointerDownRef.current) {
      cancelRequestedOnPointerDownRef.current = false;
      return;
    }
    controller.closeDialog();
  });
  return (
    <Dialog open={controller.isEditorOpen} onOpenChange={onOpenChangeProp1}>
      <DialogContent
        className="sm:max-w-[560px]"
        showCloseButton={false}
        onKeyDownCapture={onKeyDownCaptureProp2}
        onEscapeKeyDown={onEscapeKeyDownProp3}
        onInteractOutside={onInteractOutsideProp4}
      >
        <DialogHeader>
          <DialogTitle>
            {source ? labels.editTitle : labels.createTitle}
          </DialogTitle>
          <DialogDescription className="sr-only">
            {labels.contentPlaceholder}
          </DialogDescription>
        </DialogHeader>
        <form className="flex min-h-0 flex-col gap-3" onSubmit={onSubmitProp5}>
          <label className="flex flex-col gap-1.5 text-[13px] text-[var(--text-secondary)]">
            <span>{labels.titleLabel}</span>
            <Input
              autoFocus
              aria-invalid={Boolean(validationError)}
              disabled={controller.isSaving}
              maxLength={MAX_TITLE_CODE_POINTS * 2}
              placeholder={labels.titlePlaceholder}
              value={draft.title}
              onChange={onChangeProp6}
            />
          </label>
          <label className="flex min-h-0 flex-col gap-1.5 text-[13px] text-[var(--text-secondary)]">
            <span>{labels.contentLabel}</span>
            <Textarea
              aria-invalid={Boolean(validationError)}
              className="min-h-[128px] resize-y"
              disabled={controller.isSaving}
              placeholder={labels.contentPlaceholder}
              value={draft.content}
              onChange={onChangeProp7}
            />
          </label>
          {validationError || mutationError ? (
            <p className="text-[12px] text-[var(--state-danger)]" role="alert">
              {validationError ?? mutationError}
            </p>
          ) : null}
          <DialogFooter>
            <Button
              disabled={controller.isSaving}
              size="dialog"
              type="button"
              variant="ghost"
              onPointerDown={onPointerDownProp8}
              onClick={onClickProp9}
            >
              {labels.cancel}
            </Button>
            <Button disabled={controller.isSaving} size="dialog" type="submit">
              {controller.isSaving ? <Spinner size={14} /> : null}
              {controller.isSaving ? labels.saving : labels.save}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function promptDraft(
  source: AgentHostQuickPrompt | null
): AgentQuickPromptDraft {
  return source
    ? { title: source.title, content: source.content }
    : { title: "", content: "" };
}

function validateDraft(
  draft: AgentQuickPromptDraft,
  labels: AgentQuickPromptLibraryController["labels"]
): string | null {
  if (!draft.title.trim() || !draft.content.trim()) {
    return labels.required;
  }
  if (Array.from(draft.title.trim()).length > MAX_TITLE_CODE_POINTS) {
    return labels.titleTooLong;
  }
  if (new TextEncoder().encode(draft.content).byteLength > MAX_CONTENT_BYTES) {
    return labels.contentTooLarge;
  }
  return null;
}
