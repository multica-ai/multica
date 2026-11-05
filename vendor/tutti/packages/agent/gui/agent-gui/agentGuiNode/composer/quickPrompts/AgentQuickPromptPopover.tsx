import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useId, useRef, useState } from "react";
import {
  Button,
  ConfirmationDialog,
  DialogFooter,
  Input,
  Popover,
  PopoverContent,
  PopoverTrigger,
  ScrollArea,
  Spinner,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@tutti-os/ui-system";
import {
  AddIcon,
  ArrowLeftIcon,
  ArrowRightIcon,
  CheckIcon,
  GripVerticalIcon,
  MessageSquareTextIcon,
  SearchIcon
} from "@tutti-os/ui-system/icons";
import { cn } from "../../../../app/renderer/lib/utils";
import styles from "../../AgentGUINode.styles";
import { AgentQuickPromptEditorDialog } from "./AgentQuickPromptEditorDialog";
import { AgentQuickPromptList } from "./AgentQuickPromptList";
import type { AgentQuickPromptTemplate } from "./agentQuickPromptLabels";
import type { AgentQuickPromptLibraryController } from "./useAgentQuickPromptLibrary";
import {
  type PrimaryPointerAction,
  usePrimaryPointerAction
} from "./usePrimaryPointerAction";
import { observer } from "mobx-react-lite";
export const AgentQuickPromptPopover = observer(AgentQuickPromptPopoverRender);
const RecommendedTemplateList = observer(RecommendedTemplateListRender);
const RecommendedTemplateButton = observer(RecommendedTemplateButtonRender);
const TemplateEntry = observer(TemplateEntryRender);
const PromptState = observer(PromptStateRender);

function AgentQuickPromptPopoverRender({
  controller,
  disabled
}: {
  controller: AgentQuickPromptLibraryController;
  disabled: boolean;
}): React.JSX.Element | null {
  const setIsSortingCallback = usePersistCallback<typeof setIsSorting>(
    (...args) => setIsSorting(...args)
  );

  const focusRowCallback = usePersistCallback<typeof focusRow>((...args) =>
    focusRow(...args)
  );

  const requestTemplateCallback = usePersistCallback<typeof requestTemplate>(
    (...args) => requestTemplate(...args)
  );

  const searchRef = useRef<HTMLInputElement | null>(null);
  const rowRefs = useRef(new Map<string, HTMLButtonElement>());
  const firstTemplateRef = useRef<HTMLButtonElement | null>(null);
  const preserveExternalFocusRef = useRef(false);
  const [view, setView] = useState<"prompts" | "templates">("prompts");
  let activeView = view;
  if (controller.insertionError && activeView !== "prompts") {
    activeView = "prompts";
    setView("prompts");
  }
  const [sortingState, setSortingState] = useState({
    isPopoverOpen: controller.isPopoverOpen,
    isSorting: false
  });
  let isSorting = sortingState.isSorting;
  if (sortingState.isPopoverOpen !== controller.isPopoverOpen) {
    isSorting = false;
    setSortingState({
      isPopoverOpen: controller.isPopoverOpen,
      isSorting: false
    });
  }
  const setIsSorting = (next: boolean): void => {
    setSortingState({
      isPopoverOpen: controller.isPopoverOpen,
      isSorting: next
    });
  };

  const titleId = useId();
  const { labels, snapshot } = controller;
  const templateEntryAction = usePrimaryPointerAction(() =>
    setView("templates")
  );
  const returnToPromptsAction = usePrimaryPointerAction(() =>
    setView("prompts")
  );
  const createAction = usePrimaryPointerAction(() => {
    if (controller.isInteractionLocked) return;
    preserveExternalFocusRef.current = true;
    controller.openCreate();
  });
  const deleteCancelAction = usePrimaryPointerAction(controller.closeDialog);
  const deleteConfirmAction = usePrimaryPointerAction(() => {
    void controller.submitDelete();
  });
  const startSortingAction = usePrimaryPointerAction(() => setIsSorting(true));
  const finishSortingAction = usePrimaryPointerAction(() =>
    setIsSorting(false)
  );

  if (!controller.capabilityAvailable) {
    return null;
  }

  const focusRow = (index: number): void => {
    const prompt = controller.filteredPrompts[index];
    if (prompt) rowRefs.current.get(prompt.id)?.focus();
  };

  const errorLabel =
    controller.mutationError === "conflict"
      ? labels.conflict
      : controller.mutationError === "generic"
        ? labels.mutationError
        : null;
  const requestTemplate = (template: AgentQuickPromptTemplate): void => {
    preserveExternalFocusRef.current = true;
    controller.openCreate(
      { title: template.title, content: template.content },
      {
        insertIntoComposerAfterSave: true,
        usagePromptType: "recommended_template"
      }
    );
  };

  const isTemplateView = activeView === "templates";
  const reorderDisabledMessage = resolveReorderDisabledMessage(controller);
  const startSortingButton = (
    <Button
      disabled={reorderDisabledMessage !== null}
      size="sm"
      type="button"
      variant="ghost"
      {...startSortingAction}
    >
      <GripVerticalIcon data-icon="inline-start" />
      {labels.startSorting}
    </Button>
  );

  return (
    <>
      <PopoverBinding1
        controller={controller}
        setView={setView}
        setIsSorting={setIsSortingCallback}
        labels={labels}
        disabled={disabled}
        titleId={titleId}
        isTemplateView={isTemplateView}
        firstTemplateRef={firstTemplateRef}
        rowRefs={rowRefs}
        searchRef={searchRef}
        preserveExternalFocusRef={preserveExternalFocusRef}
        returnToPromptsAction={returnToPromptsAction}
        isSorting={isSorting}
        finishSortingAction={finishSortingAction}
        reorderDisabledMessage={reorderDisabledMessage}
        startSortingButton={startSortingButton}
        createAction={createAction}
        focusRow={focusRowCallback}
        snapshot={snapshot}
        requestTemplate={requestTemplateCallback}
        templateEntryAction={templateEntryAction}
      />
      {controller.isEditorOpen ? (
        <AgentQuickPromptEditorDialog controller={controller} />
      ) : null}
      <ConfirmationDialogBinding3
        labels={labels}
        controller={controller}
        deleteCancelAction={deleteCancelAction}
        deleteConfirmAction={deleteConfirmAction}
        errorLabel={errorLabel}
      />
    </>
  );
}

function resolveReorderDisabledMessage(
  controller: AgentQuickPromptLibraryController
): string | null {
  const { labels, snapshot } = controller;
  if (!controller.reorderCapabilityAvailable) {
    return labels.reorderDisabledUnsupported;
  }
  if (snapshot.status === "idle" || snapshot.status === "loading") {
    return labels.loading;
  }
  if (snapshot.status === "error" && snapshot.prompts.length === 0) {
    return labels.loadError;
  }
  if (controller.isInteractionLocked) {
    return labels.reorderDisabledPending;
  }
  if (controller.searchQuery.trim()) {
    return labels.reorderDisabledSearch;
  }
  if (controller.filteredPrompts.length < 2) {
    return labels.reorderDisabledMinimum;
  }
  return null;
}

function RecommendedTemplateListRender({
  disabled,
  firstTemplateRef,
  labels,
  onSelect
}: RecommendedTemplateListProps): React.JSX.Element {
  return (
    <section className="flex flex-col gap-1 px-1 pt-2">
      <div className="px-2 pb-1">
        <h3 className="text-[13px] font-medium text-[var(--text-primary)]">
          {labels.recommendedTemplatesTitle}
        </h3>
        <p className="pt-0.5 text-[12px] leading-[1.35] text-[var(--text-secondary)]">
          {labels.recommendedTemplatesDescription}
        </p>
      </div>
      {labels.recommendedTemplates.map((template, index) => (
        <RecommendedTemplateButton
          key={template.id}
          disabled={disabled}
          labels={labels}
          selectionRef={index === 0 ? firstTemplateRef : undefined}
          template={template}
          onSelect={onSelect}
        />
      ))}
    </section>
  );
}

function RecommendedTemplateButtonRender({
  disabled,
  labels,
  onSelect,
  selectionRef,
  template
}: RecommendedTemplateButtonProps): React.JSX.Element {
  const selectionAction = usePrimaryPointerAction(() => {
    if (!disabled) onSelect(template);
  });

  return (
    <Button
      {...selectionAction}
      ref={selectionRef}
      className="h-auto w-full justify-between px-2 py-2 text-left whitespace-normal"
      disabled={disabled}
      type="button"
      variant="ghost"
    >
      <span className="flex min-w-0 flex-col items-start gap-0.5">
        <span className="w-full truncate font-medium text-[var(--text-primary)]">
          {template.title}
        </span>
        <span className="line-clamp-2 w-full text-[12px] leading-[1.35] text-[var(--text-secondary)]">
          {template.description}
        </span>
      </span>
      <span className="flex shrink-0 items-center gap-1 text-[12px] text-[var(--text-secondary)]">
        {labels.useTemplate}
        <ArrowRightIcon data-icon="inline-end" />
      </span>
    </Button>
  );
}

function TemplateEntryRender({
  action,
  disabled,
  label
}: {
  action: PrimaryPointerAction;
  disabled: boolean;
  label: string;
}): React.JSX.Element {
  return (
    <Button
      {...action}
      className="mt-1 h-auto w-full justify-between px-2 py-2 text-left whitespace-normal"
      disabled={disabled}
      type="button"
      variant="ghost"
    >
      <span className="font-medium">{label}</span>
      <ArrowRightIcon data-icon="inline-end" />
    </Button>
  );
}

function PromptStateRender({
  action,
  icon,
  label
}: {
  action?: React.ReactNode;
  icon?: React.ReactNode;
  label: string;
}): React.JSX.Element {
  return (
    <div className="flex min-h-[180px] flex-col items-center justify-center gap-2 px-4 text-center text-[13px] text-[var(--text-secondary)]">
      {icon}
      <span>{label}</span>
      {action}
    </div>
  );
}

const InputBinding1 = observer(function InputBinding1({
  searchRef,
  labels,
  controller,
  focusRow,
  ...slotProps
}: InputBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof InputBinding1Props>) {
  const onChangeProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Input>["onChange"]>
  >((event) => controller.setSearchQuery(event.target.value));
  const onKeyDownProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Input>["onKeyDown"]>
  >((event) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      focusRow(0);
    }
  });
  return (
    <Slot.Root {...slotProps}>
      <Input
        ref={searchRef}
        aria-label={labels.searchPlaceholder}
        className="pl-8"
        disabled={controller.isInteractionLocked}
        placeholder={labels.searchPlaceholder}
        value={controller.searchQuery}
        onChange={onChangeProp1}
        onKeyDown={onKeyDownProp2}
      />
    </Slot.Root>
  );
});

const AgentQuickPromptListBinding2 = observer(
  function AgentQuickPromptListBinding2({
    controller,
    isSorting,
    rowRefs,
    focusRow,
    preserveExternalFocusRef
  }: AgentQuickPromptListBinding2Props) {
    const onDeleteProp3 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof AgentQuickPromptList>["onDelete"]>
    >((prompt) => {
      preserveExternalFocusRef.current = true;
      controller.deletePrompt(prompt);
    });
    const onEditProp4 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof AgentQuickPromptList>["onEdit"]>
    >((prompt) => {
      preserveExternalFocusRef.current = true;
      controller.openEdit(prompt);
    });
    const onSelectProp5 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof AgentQuickPromptList>["onSelect"]>
    >((prompt) => {
      if (isSorting) return;
      preserveExternalFocusRef.current = true;
      controller.selectPrompt(prompt);
    });
    return (
      <AgentQuickPromptList
        controller={controller}
        isSorting={isSorting}
        rowRefs={rowRefs}
        onFocusRow={focusRow}
        onDelete={onDeleteProp3}
        onEdit={onEditProp4}
        onSelect={onSelectProp5}
      />
    );
  }
);

const ConfirmationDialogBinding3 = observer(
  function ConfirmationDialogBinding3({
    labels,
    controller,
    deleteCancelAction,
    deleteConfirmAction,
    errorLabel
  }: ConfirmationDialogBinding3Props) {
    const onOpenChangeProp6 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof ConfirmationDialog>["onOpenChange"]
      >
    >((open) => {
      if (!open && !controller.isDeleting) controller.closeDialog();
    });
    return (
      <ConfirmationDialog
        cancelLabel={labels.cancel}
        confirmBusy={controller.isDeleting}
        confirmLabel={
          controller.isDeleting ? labels.deleting : labels.deleteConfirm
        }
        description={
          controller.promptToDelete
            ? labels.deleteDescription(controller.promptToDelete.title)
            : undefined
        }
        footer={
          <DialogFooter>
            <Button
              {...deleteCancelAction}
              disabled={controller.isDeleting}
              size="dialog"
              type="button"
              variant="ghost"
            >
              {labels.cancel}
            </Button>
            <Button
              {...deleteConfirmAction}
              disabled={controller.isDeleting}
              size="dialog"
              type="button"
              variant="destructive"
            >
              {controller.isDeleting ? labels.deleting : labels.deleteConfirm}
            </Button>
          </DialogFooter>
        }
        onOpenChange={onOpenChangeProp6}
        open={controller.mode === "delete"}
        title={labels.deleteTitle}
        tone="destructive"
      >
        {errorLabel ? (
          <p className="text-[12px] text-[var(--state-danger)]" role="alert">
            {errorLabel}
          </p>
        ) : null}
      </ConfirmationDialog>
    );
  }
);

const PopoverContentBinding1 = observer(function PopoverContentBinding1({
  titleId,
  isTemplateView,
  firstTemplateRef,
  controller,
  rowRefs,
  searchRef,
  preserveExternalFocusRef,
  labels,
  returnToPromptsAction,
  isSorting,
  finishSortingAction,
  reorderDisabledMessage,
  startSortingButton,
  createAction,
  focusRow,
  snapshot,
  requestTemplate,
  templateEntryAction
}: PopoverContentBinding1Props) {
  const onOpenAutoFocusProp7 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof PopoverContent>["onOpenAutoFocus"]>
  >((event) => {
    event.preventDefault();
    window.requestAnimationFrame(() => {
      if (isTemplateView) {
        firstTemplateRef.current?.focus();
        return;
      }
      const selectedId = controller.selectedPrompt?.id;
      const selectedRow = selectedId ? rowRefs.current.get(selectedId) : null;
      (selectedRow ?? searchRef.current)?.focus();
    });
  });
  const onCloseAutoFocusProp8 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof PopoverContent>["onCloseAutoFocus"]>
  >((event) => {
    if (preserveExternalFocusRef.current) event.preventDefault();
    preserveExternalFocusRef.current = false;
  });
  return (
    <PopoverContent
      aria-labelledby={titleId}
      align="start"
      className="h-[420px] max-h-[var(--radix-popover-content-available-height)] w-[400px] gap-0 overflow-hidden p-0"
      side="top"
      sideOffset={8}
      onOpenAutoFocus={onOpenAutoFocusProp7}
      onCloseAutoFocus={onCloseAutoFocusProp8}
    >
      <div className="flex shrink-0 items-center justify-between gap-2 border-b border-[var(--border-1)] px-3 py-2.5">
        <h2
          id={titleId}
          className="text-[14px] font-medium text-[var(--text-primary)]"
        >
          {isTemplateView ? labels.recommendedTemplatesTitle : labels.title}
        </h2>
        {isTemplateView ? (
          <Button
            size="sm"
            type="button"
            variant="ghost"
            {...returnToPromptsAction}
          >
            <ArrowLeftIcon data-icon="inline-start" />
            {labels.returnToPrompts}
          </Button>
        ) : (
          <div className="flex items-center gap-1">
            {isSorting ? (
              <Button
                disabled={controller.isInteractionLocked}
                size="sm"
                type="button"
                variant="ghost"
                {...finishSortingAction}
              >
                <CheckIcon data-icon="inline-start" />
                {labels.finishSorting}
              </Button>
            ) : (
              <>
                {reorderDisabledMessage ? (
                  <TooltipProvider delayDuration={120}>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span
                          aria-label={reorderDisabledMessage}
                          className="inline-flex"
                          tabIndex={0}
                        >
                          {startSortingButton}
                        </span>
                      </TooltipTrigger>
                      <TooltipContent side="top">
                        {reorderDisabledMessage}
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                ) : (
                  startSortingButton
                )}
                <Button
                  {...createAction}
                  disabled={controller.isInteractionLocked}
                  size="sm"
                  type="button"
                  variant="ghost"
                >
                  <AddIcon data-icon="inline-start" />
                  {labels.add}
                </Button>
              </>
            )}
          </div>
        )}
      </div>
      {!isTemplateView && !isSorting ? (
        <div className="relative shrink-0 px-3 py-2.5">
          <SearchIcon
            aria-hidden
            className="pointer-events-none absolute top-1/2 left-5 size-3.5 -translate-y-1/2 text-[var(--text-tertiary)]"
          />
          <InputBinding1
            searchRef={searchRef}
            labels={labels}
            controller={controller}
            focusRow={focusRow}
          />
        </div>
      ) : null}
      {!isTemplateView &&
      snapshot.status === "error" &&
      snapshot.prompts.length > 0 ? (
        <div
          className="flex shrink-0 items-center justify-between gap-2 px-3 pb-2 text-[12px] text-[var(--state-danger)]"
          role="alert"
        >
          <span>{labels.loadError}</span>
          <Button
            size="xs"
            type="button"
            variant="ghost"
            onClick={controller.retry}
          >
            {labels.retry}
          </Button>
        </div>
      ) : null}
      {!isTemplateView && controller.reorderError ? (
        <div
          className="flex shrink-0 items-center justify-between gap-2 px-3 pb-2 text-[12px] text-[var(--state-danger)]"
          role="alert"
        >
          <span>
            {controller.reorderError === "conflict"
              ? labels.reorderConflict
              : labels.reorderError}
          </span>
          <Button
            size="xs"
            type="button"
            variant="ghost"
            onClick={controller.retry}
          >
            {labels.retry}
          </Button>
        </div>
      ) : null}
      {!isTemplateView && controller.insertionError ? (
        <div
          className="shrink-0 px-3 pb-2 text-[12px] text-[var(--state-danger)]"
          role="alert"
        >
          {labels.insertionError}
        </div>
      ) : null}
      <ScrollArea
        className="min-h-0 flex-1"
        viewportClassName={cn("px-2 pb-2", isSorting && "pt-2")}
        viewportTestId="agent-quick-prompt-scroll-viewport"
      >
        {isTemplateView ? (
          <RecommendedTemplateList
            disabled={controller.isInteractionLocked}
            firstTemplateRef={firstTemplateRef}
            labels={labels}
            onSelect={requestTemplate}
          />
        ) : snapshot.status === "loading" && snapshot.prompts.length === 0 ? (
          <PromptState icon={<Spinner size={16} />} label={labels.loading} />
        ) : snapshot.status === "error" && snapshot.prompts.length === 0 ? (
          <PromptState
            label={labels.loadError}
            action={
              <Button
                disabled={controller.isInteractionLocked}
                size="sm"
                type="button"
                variant="ghost"
                onClick={controller.retry}
              >
                {labels.retry}
              </Button>
            }
          />
        ) : snapshot.prompts.length === 0 && !controller.searchQuery.trim() ? (
          <RecommendedTemplateList
            disabled={controller.isInteractionLocked}
            firstTemplateRef={firstTemplateRef}
            labels={labels}
            onSelect={requestTemplate}
          />
        ) : controller.filteredPrompts.length === 0 ? (
          <PromptState label={labels.noResults} />
        ) : (
          <div className="flex flex-col gap-0.5">
            <AgentQuickPromptListBinding2
              controller={controller}
              isSorting={isSorting}
              rowRefs={rowRefs}
              focusRow={focusRow}
              preserveExternalFocusRef={preserveExternalFocusRef}
            />
            {!isSorting ? (
              <TemplateEntry
                label={labels.createFromTemplate}
                action={templateEntryAction}
                disabled={controller.isInteractionLocked}
              />
            ) : null}
          </div>
        )}
      </ScrollArea>
    </PopoverContent>
  );
});

const PopoverBinding1 = observer(function PopoverBinding1({
  controller,
  setView,
  setIsSorting,
  labels,
  disabled,
  titleId,
  isTemplateView,
  firstTemplateRef,
  rowRefs,
  searchRef,
  preserveExternalFocusRef,
  returnToPromptsAction,
  isSorting,
  finishSortingAction,
  reorderDisabledMessage,
  startSortingButton,
  createAction,
  focusRow,
  snapshot,
  requestTemplate,
  templateEntryAction
}: PopoverBinding1Props) {
  const onOpenChangeProp9 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Popover>["onOpenChange"]>
  >((open) => {
    if (!open) {
      setView("prompts");
      setIsSorting(false);
    }
    controller.setPopoverOpen(open);
  });
  return (
    <Popover
      modal={false}
      open={controller.isPopoverOpen}
      onOpenChange={onOpenChangeProp9}
    >
      <TooltipProvider delayDuration={120}>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="inline-flex shrink-0">
              <PopoverTrigger asChild>
                <Button
                  aria-label={labels.triggerTooltip}
                  className={cn(
                    styles.composerMenuTrigger,
                    "w-auto max-w-[180px] !gap-1.5 focus-visible:!outline-2 focus-visible:!outline-offset-2 focus-visible:!outline-[var(--border-focus)]"
                  )}
                  disabled={disabled}
                  size="sm"
                  type="button"
                  variant="chrome"
                >
                  <MessageSquareTextIcon
                    className="size-4"
                    data-icon="inline-start"
                  />
                  <span className="hidden min-[900px]:inline">
                    {labels.trigger}
                  </span>
                </Button>
              </PopoverTrigger>
            </span>
          </TooltipTrigger>
          <TooltipContent side="top">{labels.triggerTooltip}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <PopoverContentBinding1
        titleId={titleId}
        isTemplateView={isTemplateView}
        firstTemplateRef={firstTemplateRef}
        controller={controller}
        rowRefs={rowRefs}
        searchRef={searchRef}
        preserveExternalFocusRef={preserveExternalFocusRef}
        labels={labels}
        returnToPromptsAction={returnToPromptsAction}
        isSorting={isSorting}
        finishSortingAction={finishSortingAction}
        reorderDisabledMessage={reorderDisabledMessage}
        startSortingButton={startSortingButton}
        createAction={createAction}
        focusRow={focusRow}
        snapshot={snapshot}
        requestTemplate={requestTemplate}
        templateEntryAction={templateEntryAction}
      />
    </Popover>
  );
});

type RecommendedTemplateListProps = {
  disabled: boolean;
  firstTemplateRef: React.RefObject<HTMLButtonElement | null>;
  labels: AgentQuickPromptLibraryController["labels"];
  onSelect: (template: AgentQuickPromptTemplate) => void;
};

type RecommendedTemplateButtonProps = {
  disabled: boolean;
  labels: AgentQuickPromptLibraryController["labels"];
  onSelect: (template: AgentQuickPromptTemplate) => void;
  selectionRef?: React.RefObject<HTMLButtonElement | null>;
  template: AgentQuickPromptTemplate;
};

type InputBinding1Props = {
  searchRef: NonNullable<{
    titleId: string;
    isTemplateView: boolean;
    firstTemplateRef: import("react").RefObject<HTMLButtonElement | null>;
    controller: AgentQuickPromptLibraryController;
    rowRefs: import("react").RefObject<Map<string, HTMLButtonElement>>;
    searchRef: import("react").RefObject<HTMLInputElement | null>;
    preserveExternalFocusRef: import("react").RefObject<boolean>;
    labels: import("./agentQuickPromptLabels").AgentQuickPromptLabels;
    returnToPromptsAction: PrimaryPointerAction;
    isSorting: boolean;
    finishSortingAction: PrimaryPointerAction;
    reorderDisabledMessage: string | null;
    startSortingButton: import("react").JSX.Element;
    createAction: PrimaryPointerAction;
    focusRow: (index: number) => void;
    snapshot: import("../../../../host/agentHostApi").AgentHostQuickPromptSnapshot;
    requestTemplate: (template: AgentQuickPromptTemplate) => void;
    templateEntryAction: PrimaryPointerAction;
  }>["searchRef"];
  labels: import("./agentQuickPromptLabels").AgentQuickPromptLabels;
  controller: AgentQuickPromptLibraryController;
  focusRow: (index: number) => void;
};

type AgentQuickPromptListBinding2Props = {
  controller: AgentQuickPromptLibraryController;
  isSorting: boolean;
  rowRefs: NonNullable<{
    titleId: string;
    isTemplateView: boolean;
    firstTemplateRef: import("react").RefObject<HTMLButtonElement | null>;
    controller: AgentQuickPromptLibraryController;
    rowRefs: import("react").RefObject<Map<string, HTMLButtonElement>>;
    searchRef: import("react").RefObject<HTMLInputElement | null>;
    preserveExternalFocusRef: import("react").RefObject<boolean>;
    labels: import("./agentQuickPromptLabels").AgentQuickPromptLabels;
    returnToPromptsAction: PrimaryPointerAction;
    isSorting: boolean;
    finishSortingAction: PrimaryPointerAction;
    reorderDisabledMessage: string | null;
    startSortingButton: import("react").JSX.Element;
    createAction: PrimaryPointerAction;
    focusRow: (index: number) => void;
    snapshot: import("../../../../host/agentHostApi").AgentHostQuickPromptSnapshot;
    requestTemplate: (template: AgentQuickPromptTemplate) => void;
    templateEntryAction: PrimaryPointerAction;
  }>["rowRefs"];
  focusRow: (index: number) => void;
  preserveExternalFocusRef: NonNullable<{
    titleId: string;
    isTemplateView: boolean;
    firstTemplateRef: import("react").RefObject<HTMLButtonElement | null>;
    controller: AgentQuickPromptLibraryController;
    rowRefs: import("react").RefObject<Map<string, HTMLButtonElement>>;
    searchRef: import("react").RefObject<HTMLInputElement | null>;
    preserveExternalFocusRef: import("react").RefObject<boolean>;
    labels: import("./agentQuickPromptLabels").AgentQuickPromptLabels;
    returnToPromptsAction: PrimaryPointerAction;
    isSorting: boolean;
    finishSortingAction: PrimaryPointerAction;
    reorderDisabledMessage: string | null;
    startSortingButton: import("react").JSX.Element;
    createAction: PrimaryPointerAction;
    focusRow: (index: number) => void;
    snapshot: import("../../../../host/agentHostApi").AgentHostQuickPromptSnapshot;
    requestTemplate: (template: AgentQuickPromptTemplate) => void;
    templateEntryAction: PrimaryPointerAction;
  }>["preserveExternalFocusRef"];
};

type ConfirmationDialogBinding3Props = {
  labels: NonNullable<
    NonNullable<{
      controller: AgentQuickPromptLibraryController;
      disabled: boolean;
    }>["controller"]
  >["labels"];
  controller: NonNullable<{
    controller: AgentQuickPromptLibraryController;
    disabled: boolean;
  }>["controller"];
  deleteCancelAction: PrimaryPointerAction;
  deleteConfirmAction: PrimaryPointerAction;
  errorLabel: string | null;
};

type PopoverContentBinding1Props = {
  titleId: string;
  isTemplateView: boolean;
  firstTemplateRef: NonNullable<{
    controller: AgentQuickPromptLibraryController;
    setView: import("react").Dispatch<
      import("react").SetStateAction<"prompts" | "templates">
    >;
    setIsSorting: (next: boolean) => void;
    labels: import("./agentQuickPromptLabels").AgentQuickPromptLabels;
    disabled: boolean;
    titleId: string;
    isTemplateView: boolean;
    firstTemplateRef: import("react").RefObject<HTMLButtonElement | null>;
    rowRefs: import("react").RefObject<Map<string, HTMLButtonElement>>;
    searchRef: import("react").RefObject<HTMLInputElement | null>;
    preserveExternalFocusRef: import("react").RefObject<boolean>;
    returnToPromptsAction: PrimaryPointerAction;
    isSorting: boolean;
    finishSortingAction: PrimaryPointerAction;
    reorderDisabledMessage: string | null;
    startSortingButton: import("react").JSX.Element;
    createAction: PrimaryPointerAction;
    focusRow: (index: number) => void;
    snapshot: import("../../../../host/agentHostApi").AgentHostQuickPromptSnapshot;
    requestTemplate: (template: AgentQuickPromptTemplate) => void;
    templateEntryAction: PrimaryPointerAction;
  }>["firstTemplateRef"];
  controller: AgentQuickPromptLibraryController;
  rowRefs: NonNullable<{
    controller: AgentQuickPromptLibraryController;
    setView: import("react").Dispatch<
      import("react").SetStateAction<"prompts" | "templates">
    >;
    setIsSorting: (next: boolean) => void;
    labels: import("./agentQuickPromptLabels").AgentQuickPromptLabels;
    disabled: boolean;
    titleId: string;
    isTemplateView: boolean;
    firstTemplateRef: import("react").RefObject<HTMLButtonElement | null>;
    rowRefs: import("react").RefObject<Map<string, HTMLButtonElement>>;
    searchRef: import("react").RefObject<HTMLInputElement | null>;
    preserveExternalFocusRef: import("react").RefObject<boolean>;
    returnToPromptsAction: PrimaryPointerAction;
    isSorting: boolean;
    finishSortingAction: PrimaryPointerAction;
    reorderDisabledMessage: string | null;
    startSortingButton: import("react").JSX.Element;
    createAction: PrimaryPointerAction;
    focusRow: (index: number) => void;
    snapshot: import("../../../../host/agentHostApi").AgentHostQuickPromptSnapshot;
    requestTemplate: (template: AgentQuickPromptTemplate) => void;
    templateEntryAction: PrimaryPointerAction;
  }>["rowRefs"];
  searchRef: NonNullable<{
    controller: AgentQuickPromptLibraryController;
    setView: import("react").Dispatch<
      import("react").SetStateAction<"prompts" | "templates">
    >;
    setIsSorting: (next: boolean) => void;
    labels: import("./agentQuickPromptLabels").AgentQuickPromptLabels;
    disabled: boolean;
    titleId: string;
    isTemplateView: boolean;
    firstTemplateRef: import("react").RefObject<HTMLButtonElement | null>;
    rowRefs: import("react").RefObject<Map<string, HTMLButtonElement>>;
    searchRef: import("react").RefObject<HTMLInputElement | null>;
    preserveExternalFocusRef: import("react").RefObject<boolean>;
    returnToPromptsAction: PrimaryPointerAction;
    isSorting: boolean;
    finishSortingAction: PrimaryPointerAction;
    reorderDisabledMessage: string | null;
    startSortingButton: import("react").JSX.Element;
    createAction: PrimaryPointerAction;
    focusRow: (index: number) => void;
    snapshot: import("../../../../host/agentHostApi").AgentHostQuickPromptSnapshot;
    requestTemplate: (template: AgentQuickPromptTemplate) => void;
    templateEntryAction: PrimaryPointerAction;
  }>["searchRef"];
  preserveExternalFocusRef: NonNullable<{
    controller: AgentQuickPromptLibraryController;
    setView: import("react").Dispatch<
      import("react").SetStateAction<"prompts" | "templates">
    >;
    setIsSorting: (next: boolean) => void;
    labels: import("./agentQuickPromptLabels").AgentQuickPromptLabels;
    disabled: boolean;
    titleId: string;
    isTemplateView: boolean;
    firstTemplateRef: import("react").RefObject<HTMLButtonElement | null>;
    rowRefs: import("react").RefObject<Map<string, HTMLButtonElement>>;
    searchRef: import("react").RefObject<HTMLInputElement | null>;
    preserveExternalFocusRef: import("react").RefObject<boolean>;
    returnToPromptsAction: PrimaryPointerAction;
    isSorting: boolean;
    finishSortingAction: PrimaryPointerAction;
    reorderDisabledMessage: string | null;
    startSortingButton: import("react").JSX.Element;
    createAction: PrimaryPointerAction;
    focusRow: (index: number) => void;
    snapshot: import("../../../../host/agentHostApi").AgentHostQuickPromptSnapshot;
    requestTemplate: (template: AgentQuickPromptTemplate) => void;
    templateEntryAction: PrimaryPointerAction;
  }>["preserveExternalFocusRef"];
  labels: import("./agentQuickPromptLabels").AgentQuickPromptLabels;
  returnToPromptsAction: PrimaryPointerAction;
  isSorting: boolean;
  finishSortingAction: PrimaryPointerAction;
  reorderDisabledMessage: string | null;
  startSortingButton: NonNullable<{
    controller: AgentQuickPromptLibraryController;
    setView: import("react").Dispatch<
      import("react").SetStateAction<"prompts" | "templates">
    >;
    setIsSorting: (next: boolean) => void;
    labels: import("./agentQuickPromptLabels").AgentQuickPromptLabels;
    disabled: boolean;
    titleId: string;
    isTemplateView: boolean;
    firstTemplateRef: import("react").RefObject<HTMLButtonElement | null>;
    rowRefs: import("react").RefObject<Map<string, HTMLButtonElement>>;
    searchRef: import("react").RefObject<HTMLInputElement | null>;
    preserveExternalFocusRef: import("react").RefObject<boolean>;
    returnToPromptsAction: PrimaryPointerAction;
    isSorting: boolean;
    finishSortingAction: PrimaryPointerAction;
    reorderDisabledMessage: string | null;
    startSortingButton: import("react").JSX.Element;
    createAction: PrimaryPointerAction;
    focusRow: (index: number) => void;
    snapshot: import("../../../../host/agentHostApi").AgentHostQuickPromptSnapshot;
    requestTemplate: (template: AgentQuickPromptTemplate) => void;
    templateEntryAction: PrimaryPointerAction;
  }>["startSortingButton"];
  createAction: PrimaryPointerAction;
  focusRow: (index: number) => void;
  snapshot: import("../../../../host/agentHostApi").AgentHostQuickPromptSnapshot;
  requestTemplate: (template: AgentQuickPromptTemplate) => void;
  templateEntryAction: PrimaryPointerAction;
};

type PopoverBinding1Props = {
  controller: NonNullable<{
    controller: AgentQuickPromptLibraryController;
    disabled: boolean;
  }>["controller"];
  setView: import("react").Dispatch<
    import("react").SetStateAction<"prompts" | "templates">
  >;
  setIsSorting: (next: boolean) => void;
  labels: NonNullable<
    NonNullable<{
      controller: AgentQuickPromptLibraryController;
      disabled: boolean;
    }>["controller"]
  >["labels"];
  disabled: NonNullable<{
    controller: AgentQuickPromptLibraryController;
    disabled: boolean;
  }>["disabled"];
  titleId: string;
  isTemplateView: boolean;
  firstTemplateRef: import("react").RefObject<HTMLButtonElement | null>;
  rowRefs: import("react").RefObject<Map<string, HTMLButtonElement>>;
  searchRef: import("react").RefObject<HTMLInputElement | null>;
  preserveExternalFocusRef: import("react").RefObject<boolean>;
  returnToPromptsAction: PrimaryPointerAction;
  isSorting: boolean;
  finishSortingAction: PrimaryPointerAction;
  reorderDisabledMessage: string | null;
  startSortingButton: import("react").JSX.Element;
  createAction: PrimaryPointerAction;
  focusRow: (index: number) => void;
  snapshot: NonNullable<
    NonNullable<{
      controller: AgentQuickPromptLibraryController;
      disabled: boolean;
    }>["controller"]
  >["snapshot"];
  requestTemplate: (template: AgentQuickPromptTemplate) => void;
  templateEntryAction: PrimaryPointerAction;
};
