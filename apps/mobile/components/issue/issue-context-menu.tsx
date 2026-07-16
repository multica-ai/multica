/**
 * Status actions for the native iOS menu (UIMenu via @react-native-menu/menu)
 * shown when an issue row is long-pressed — see SwipeableIssueRow. Cancelled is
 * destructive (red).
 *
 * Swipe handles the one quick, safe action (Done). This menu is the deliberate
 * path to *any* status. Options are identical for human- and agent-assigned
 * issues — the affordance never forks by assignee type.
 *
 * Each action carries an SF Symbol (`image`). NOTE: @react-native-menu/menu
 * does not currently render action images on the New Architecture (Fabric) —
 * the menu is text-only for now. The symbols are kept so icons appear once the
 * library closes that gap; the icon-capable alternative
 * (react-native-ios-context-menu) does not yet compile against RN 0.83.
 *
 * NOTE (agent side effect): advancing an agent-assigned issue into an active
 * state (e.g. backlog → todo) kicks the agent off to start working — real
 * compute. The exact trigger rule lives in the backend; a future refinement is
 * to confirm that consequence. Left out for now pending the precise rule.
 */
import type { MenuAction } from "@react-native-menu/menu";
import type { Issue, IssueStatus } from "@multica/core/types";
import { STATUS_LABEL } from "@/lib/issue-status";

// Workflow order. SF Symbols echo the status semantics.
const STATUS_MENU: { status: IssueStatus; symbol: string }[] = [
  { status: "backlog", symbol: "tray" },
  { status: "todo", symbol: "circle" },
  { status: "in_progress", symbol: "circle.lefthalf.filled" },
  { status: "in_review", symbol: "eye" },
  { status: "blocked", symbol: "exclamationmark.octagon" },
  { status: "done", symbol: "checkmark.circle.fill" },
  { status: "cancelled", symbol: "xmark.circle" },
];

/** Every status except the issue's current one (selecting it would no-op). */
export function statusMenuActions(issue: Issue): MenuAction[] {
  return STATUS_MENU.filter(({ status }) => status !== issue.status).map(
    ({ status, symbol }) => ({
      id: status,
      title: STATUS_LABEL[status],
      image: symbol,
      ...(status === "cancelled"
        ? { attributes: { destructive: true } }
        : {}),
    }),
  );
}
