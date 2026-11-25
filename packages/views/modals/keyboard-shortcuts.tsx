"use client";

import { useMemo, useState } from "react";
import { Keyboard, Search } from "@multica/ui/icons";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useIsGuest } from "@multica/core/permissions";
import {
  createShortcutChord,
  resolveShortcut,
  useShortcutStore,
  type ShortcutActionId,
  type ShortcutChord,
} from "@multica/core/shortcuts";
import { useT } from "../i18n";
import { AppLink, useNavigation } from "../navigation";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
import { isShortcutActionAvailable } from "../layout/global-shortcuts";

type GroupId = "inbox" | "issue" | "navigation" | "general" | "list" | "editor";
type FixedRowId =
  | "list_move"
  | "list_columns"
  | "list_open"
  | "list_peek"
  | "list_select"
  | "list_extend"
  | "list_select_all"
  | "list_clear"
  | "editor_leave";

type Row =
  | { kind: "action"; id: ShortcutActionId }
  | { kind: "fixed"; id: FixedRowId; shortcuts: readonly ShortcutChord[] };

const action = (id: ShortcutActionId): Row => ({ kind: "action", id });
const fixed = (id: FixedRowId, ...shortcuts: ShortcutChord[]): Row => ({ kind: "fixed", id, shortcuts });
const key = createShortcutChord;

/**
 * What the sheet lists, in its default order. Bindings come from the registry
 * (so a rebound key shows as rebound, and an unbound action is left out);
 * the list and editor keys are fixed and listed as such.
 */
const GROUPS: readonly { id: GroupId; rows: readonly Row[] }[] = [
  {
    id: "inbox",
    rows: [
      action("archiveInboxItem"),
      action("unsubscribeArchiveInboxItem"),
      action("snoozeInboxItem"),
      action("markInboxUnread"),
      action("nextInboxItem"),
      action("prevInboxItem"),
      action("undoInboxAction"),
    ],
  },
  {
    id: "issue",
    rows: [
      action("setIssueStatus"),
      action("assignIssue"),
      action("setIssuePriority"),
      action("setIssueLabels"),
      action("setIssueDueDate"),
      action("moveIssueToProject"),
      action("runIssueQuickAction"),
      action("replyToIssue"),
    ],
  },
  {
    id: "navigation",
    rows: [
      action("goInbox"),
      action("goMyIssues"),
      action("goIssues"),
      action("goProjects"),
      action("goChat"),
      action("goAgents"),
      action("goAutopilots"),
      action("goSquads"),
      action("goUsage"),
      action("goRuntimes"),
      action("goSkills"),
      action("goSettings"),
      action("goBack"),
      action("goForward"),
    ],
  },
  {
    id: "general",
    rows: [
      action("openSearch"),
      action("createIssue"),
      action("showShortcuts"),
      action("toggleSidebar"),
      action("toggleRightSidebar"),
      action("toggleChat"),
      action("newChat"),
    ],
  },
  {
    id: "list",
    rows: [
      fixed("list_move", key("J"), key("K")),
      fixed("list_columns", key("Left"), key("Right")),
      fixed("list_open", key("Enter")),
      fixed("list_peek", key("Space")),
      fixed("list_select", key("X")),
      fixed("list_extend", key("Up", { shift: true }), key("Down", { shift: true })),
      fixed("list_select_all", key("A", { primary: true })),
      fixed("list_clear", key("Escape")),
    ],
  },
  {
    id: "editor",
    rows: [action("send"), fixed("editor_leave", key("Escape")), action("findInIssue"), action("voiceInput")],
  },
];

/** The groups the current page uses, most specific first. */
function useCurrentGroups(): ReadonlySet<GroupId> {
  const { pathname } = useNavigation();
  const p = useWorkspacePaths();
  return useMemo(() => {
    const under = (path: string) => pathname === path || pathname.startsWith(`${path}/`);
    if (under(p.inbox())) return new Set<GroupId>(["inbox", "issue"]);
    if (pathname.startsWith(`${p.issues()}/`)) return new Set<GroupId>(["issue"]);
    if (pathname === p.issues() || under(p.myIssues()) || under(p.views()) || pathname.startsWith(`${p.projects()}/`)) {
      return new Set<GroupId>(["list", "issue"]);
    }
    return new Set<GroupId>();
  }, [p, pathname]);
}

export function KeyboardShortcutsModal({
  open = true,
  onClose,
  onOpenChangeComplete,
}: {
  open?: boolean;
  onClose: () => void;
  onOpenChangeComplete?: (open: boolean) => void;
}) {
  const { t } = useT("settings");
  const [query, setQuery] = useState("");
  const overrides = useShortcutStore((state) => state.overrides);
  const current = useCurrentGroups();
  const p = useWorkspacePaths();
  const guest = useIsGuest(useCurrentWorkspace()?.id);

  const groups = useMemo(() => {
    const labelOf = (row: Row) =>
      row.kind === "action"
        ? t(($) => $.shortcuts.actions[row.id].label)
        : t(($) => $.shortcuts.cheatsheet.rows[row.id]);
    const needle = query.trim().toLocaleLowerCase();
    const ordered = [
      ...[...current].flatMap((id) => GROUPS.filter((group) => group.id === id)),
      ...GROUPS.filter((group) => !current.has(group.id)),
    ];
    return ordered
      .map((group) => ({
        id: group.id,
        rows: group.rows.flatMap((row) => {
          if (row.kind === "action" && !isShortcutActionAvailable(row.id, guest)) return [];
          const shortcuts =
            row.kind === "action" ? [resolveShortcut(overrides, row.id)].filter((s) => s !== null) : row.shortcuts;
          if (shortcuts.length === 0) return [];
          const label = labelOf(row);
          if (needle && !label.toLocaleLowerCase().includes(needle)) return [];
          return [{ id: row.id, label, shortcuts }];
        }),
      }))
      .filter((group) => group.rows.length > 0);
  }, [current, guest, overrides, query, t]);

  const then = t(($) => $.shortcuts.then);

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()} onOpenChangeComplete={onOpenChangeComplete}>
      <DialogContent className="flex max-h-[min(40rem,85%)] flex-col gap-0 overflow-hidden p-0 sm:max-w-4xl">
        <div className="flex shrink-0 items-center gap-4 border-b px-5 py-3 pr-12">
          <DialogTitle className="flex-1">{t(($) => $.shortcuts.cheatsheet.title)}</DialogTitle>
          <div className="relative w-56">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t(($) => $.shortcuts.search_placeholder)}
              aria-label={t(($) => $.shortcuts.search_placeholder)}
              className="h-8 pl-8"
            />
          </div>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {groups.length === 0 ? (
            <p className="py-10 text-center text-body text-muted-foreground">{t(($) => $.shortcuts.no_results)}</p>
          ) : (
            <div className="grid gap-x-8 gap-y-6 sm:grid-cols-2 lg:grid-cols-3">
              {groups.map((group) => (
                <section key={group.id} aria-labelledby={`shortcuts-group-${group.id}`}>
                  <h3 id={`shortcuts-group-${group.id}`} className="mb-1.5 text-caption font-medium text-muted-foreground">
                    {t(($) => $.shortcuts.cheatsheet.groups[group.id])}
                    {current.has(group.id) ? (
                      <span className="font-normal"> · {t(($) => $.shortcuts.cheatsheet.current_page)}</span>
                    ) : null}
                  </h3>
                  <ul>
                    {group.rows.map((row) => (
                      <li key={row.id} className="flex min-h-8 items-center justify-between gap-3 text-body">
                        <span className="min-w-0 truncate">{row.label}</span>
                        <span className="flex shrink-0 items-center gap-1">
                          {row.shortcuts.map((shortcut, index) => (
                            <ShortcutKeycaps key={index} shortcut={shortcut} thenLabel={then} />
                          ))}
                        </span>
                      </li>
                    ))}
                  </ul>
                </section>
              ))}
            </div>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-2 border-t px-5 py-3 text-caption text-muted-foreground">
          <Keyboard className="size-3.5 shrink-0" />
          <span>
            {t(($) => $.shortcuts.cheatsheet.footer_prefix)}
            <AppLink href={`${p.settings()}?tab=shortcuts`} className="text-brand hover:underline" onClick={onClose}>
              {t(($) => $.shortcuts.cheatsheet.footer_link)}
            </AppLink>
            {t(($) => $.shortcuts.cheatsheet.footer_suffix)}
          </span>
        </div>
      </DialogContent>
    </Dialog>
  );
}
