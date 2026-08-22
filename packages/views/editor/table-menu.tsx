"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  autoUpdate,
  computePosition,
  flip,
  hide,
  offset,
  shift,
} from "@floating-ui/dom";
import { posToDOMRect, type Editor } from "@tiptap/core";
import {
  BetweenHorizontalEnd,
  BetweenHorizontalStart,
  BetweenVerticalEnd,
  BetweenVerticalStart,
  Columns3,
  Rows3,
  Trash2,
  type LucideIcon,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Separator } from "@multica/ui/components/ui/separator";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../i18n";

function shouldShowTableMenu(editor: Editor): boolean {
  return editor.isEditable && !editor.isDestroyed && editor.isActive("table");
}

function selectionRect(editor: Editor): DOMRect {
  try {
    const { from, to } = editor.state.selection;
    const { node } = editor.view.domAtPos(from);
    const element = node instanceof Element ? node : node.parentElement;
    const cell = element?.closest("td, th");
    if (cell) return cell.getBoundingClientRect();
    return posToDOMRect(editor.view, from, to);
  } catch {
    return new DOMRect();
  }
}

function TableActionButton({
  icon: Icon,
  label,
  onAction,
  destructive = false,
}: {
  icon: LucideIcon;
  label: string;
  onAction: () => void;
  destructive?: boolean;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={label}
            className={
              destructive
                ? "text-destructive hover:text-destructive"
                : undefined
            }
            onClick={onAction}
            onMouseDown={(event) => event.preventDefault()}
          />
        }
      >
        <Icon className="size-3.5" />
      </TooltipTrigger>
      <TooltipContent side="top" sideOffset={8}>
        {label}
      </TooltipContent>
    </Tooltip>
  );
}

/** Contextual structural controls for the table cell holding the selection. */
function EditorTableMenu({ editor }: { editor: Editor }) {
  const { t } = useT("editor");
  const [visible, setVisible] = useState(() => shouldShowTableMenu(editor));
  const floatingRef = useRef<HTMLDivElement>(null);
  const updatePositionRef = useRef<() => void>(() => {});

  const virtualRef = useMemo(
    () => ({
      getBoundingClientRect: () =>
        editor.isDestroyed ? new DOMRect() : selectionRect(editor),
      contextElement: editor.view.dom,
    }),
    [editor],
  );

  useEffect(() => {
    const onTransaction = () => {
      if (!editor.isInitialized) return;
      const nextVisible = shouldShowTableMenu(editor);
      setVisible(nextVisible);
      if (nextVisible) updatePositionRef.current();
    };
    editor.on("transaction", onTransaction);
    return () => {
      editor.off("transaction", onTransaction);
    };
  }, [editor]);

  useEffect(() => {
    const onBlur = () => {
      setTimeout(() => {
        if (editor.isDestroyed || editor.view.hasFocus()) return;
        if (floatingRef.current?.contains(document.activeElement)) return;
        setVisible(false);
      }, 0);
    };
    editor.on("blur", onBlur);
    return () => {
      editor.off("blur", onBlur);
    };
  }, [editor]);

  useEffect(() => {
    const element = floatingRef.current;
    if (!visible || !element || !editor.isInitialized) return;

    const updatePosition = () => {
      void computePosition(virtualRef, element, {
        strategy: "fixed",
        placement: "bottom-start",
        middleware: [
          offset(6),
          flip({ fallbackPlacements: ["top-start"] }),
          shift({ padding: 8 }),
          hide(),
        ],
      }).then(({ x, y, middlewareData }) => {
        if (!element.isConnected) return;
        element.style.visibility = middlewareData.hide?.referenceHidden
          ? "hidden"
          : "visible";
        element.style.left = `${x}px`;
        element.style.top = `${y}px`;
      });
    };

    updatePositionRef.current = updatePosition;
    const cleanup = autoUpdate(virtualRef, element, updatePosition);
    return () => {
      updatePositionRef.current = () => {};
      cleanup();
    };
  }, [visible, editor, virtualRef]);

  useEffect(() => {
    if (!visible) return;
    const onMouseDown = (event: MouseEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (editor.view.dom.contains(target)) return;
      if (floatingRef.current?.contains(target)) return;
      setVisible(false);
    };
    document.addEventListener("mousedown", onMouseDown);
    return () => document.removeEventListener("mousedown", onMouseDown);
  }, [visible, editor]);

  const run = useCallback(
    (
      command: (
        chain: ReturnType<Editor["chain"]>,
      ) => ReturnType<Editor["chain"]>,
    ) => {
      command(editor.chain().focus()).run();
    },
    [editor],
  );

  if (!visible) return null;

  return (
    <div
      ref={floatingRef}
      role="toolbar"
      aria-label={t(($) => $.table_menu.label)}
      className="bubble-menu"
      style={{
        position: "fixed",
        zIndex: 50,
        width: "max-content",
        visibility: "hidden",
      }}
      onMouseDown={(event) => event.preventDefault()}
    >
      <TooltipProvider delay={300}>
        <TableActionButton
          icon={BetweenHorizontalStart}
          label={t(($) => $.table_menu.add_row_above)}
          onAction={() => run((chain) => chain.addRowBefore())}
        />
        <TableActionButton
          icon={BetweenHorizontalEnd}
          label={t(($) => $.table_menu.add_row_below)}
          onAction={() => run((chain) => chain.addRowAfter())}
        />
        <TableActionButton
          icon={Rows3}
          label={t(($) => $.table_menu.delete_row)}
          onAction={() => run((chain) => chain.deleteRow())}
        />
        <Separator orientation="vertical" className="mx-0.5 h-5" />
        <TableActionButton
          icon={BetweenVerticalStart}
          label={t(($) => $.table_menu.add_column_left)}
          onAction={() => run((chain) => chain.addColumnBefore())}
        />
        <TableActionButton
          icon={BetweenVerticalEnd}
          label={t(($) => $.table_menu.add_column_right)}
          onAction={() => run((chain) => chain.addColumnAfter())}
        />
        <TableActionButton
          icon={Columns3}
          label={t(($) => $.table_menu.delete_column)}
          onAction={() => run((chain) => chain.deleteColumn())}
        />
        <Separator orientation="vertical" className="mx-0.5 h-5" />
        <TableActionButton
          icon={Trash2}
          label={t(($) => $.table_menu.delete_table)}
          onAction={() => run((chain) => chain.deleteTable())}
          destructive
        />
      </TooltipProvider>
    </div>
  );
}

export { EditorTableMenu };
