import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useMemo, useRef } from "react";
import { Sortable, SortableContent, SortableItem } from "@tutti-os/ui-system";
import type { AgentHostQuickPrompt } from "../../../../host/agentHostApi";
import { AgentQuickPromptRow } from "./AgentQuickPromptRow";
import type { AgentQuickPromptLibraryController } from "./useAgentQuickPromptLibrary";
import { observer } from "mobx-react-lite";
export const AgentQuickPromptList = observer(AgentQuickPromptListRender);

function AgentQuickPromptListRender({
  controller,
  isSorting,
  onDelete,
  onEdit,
  onFocusRow,
  onSelect,
  rowRefs
}: AgentQuickPromptListProps): React.JSX.Element {
  const handleRefs = useRef(new Map<string, HTMLButtonElement>());
  const { filteredPrompts, labels, snapshot } = controller;
  const promptsById = useMemo(
    () => new Map(filteredPrompts.map((prompt) => [prompt.id, prompt])),
    [filteredPrompts]
  );
  const accessibility = useMemo(
    () => ({
      announcements: {
        onDragStart({ active }: { active: { id: string | number } }) {
          const index = filteredPrompts.findIndex(
            (prompt) => prompt.id === active.id
          );
          return labels.dragStart(
            promptsById.get(String(active.id))?.title ?? String(active.id),
            index + 1,
            filteredPrompts.length
          );
        },
        onDragMove({ active, over }: DragAnnouncementInput) {
          const index = over
            ? filteredPrompts.findIndex((prompt) => prompt.id === over.id)
            : filteredPrompts.findIndex((prompt) => prompt.id === active.id);
          return labels.dragMove(
            promptsById.get(String(active.id))?.title ?? String(active.id),
            index + 1,
            filteredPrompts.length
          );
        },
        onDragOver({ active, over }: DragAnnouncementInput) {
          const index = over
            ? filteredPrompts.findIndex((prompt) => prompt.id === over.id)
            : filteredPrompts.findIndex((prompt) => prompt.id === active.id);
          return labels.dragMove(
            promptsById.get(String(active.id))?.title ?? String(active.id),
            index + 1,
            filteredPrompts.length
          );
        },
        onDragEnd({ active, over }: DragAnnouncementInput) {
          if (!over) {
            return labels.dragCancel(
              promptsById.get(String(active.id))?.title ?? String(active.id),
              filteredPrompts.findIndex((prompt) => prompt.id === active.id) +
                1,
              filteredPrompts.length
            );
          }
          const index = filteredPrompts.findIndex(
            (prompt) => prompt.id === over.id
          );
          return labels.dragDrop(
            promptsById.get(String(active.id))?.title ?? String(active.id),
            index + 1,
            filteredPrompts.length
          );
        },
        onDragCancel({ active }: DragAnnouncementInput) {
          const index = filteredPrompts.findIndex(
            (prompt) => prompt.id === active.id
          );
          return labels.dragCancel(
            promptsById.get(String(active.id))?.title ?? String(active.id),
            index + 1,
            filteredPrompts.length
          );
        }
      },
      screenReaderInstructions: { draggable: labels.dragInstructions }
    }),
    [filteredPrompts, labels, promptsById]
  );

  const valueProp1 = useMemo<
    NonNullable<
      React.ComponentProps<typeof Sortable<AgentHostQuickPrompt>>["value"]
    >
  >(() => [...filteredPrompts], [filteredPrompts]);
  const getItemValueProp2 = usePersistCallback<
    NonNullable<
      React.ComponentProps<
        typeof Sortable<AgentHostQuickPrompt>
      >["getItemValue"]
    >
  >((prompt) => prompt.id);
  const onMoveProp3 = usePersistCallback<
    NonNullable<
      React.ComponentProps<typeof Sortable<AgentHostQuickPrompt>>["onMove"]
    >
  >(({ activeIndex, overIndex }) => {
    const next = [...filteredPrompts];
    const [moving] = next.splice(activeIndex, 1);
    if (!moving) return;
    next.splice(overIndex, 0, moving);
    const beforePromptId = next[overIndex + 1]?.id ?? null;
    void controller.reorderPrompts(moving.id, beforePromptId).then(() => {
      window.requestAnimationFrame(() =>
        handleRefs.current.get(moving.id)?.focus()
      );
    });
  });
  return (
    <Sortable
      accessibility={accessibility}
      value={valueProp1}
      getItemValue={getItemValueProp2}
      onMove={onMoveProp3}
    >
      <SortableContent className="flex flex-col gap-0.5">
        {filteredPrompts.map((prompt, index) => (
          <SortableItem key={prompt.id} value={prompt.id}>
            <AgentQuickPromptRowBinding1
              handleRefs={handleRefs}
              prompt={prompt}
              labels={labels}
              isSorting={isSorting}
              controller={controller}
              snapshot={snapshot}
              rowRefs={rowRefs}
              onDelete={onDelete}
              onEdit={onEdit}
              onFocusRow={onFocusRow}
              index={index}
              filteredPrompts={filteredPrompts}
              onSelect={onSelect}
            />
          </SortableItem>
        ))}
      </SortableContent>
    </Sortable>
  );
}

type DragAnnouncementInput = {
  active: { id: string | number };
  over: { id: string | number } | null;
};

const AgentQuickPromptRowBinding1 = observer(
  function AgentQuickPromptRowBinding1({
    handleRefs,
    prompt,
    labels,
    isSorting,
    controller,
    snapshot,
    rowRefs,
    onDelete,
    onEdit,
    onFocusRow,
    index,
    filteredPrompts,
    onSelect
  }: {
    handleRefs: import("react").RefObject<Map<string, HTMLButtonElement>>;
    prompt: AgentHostQuickPrompt;
    labels: NonNullable<
      NonNullable<AgentQuickPromptListProps>["controller"]
    >["labels"];
    isSorting: NonNullable<AgentQuickPromptListProps>["isSorting"];
    controller: NonNullable<AgentQuickPromptListProps>["controller"];
    snapshot: NonNullable<
      NonNullable<AgentQuickPromptListProps>["controller"]
    >["snapshot"];
    rowRefs: NonNullable<AgentQuickPromptListProps>["rowRefs"];
    onDelete: NonNullable<AgentQuickPromptListProps>["onDelete"];
    onEdit: NonNullable<AgentQuickPromptListProps>["onEdit"];
    onFocusRow: NonNullable<AgentQuickPromptListProps>["onFocusRow"];
    index: number;
    filteredPrompts: NonNullable<
      NonNullable<AgentQuickPromptListProps>["controller"]
    >["filteredPrompts"];
    onSelect: NonNullable<AgentQuickPromptListProps>["onSelect"];
  }) {
    const handleRefProp4 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof AgentQuickPromptRow>["handleRef"]>
    >((node) => {
      if (node) handleRefs.current.set(prompt.id, node);
      else handleRefs.current.delete(prompt.id);
    });
    const selectionRefProp5 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof AgentQuickPromptRow>["selectionRef"]
      >
    >((node) => {
      if (node) rowRefs.current.set(prompt.id, node);
      else rowRefs.current.delete(prompt.id);
    });
    const onDeleteProp6 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof AgentQuickPromptRow>["onDelete"]>
    >(() => onDelete(prompt));
    const onEditProp7 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof AgentQuickPromptRow>["onEdit"]>
    >(() => onEdit(prompt));
    const onFocusNextProp8 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof AgentQuickPromptRow>["onFocusNext"]
      >
    >((offset) =>
      onFocusRow(
        (index + offset + filteredPrompts.length) % filteredPrompts.length
      )
    );
    const onSelectProp9 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof AgentQuickPromptRow>["onSelect"]>
    >(() => onSelect(prompt));
    return (
      <AgentQuickPromptRow
        handleRef={handleRefProp4}
        labels={labels}
        isSorting={isSorting}
        pending={
          controller.isInteractionLocked ||
          snapshot.pendingMutationIds.includes(prompt.id)
        }
        prompt={prompt}
        reorderDisabled={!controller.canReorder}
        selectionRef={selectionRefProp5}
        showReorderHandle={isSorting && controller.showReorderHandles}
        onDelete={onDeleteProp6}
        onEdit={onEditProp7}
        onFocusNext={onFocusNextProp8}
        onSelect={onSelectProp9}
      />
    );
  }
);

type AgentQuickPromptListProps = {
  controller: AgentQuickPromptLibraryController;
  isSorting: boolean;
  onDelete: (prompt: AgentHostQuickPrompt) => void;
  onEdit: (prompt: AgentHostQuickPrompt) => void;
  onFocusRow: (index: number) => void;
  onSelect: (prompt: AgentHostQuickPrompt) => void;
  rowRefs: React.MutableRefObject<Map<string, HTMLButtonElement>>;
};

type AgentQuickPromptRowBinding1Props = {
  handleRefs: import("react").RefObject<Map<string, HTMLButtonElement>>;
  prompt: AgentHostQuickPrompt;
  labels: import("./agentQuickPromptLabels").AgentQuickPromptLabels;
  isSorting: boolean;
  controller: AgentQuickPromptLibraryController;
  snapshot: import("../../../../host/agentHostApi").AgentHostQuickPromptSnapshot;
  rowRefs: NonNullable<{
    controller: AgentQuickPromptLibraryController;
    isSorting: boolean;
    onDelete: (prompt: AgentHostQuickPrompt) => void;
    onEdit: (prompt: AgentHostQuickPrompt) => void;
    onFocusRow: (index: number) => void;
    onSelect: (prompt: AgentHostQuickPrompt) => void;
    rowRefs: React.MutableRefObject<Map<string, HTMLButtonElement>>;
  }>["rowRefs"];
  onDelete: (prompt: AgentHostQuickPrompt) => void;
  onEdit: (prompt: AgentHostQuickPrompt) => void;
  onFocusRow: (index: number) => void;
  index: number;
  filteredPrompts: readonly AgentHostQuickPrompt[];
  onSelect: (prompt: AgentHostQuickPrompt) => void;
};
