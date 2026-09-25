"use client";

import type { InboxItem } from "@multica/core/types";
import { RowActionsMenu } from "../../common/row-actions-menu";
import { useT } from "../../i18n";
import { useInboxRowActions } from "./inbox-context-menu";
import { useInboxItemActions } from "./inbox-item-actions";
import type { InboxView } from "./inbox-view";

/**
 * The row's compact action menu. It carries the same actions as the desktop
 * right-click menu, and is the only way to reach them on a touch pointer,
 * which has neither hover nor right-click.
 *
 * Renders nothing without an `InboxContextMenuProvider`: the actions live on
 * the list, and a row without them has nothing to offer.
 */
export function InboxRowMenu({
  item,
  view,
}: {
  item: InboxItem;
  view: InboxView;
}) {
  const { t } = useT("inbox");
  const actions = useInboxRowActions();
  const groups = useInboxItemActions(item, view, actions);
  if (!actions) return null;

  return <RowActionsMenu label={t(($) => $.list.actions_aria)} groups={groups} />;
}
