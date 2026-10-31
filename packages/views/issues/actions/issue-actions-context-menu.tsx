"use client";

import {
  cloneElement,
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
} from "react";
import { toast } from "sonner";
import type { AgentTask, Issue } from "@multica/core/types";
import { useCancelIssueRun } from "@multica/core/issues/mutations";
import {
  ContextMenu,
  ContextMenuContent,
} from "@multica/ui/components/ui/context-menu";
import { useIssueActions } from "./use-issue-actions";
import {
  IssueActionsMenuItems,
  contextPrimitives,
} from "./issue-actions-menu-items";
import { TerminateTaskConfirmDialog } from "../components/terminate-task-confirm-dialog";
import { useT } from "../../i18n";

/**
 * One shared context menu per surface instead of one Base UI ContextMenu
 * root per card/row. A board remount used to create N × (MenuRoot + Trigger)
 * — the single largest slice of the tab-switch freeze — even though at most
 * one menu is ever open. Items delegate: the per-item wrapper only attaches
 * an `onContextMenu` that reports (issue, cursor position) up to the
 * provider's singleton, which anchors the menu at the cursor via a virtual
 * anchor element.
 *
 * Known debt: Base UI's ContextMenuTrigger also opened on touch long-press;
 * the delegated `contextmenu` event covers right-click (and Android
 * long-press) but not iOS Safari long-press.
 */

interface ActiveMenu {
  issue: Issue;
  position: { x: number; y: number };
}

type OpenIssueContextMenu = (issue: Issue, event: React.MouseEvent) => void;

const IssueContextMenuContext = createContext<OpenIssueContextMenu | null>(
  null,
);

type IssueContextMenuVariant = "full" | "sidebar";

export function IssueContextMenuProvider({
  children,
  variant = "full",
}: {
  children: ReactNode;
  /** Which menu the surface's rows open; see `IssueActionsMenuItems`. */
  variant?: IssueContextMenuVariant;
}) {
  const [active, setActive] = useState<ActiveMenu | null>(null);
  const [open, setOpen] = useState(false);
  // The element the user right-clicked. Base UI's per-item trigger used to
  // stamp `data-popup-open` on it (rows/cards style their "menu is open for
  // me" state off that attribute). React never renders this attribute, so
  // managing it imperatively here keeps the affordance without re-rendering
  // every row on open/close.
  const triggerElRef = useRef<HTMLElement | null>(null);

  const openMenu = useCallback<OpenIssueContextMenu>((issue, event) => {
    event.preventDefault();
    triggerElRef.current?.removeAttribute("data-popup-open");
    const el = event.currentTarget as HTMLElement;
    el.setAttribute("data-popup-open", "");
    triggerElRef.current = el;
    setActive({ issue, position: { x: event.clientX, y: event.clientY } });
    setOpen(true);
  }, []);

  const handleOpenChange = useCallback((v: boolean) => {
    if (!v) {
      triggerElRef.current?.removeAttribute("data-popup-open");
      triggerElRef.current = null;
    }
    setOpen(v);
  }, []);

  return (
    <IssueContextMenuContext.Provider value={openMenu}>
      {children}
      {/* Mounted on first use, kept mounted after (one closed menu root per
          surface — the popup itself unmounts while closed). */}
      {active && (
        <IssueContextMenuSingleton
          issue={active.issue}
          position={active.position}
          open={open}
          onOpenChange={handleOpenChange}
          variant={variant}
        />
      )}
    </IssueContextMenuContext.Provider>
  );
}

function IssueContextMenuSingleton({
  issue,
  position,
  open,
  onOpenChange,
  variant,
}: ActiveMenu & {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  variant: IssueContextMenuVariant;
}) {
  const actions = useIssueActions(issue);
  const [stopTask, setStopTask] = useState<AgentTask | null>(null);

  // Point-sized virtual anchor at the right-click position — replaces the
  // cursor anchor Base UI's own trigger would have registered.
  const anchor = useMemo(
    () => ({
      getBoundingClientRect: () =>
        DOMRect.fromRect({
          x: position.x,
          y: position.y,
          width: 0,
          height: 0,
        }),
    }),
    [position.x, position.y],
  );

  return (
    <>
      <ContextMenu open={open} onOpenChange={(v) => onOpenChange(v)}>
        {/* A fixed width keeps the sidebar menu from resizing with the
            header title, which is clamped to two lines instead. */}
        <ContextMenuContent anchor={anchor} className={variant === "sidebar" ? "w-62" : undefined}>
          <IssueActionsMenuItems
            issue={issue}
            actions={actions}
            primitives={contextPrimitives}
            onClose={() => onOpenChange(false)}
            variant={variant}
            onStopRun={setStopTask}
          />
        </ContextMenuContent>
      </ContextMenu>
      {stopTask && (
        <StopRunConfirmDialog
          issueId={issue.id}
          task={stopTask}
          onClose={() => setStopTask(null)}
        />
      )}
    </>
  );
}

function StopRunConfirmDialog({
  issueId,
  task,
  onClose,
}: {
  issueId: string;
  task: AgentTask;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const cancelRun = useCancelIssueRun(issueId);
  return (
    <TerminateTaskConfirmDialog
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      onConfirm={() => {
        // The dialog closes (and unmounts this component) before the request
        // settles, so per-call mutate callbacks would never fire.
        void cancelRun.mutateAsync(task.id).catch((e: unknown) => {
          toast.error(e instanceof Error ? e.message : t(($) => $.execution_log.cancel_failed));
        });
      }}
      showRunningNote
    />
  );
}

interface IssueActionsContextMenuProps {
  issue: Issue;
  /** A single React element the menu trigger behavior is grafted onto. */
  children: ReactElement<{
    onContextMenu?: (e: React.MouseEvent) => void;
    className?: string;
  }>;
}

/**
 * Per-item wrapper: clones its child with an `onContextMenu` that opens the
 * surface-level singleton menu for this issue. Costs one context read — no
 * menu machinery per item. Requires {@link IssueContextMenuProvider} above
 * (IssueSurface mounts it).
 */
export function IssueActionsContextMenu({
  issue,
  children,
}: IssueActionsContextMenuProps) {
  const openMenu = useContext(IssueContextMenuContext);
  if (!openMenu) {
    throw new Error(
      "IssueActionsContextMenu requires an IssueContextMenuProvider ancestor",
    );
  }
  const childOnContextMenu = children.props.onContextMenu;
  const childClassName = children.props.className;
  return cloneElement(children, {
    // Base UI's trigger merged `select-none` into the item; keep that so
    // right-click targets don't become text-selectable.
    className: childClassName ? `${childClassName} select-none` : "select-none",
    onContextMenu: (e: React.MouseEvent) => {
      childOnContextMenu?.(e);
      openMenu(issue, e);
    },
  });
}
