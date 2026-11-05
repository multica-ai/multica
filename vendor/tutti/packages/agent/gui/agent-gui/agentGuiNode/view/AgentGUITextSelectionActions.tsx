import { Slot } from "radix-ui";
import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { Button, MenuSurface } from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
import { createPortal } from "react-dom";

export interface AgentGUITextSelectionSnapshot {
  left: number;
  text: string;
  top: number;
}

export function readAgentGUITextSelection(
  root: HTMLElement
): AgentGUITextSelectionSnapshot | null {
  const ownerDocument = root.ownerDocument;
  const selection = ownerDocument.getSelection();
  if (
    !selection ||
    selection.isCollapsed ||
    selection.rangeCount === 0 ||
    !selection.anchorNode ||
    !selection.focusNode ||
    !root.contains(selection.anchorNode) ||
    !root.contains(selection.focusNode)
  ) {
    return null;
  }
  const text = selection.toString().trim();
  if (!text) return null;
  const rect = selection.getRangeAt(0).getBoundingClientRect();
  if (rect.width <= 0 && rect.height <= 0) return null;
  const viewportWidth = ownerDocument.defaultView?.innerWidth ?? 0;
  const center = rect.left + rect.width / 2;
  return {
    left: Math.min(Math.max(center, 132), Math.max(132, viewportWidth - 132)),
    text,
    top: Math.max(48, rect.top - 8)
  };
}

export const AgentGUITextSelectionActions = observer(
  function AgentGUITextSelectionActions({
    labels,
    snapshot,
    portalTarget,
    onAddToConversation,
    onAskInSide,
    onDismiss
  }: AgentGUITextSelectionActionsProps): React.JSX.Element {
    const run = (action: (text: string) => void) => {
      onDismiss();
      portalTarget.ownerDocument.getSelection()?.removeAllRanges();
      action(snapshot.text);
    };
    const runCallback = usePersistCallback(run);

    const styleProp1Value1 = snapshot.left;
    const styleProp1Value2 = snapshot.top;
    const styleProp1 = useMemo<
      NonNullable<React.ComponentProps<typeof MenuSurface>["style"]>
    >(
      () => ({
        left: styleProp1Value1,
        top: styleProp1Value2,
        zIndex: "var(--z-popover)"
      }),
      [styleProp1Value1, styleProp1Value2]
    );
    const onPointerDownProp2 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof MenuSurface>["onPointerDown"]>
    >((event) => event.preventDefault());
    const onClickProp3 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof Button>["onClick"]>
    >(() => run(onAddToConversation));
    return createPortal(
      <MenuSurface
        role="toolbar"
        aria-label={labels.addToConversation}
        className="nodrag fixed flex-row -translate-x-1/2 -translate-y-full gap-0 overflow-hidden rounded-xl p-0 text-[14px] font-medium shadow-[var(--tsh-shell-shadow)] [-webkit-app-region:no-drag]"
        data-testid="agent-gui-text-selection-actions"
        style={styleProp1}
        onPointerDown={onPointerDownProp2}
      >
        <Button
          type="button"
          variant="ghost"
          size="lg"
          className="h-10 rounded-none px-4 text-[14px] font-medium"
          onClick={onClickProp3}
        >
          {labels.addToConversation}
        </Button>
        {onAskInSide ? (
          <ButtonBinding1
            run={runCallback}
            onAskInSide={onAskInSide}
            labels={labels}
          />
        ) : null}
      </MenuSurface>,
      portalTarget
    );
  }
);

const ButtonBinding1 = observer(function ButtonBinding1({
  run,
  onAskInSide,
  labels,
  ...slotProps
}: {
  run: (action: (text: string) => void) => void;
  onAskInSide: (text: string) => void;
  labels: NonNullable<AgentGUITextSelectionActionsProps>["labels"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    run: (action: (text: string) => void) => void;
    onAskInSide: (text: string) => void;
    labels: NonNullable<AgentGUITextSelectionActionsProps>["labels"];
  }
>) {
  const onClickProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => run(onAskInSide));
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="ghost"
        size="lg"
        className="h-10 rounded-none border-0 border-l border-[var(--border-1)] px-4 text-[14px] font-medium"
        onClick={onClickProp4}
      >
        {labels.askInSide}
      </Button>
    </Slot.Root>
  );
});

type AgentGUITextSelectionActionsProps = {
  labels: { addToConversation: string; askInSide: string };
  snapshot: AgentGUITextSelectionSnapshot;
  portalTarget: HTMLElement;
  onAddToConversation: (text: string) => void;
  onAskInSide?: (text: string) => void;
  onDismiss: () => void;
};

type ButtonBinding1Props = {
  run: (action: (text: string) => void) => void;
  onAskInSide: (text: string) => void;
  labels: { addToConversation: string; askInSide: string };
};
