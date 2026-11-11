"use client";

import { Fragment, useCallback, useMemo, useRef, type ReactNode } from "react";
import type { MenuPrimitives } from "./menu-primitives";
import type { RowActionItem } from "./row-actions-menu";

export type { RowActionItem };

/**
 * The object around a right-clicked piece of content — a comment around a
 * link, a mention, an image or selected text. The content's own menu leads
 * and the object's actions follow in one submenu, so a menu never stacks
 * more than two layers.
 */
export interface ContextMenuScope {
  /** Names the submenu, e.g. "Comment actions". */
  label: string;
  icon: ReactNode;
  groups: RowActionItem[][];
  /** Entries for text selected inside the scope, after the host's Copy. */
  selectionActions?: (text: string) => RowActionItem[];
  /**
   * What the right-clicked content itself offers — an attachment's preview,
   * download and link. They lead the menu and stand in for the host's link
   * entries.
   */
  contentActions?: RowActionItem[];
}

const SCOPE_ATTRIBUTE = "data-context-menu-scope";
/** Marks content with a menu of its own, which an enclosing row defers to. */
const CONTENT_ATTRIBUTE = "data-context-menu-content";
const scopes = new WeakMap<Element, () => Partial<ContextMenuScope> | null>();

/**
 * A ref that registers its element as a scope. The reader runs when a menu
 * opens inside the element, so it always sees the latest render's actions.
 * A scope may provide only some fields: the nearest scope with groups names
 * the submenu, the nearest with content actions leads the menu, and every
 * scope's selection actions apply. Pass `content` for an element whose
 * content actions should win over an enclosing row's own menu.
 */
export function useContextMenuScope(
  read: () => Partial<ContextMenuScope> | null,
  { content = false }: { content?: boolean } = {},
) {
  const readRef = useRef(read);
  readRef.current = read;
  return useCallback((element: HTMLElement | null) => {
    if (!element) return;
    element.setAttribute(SCOPE_ATTRIBUTE, "");
    element.toggleAttribute(CONTENT_ATTRIBUTE, content);
    scopes.set(element, () => readRef.current());
  }, [content]);
}

export interface ResolvedContextMenuScope {
  submenu: Pick<ContextMenuScope, "label" | "icon" | "groups"> | null;
  /** Every scope's selection entries, outermost first. */
  selectionActions: (text: string) => RowActionItem[];
  contentActions: RowActionItem[];
}

/** The scopes around `from`, read when the menu opens. */
export function findContextMenuScope(from: Element | null): ResolvedContextMenuScope {
  let submenu: ResolvedContextMenuScope["submenu"] = null;
  let contentActions: RowActionItem[] | null = null;
  const selectionReaders: ((text: string) => RowActionItem[])[] = [];
  let element = from?.closest(`[${SCOPE_ATTRIBUTE}]`) ?? null;
  while (element) {
    const scope = scopes.get(element)?.();
    if (scope) {
      const groups = scope.groups?.filter((group) => group.length > 0) ?? [];
      if (!submenu && scope.label && groups.length > 0) {
        submenu = { label: scope.label, icon: scope.icon, groups };
      }
      if (!contentActions && scope.contentActions?.length) contentActions = scope.contentActions;
      if (scope.selectionActions) selectionReaders.unshift(scope.selectionActions);
    }
    element = element.parentElement?.closest(`[${SCOPE_ATTRIBUTE}]`) ?? null;
  }
  return {
    submenu,
    contentActions: contentActions ?? [],
    selectionActions: (text) => selectionReaders.flatMap((read) => read(text)),
  };
}

/**
 * The submenu with its actions marked, and a `finalFocus` for the host menu:
 * an action of the surrounding object moves focus itself (into a reply box,
 * an emoji picker), so the closing menu must not pull it back.
 */
export function useScopeSubmenuFocus(submenu: ResolvedContextMenuScope["submenu"]) {
  const picked = useRef(false);
  const wrapped = useMemo(
    () =>
      submenu && {
        ...submenu,
        groups: submenu.groups.map((group) =>
          group.map((action) => ({
            ...action,
            onSelect: () => {
              picked.current = true;
              action.onSelect();
            },
          })),
        ),
      },
    [submenu],
  );
  const finalFocus = useCallback(() => {
    const skip = picked.current;
    picked.current = false;
    return !skip;
  }, []);
  return { submenu: wrapped, finalFocus };
}

/** Action groups as menu items, a separator between groups. */
export function MenuActionGroups({
  groups,
  primitives: P,
}: {
  groups: RowActionItem[][];
  primitives: MenuPrimitives;
}) {
  return groups
    .filter((group) => group.length > 0)
    .map((group, index) => (
      <Fragment key={group[0]!.key}>
        {index > 0 && <P.Separator />}
        {group.map((item) => (
          <P.Item
            key={item.key}
            variant={item.danger ? "destructive" : "default"}
            onClick={item.onSelect}
          >
            {item.icon}
            {item.label}
          </P.Item>
        ))}
      </Fragment>
    ));
}

/** The surrounding object's actions as the menu's last entry. */
export function ContextMenuScopeSubmenu({
  submenu,
  primitives: P,
  separated = true,
}: {
  submenu: ResolvedContextMenuScope["submenu"];
  primitives: MenuPrimitives;
  /** Whether items come before it. */
  separated?: boolean;
}) {
  if (!submenu) return null;
  return (
    <>
      {separated && <P.Separator />}
      <P.Sub>
        <P.SubTrigger>
          {submenu.icon}
          {submenu.label}
        </P.SubTrigger>
        <P.SubContent className="w-auto min-w-44">
          <MenuActionGroups groups={submenu.groups} primitives={P} />
        </P.SubContent>
      </P.Sub>
    </>
  );
}
