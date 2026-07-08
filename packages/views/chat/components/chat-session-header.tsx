"use client";

import { useEffect, useRef, useState } from "react";
import { Archive, ArchiveRestore, MoreHorizontal, Pencil, Trash2, UserRound } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  useUpdateChatSession,
  useDeleteChatSession,
  useSetChatSessionArchived,
} from "@multica/core/chat/mutations";
import { useChatStore } from "@multica/core/chat";
import type { Agent, ChatSession } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

/**
 * Per-session header for the conversation pane: agent avatar + editable chat
 * title + agent subtitle, with a ⋯ menu (rename / view agent profile / delete).
 * The avatar's hover card is the lightweight "view profile" affordance; the
 * menu item navigates to the full agent page.
 */
export function ChatSessionHeader({
  session,
  agent,
}: {
  session: ChatSession;
  agent: Agent | null;
}) {
  const { t } = useT("chat");
  const wsPaths = useWorkspacePaths();
  const { push } = useNavigation();
  const updateSession = useUpdateChatSession();
  const deleteSession = useDeleteChatSession();
  const setArchived = useSetChatSessionArchived();
  const setActiveSession = useChatStore((s) => s.setActiveSession);

  const isArchived = session.status === "archived";

  const [editing, setEditing] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [draft, setDraft] = useState(session.title ?? "");
  const inputRef = useRef<HTMLInputElement>(null);

  const title = session.title?.trim() || t(($) => $.window.untitled);

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  const startRename = () => {
    setDraft(session.title ?? "");
    setEditing(true);
  };

  const commitRename = () => {
    setEditing(false);
    const trimmed = draft.trim();
    if (!trimmed || trimmed === session.title) return;
    updateSession.mutate({ sessionId: session.id, title: trimmed });
  };

  const viewProfile = () => {
    if (agent) push(wsPaths.agentDetail(agent.id));
  };

  const doDelete = () => {
    setConfirmDelete(false);
    setActiveSession(null);
    deleteSession.mutate(session.id);
  };

  const doArchive = () => setArchived.mutate({ sessionId: session.id, archived: true });
  const doUnarchive = () => setArchived.mutate({ sessionId: session.id, archived: false });

  return (
    <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      {agent ? (
        <ActorAvatar actorType="agent" actorId={agent.id} size={30} enableHoverCard showStatusDot />
      ) : (
        <span className="size-[30px] shrink-0" />
      )}

      <div className="min-w-0 flex-1">
        {editing ? (
          <input
            ref={inputRef}
            value={draft}
            maxLength={200}
            aria-label={t(($) => $.header.rename)}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                commitRename();
              } else if (e.key === "Escape") {
                e.preventDefault();
                setEditing(false);
              }
            }}
            className="w-full rounded-sm bg-background px-1 py-0.5 text-sm font-semibold outline-none ring-1 ring-border focus-visible:ring-brand"
          />
        ) : (
          <button
            type="button"
            onClick={startRename}
            title={t(($) => $.header.rename)}
            className="block max-w-full truncate text-left text-sm font-semibold text-foreground outline-none hover:text-foreground/80 focus-visible:text-foreground/80"
          >
            {title}
          </button>
        )}
        {agent && (
          <div className="truncate text-xs text-muted-foreground">
            {agent.name}
            {agent.description ? ` · ${agent.description}` : ""}
          </div>
        )}
      </div>

      <DropdownMenu>
        <DropdownMenuTrigger
          render={<Button variant="ghost" size="icon-sm" className="text-muted-foreground" />}
        >
          <MoreHorizontal className="h-4 w-4" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-auto">
          <DropdownMenuItem onClick={startRename}>
            <Pencil className="h-4 w-4" />
            {t(($) => $.header.rename)}
          </DropdownMenuItem>
          {agent && (
            <DropdownMenuItem onClick={viewProfile}>
              <UserRound className="h-4 w-4" />
              {t(($) => $.header.view_profile)}
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          {isArchived ? (
            <>
              <DropdownMenuItem onClick={doUnarchive}>
                <ArchiveRestore className="h-4 w-4" />
                {t(($) => $.header.unarchive)}
              </DropdownMenuItem>
              {/* Hard delete is offered only once a chat is archived. */}
              <DropdownMenuItem variant="destructive" onClick={() => setConfirmDelete(true)}>
                <Trash2 className="h-4 w-4" />
                {t(($) => $.header.delete)}
              </DropdownMenuItem>
            </>
          ) : (
            <DropdownMenuItem onClick={doArchive}>
              <Archive className="h-4 w-4" />
              {t(($) => $.header.archive)}
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.session_history.delete_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>{title}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.session_history.delete_dialog.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              onClick={doDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t(($) => $.session_history.delete_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
