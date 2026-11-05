import { Slot } from "radix-ui";
import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  useCallback,
  useEffect,
  useRef,
  type KeyboardEventHandler,
  type PointerEventHandler
} from "react";
import { createPortal } from "react-dom";
import type { Editor } from "@tiptap/core";
import { EditorContent } from "@tiptap/react";
import { cn } from "../../../app/renderer/lib/utils";
import type { TranslateFn } from "../../../i18n/index";
import type { AgentRichTextContextMenuState } from "./AgentRichTextEditor.types";
import { getPortalContainer } from "@tutti-os/ui-system/portal-container";
import { observer } from "mobx-react-lite";
export const AgentRichTextEditorSurface = observer(
  AgentRichTextEditorSurfaceRender
);
const AgentRichTextContextMenuButton = observer(
  AgentRichTextContextMenuButtonRender
);

function AgentRichTextEditorSurfaceRender({
  className,
  contextMenu,
  copySelection,
  cutSelection,
  disabled,
  editor,
  handleKeyDownCapture,
  handlePointerDownCapture,
  pasteClipboardText,
  placeholder,
  testId,
  t
}: AgentRichTextEditorSurfaceProps): React.JSX.Element {
  useEffect(() => {
    if (!editor) return;
    editor.setEditable(!disabled);
    editor.view.dom.setAttribute("aria-disabled", disabled ? "true" : "false");
    editor.view.dom.setAttribute("aria-label", placeholder);
    if (testId) editor.view.dom.setAttribute("data-testid", testId);
    else editor.view.dom.removeAttribute("data-testid");
    editor.view.dispatch(
      editor.state.tr.setMeta("agentRichTextPlaceholder", placeholder)
    );
  }, [disabled, editor, placeholder, testId]);

  return (
    <div
      className="agent-gui-node__rich-text-editor-surface relative min-w-0"
      onKeyDownCapture={handleKeyDownCapture}
      onPointerDownCapture={handlePointerDownCapture}
    >
      {editor ? (
        <EditorContent
          className="agent-gui-node__rich-text-editor-content"
          editor={editor}
        />
      ) : (
        <div
          role="textbox"
          aria-label={placeholder}
          aria-disabled={disabled ? "true" : "false"}
          aria-multiline="true"
          data-testid={testId}
          className={cn(
            className,
            "overflow-y-auto whitespace-pre-wrap break-words [&_p]:m-0"
          )}
        />
      )}
      {contextMenu
        ? createPortal(
            <DivBinding1
              t={t}
              contextMenu={contextMenu}
              cutSelection={cutSelection}
              copySelection={copySelection}
              pasteClipboardText={pasteClipboardText}
            />,
            getPortalContainer()
          )
        : null}
    </div>
  );
}

function AgentRichTextContextMenuButtonRender({
  disabled,
  label,
  onSelect
}: {
  disabled: boolean;
  label: string;
  onSelect: () => void | Promise<void>;
}): React.JSX.Element {
  const selectionStartedRef = useRef(false);
  const select = useCallback(() => {
    if (disabled || selectionStartedRef.current) return;
    selectionStartedRef.current = true;
    void Promise.resolve(onSelect()).finally(() => {
      selectionStartedRef.current = false;
    });
  }, [disabled, onSelect]);
  const selectCallback = usePersistCallback(select);

  const onPointerDownProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onPointerDown"]>
  >((event) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    select();
  });
  return (
    <button
      role="menuitem"
      className="block w-full rounded-[6px] px-3 py-1.5 text-left font-medium transition-colors hover:bg-[var(--transparency-hover)] focus-visible:bg-[var(--transparency-hover)] focus-visible:outline-none disabled:cursor-default disabled:opacity-45"
      disabled={disabled}
      type="button"
      onClick={selectCallback}
      onPointerDown={onPointerDownProp1}
    >
      {label}
    </button>
  );
}

const DivBinding1 = observer(function DivBinding1({
  t,
  contextMenu,
  cutSelection,
  copySelection,
  pasteClipboardText,
  ...slotProps
}: {
  t: NonNullable<AgentRichTextEditorSurfaceProps>["t"];
  contextMenu: AgentRichTextContextMenuState;
  cutSelection: NonNullable<AgentRichTextEditorSurfaceProps>["cutSelection"];
  copySelection: NonNullable<AgentRichTextEditorSurfaceProps>["copySelection"];
  pasteClipboardText: NonNullable<AgentRichTextEditorSurfaceProps>["pasteClipboardText"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    t: NonNullable<AgentRichTextEditorSurfaceProps>["t"];
    contextMenu: AgentRichTextContextMenuState;
    cutSelection: NonNullable<AgentRichTextEditorSurfaceProps>["cutSelection"];
    copySelection: NonNullable<AgentRichTextEditorSurfaceProps>["copySelection"];
    pasteClipboardText: NonNullable<AgentRichTextEditorSurfaceProps>["pasteClipboardText"];
  }
>) {
  const styleProp2Value1 = contextMenu.x;
  const styleProp2Value2 = contextMenu.y;
  const styleProp2 = useMemo<NonNullable<React.ComponentProps<"div">["style"]>>(
    () => ({ left: styleProp2Value1, top: styleProp2Value2 }),
    [styleProp2Value1, styleProp2Value2]
  );
  const onContextMenuProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<"div">["onContextMenu"]>
  >((event) => event.preventDefault());
  const onPointerDownProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<"div">["onPointerDown"]>
  >((event) => event.stopPropagation());
  return (
    <Slot.Root {...slotProps}>
      <div
        role="menu"
        aria-label={t("agentHost.agentGui.composerTextMenu")}
        className="fixed z-[var(--z-popover)] min-w-[132px] rounded-[8px] border border-[var(--line-1)] bg-[var(--background-panel)] p-1 text-[13px] text-[var(--text-primary)] shadow-[0_14px_34px_rgb(0_0_0_/_0.28)]"
        data-agent-composer-text-menu="true"
        style={styleProp2}
        onContextMenu={onContextMenuProp3}
        onPointerDown={onPointerDownProp4}
      >
        <AgentRichTextContextMenuButton
          disabled={!contextMenu.canEdit || !contextMenu.hasSelection}
          label={t("common.cut")}
          onSelect={cutSelection}
        />
        <AgentRichTextContextMenuButton
          disabled={!contextMenu.hasSelection}
          label={t("common.copy")}
          onSelect={copySelection}
        />
        <AgentRichTextContextMenuButton
          disabled={!contextMenu.canEdit}
          label={t("common.paste")}
          onSelect={pasteClipboardText}
        />
      </div>
    </Slot.Root>
  );
});

type AgentRichTextEditorSurfaceProps = {
  className?: string;
  contextMenu: AgentRichTextContextMenuState | null;
  copySelection: () => void | Promise<void>;
  cutSelection: () => void | Promise<void>;
  disabled: boolean;
  editor: Editor | null;
  handleKeyDownCapture: KeyboardEventHandler<HTMLDivElement>;
  handlePointerDownCapture?: PointerEventHandler<HTMLDivElement>;
  pasteClipboardText: () => void | Promise<void>;
  placeholder: string;
  testId?: string;
  t: TranslateFn;
};

type DivBinding1Props = {
  t: TranslateFn;
  contextMenu: AgentRichTextContextMenuState;
  cutSelection: () => void | Promise<void>;
  copySelection: () => void | Promise<void>;
  pasteClipboardText: () => void | Promise<void>;
};
