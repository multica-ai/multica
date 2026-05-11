"use client";

// CEREBRO-PATCH(chat-session-header): JEH-799 — header bar for the active
// chat session. Shows the session title (click to rename), an archive
// toggle, and a "convert to issue" action.

import { useEffect, useRef, useState } from "react";
import { Archive, ArchiveRestore, Check, MoreHorizontal, FileText, Pencil, X } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
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
import { useNavigation } from "@multica/views/navigation";
import { chatSessionUsageOptions } from "@multica/core/chat/queries";
import type { ChatSession } from "@multica/core/types";
import {
  useUpdateChatSession,
  useArchiveActiveChatSession,
  useConvertChatSessionToIssue,
} from "../../mutations";
import { useCerebroChatHeaderStrings } from "../../strings";

type Pending = "archive" | "unarchive" | "convert" | null;

/**
 * Header bar shown above the chat session body — the row with the editable
 * session title and the actions menu (archive / unarchive / convert-to-issue).
 *
 * The "new chat" empty state (no `session`) renders nothing — there's no
 * session to act on yet, and the chat-window already shows the agent picker
 * + starter prompts in that mode.
 */
export function ChatSessionHeader({ session }: { session: ChatSession | null }) {
  const s = useCerebroChatHeaderStrings();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const update = useUpdateChatSession();
  const archive = useArchiveActiveChatSession();
  const convert = useConvertChatSessionToIssue();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [pending, setPending] = useState<Pending>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  // JEH-736 — token + cost spend for the active session. WS invalidation
  // (use-realtime-sync) refreshes this on chat:done / task:completed /
  // task:failed so the chip tracks live spend without polling.
  const { data: usage } = useQuery({
    ...chatSessionUsageOptions(session?.id ?? ""),
    enabled: !!session?.id,
  });

  // Drop edit mode + clear pending dialog if the session changes underneath us.
  useEffect(() => {
    setEditing(false);
    setPending(null);
  }, [session?.id]);

  if (!session) return null;

  const title = session.title?.trim() || s.untitled;
  const isArchived = session.status === "archived";

  const startEditing = () => {
    setDraft(session.title?.trim() ?? "");
    setEditing(true);
    requestAnimationFrame(() => {
      inputRef.current?.focus();
      inputRef.current?.select();
    });
  };

  const commitEdit = () => {
    const next = draft.trim();
    if (!next || next === session.title) {
      setEditing(false);
      return;
    }
    update.mutate(
      { sessionId: session.id, title: next },
      { onSettled: () => setEditing(false) },
    );
  };

  const cancelEdit = () => {
    setDraft("");
    setEditing(false);
  };

  const onArchiveConfirm = () => {
    archive.archive(session.id);
    setPending(null);
  };

  const onUnarchiveConfirm = () => {
    update.mutate(
      { sessionId: session.id, status: "active" },
      { onSettled: () => setPending(null) },
    );
  };

  const onConvertConfirm = () => {
    convert.mutate(session.id, {
      onSuccess: (resp) => {
        setPending(null);
        toast.success(s.toast_converted);
        router.push(wsPaths.issueDetail(resp.identifier));
      },
      onError: () => {
        setPending(null);
        toast.error(s.toast_convert_failed);
      },
    });
  };

  return (
    <div className="flex items-center justify-between gap-2 border-b px-4 py-2 bg-background/60">
      {editing ? (
        <div className="flex flex-1 items-center gap-1 min-w-0">
          <Input
            ref={inputRef}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                commitEdit();
              } else if (e.key === "Escape") {
                e.preventDefault();
                cancelEdit();
              }
            }}
            onBlur={commitEdit}
            disabled={update.isPending}
            className="h-7 flex-1 text-sm"
            aria-label={s.edit_title_aria}
          />
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={commitEdit}
                  disabled={update.isPending}
                />
              }
            >
              <Check />
            </TooltipTrigger>
            <TooltipContent side="bottom">{s.save_title}</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={cancelEdit}
                  disabled={update.isPending}
                />
              }
            >
              <X />
            </TooltipTrigger>
            <TooltipContent side="bottom">{s.cancel_edit}</TooltipContent>
          </Tooltip>
        </div>
      ) : (
        <button
          type="button"
          onClick={startEditing}
          className="group flex flex-1 items-center gap-1.5 min-w-0 rounded-md px-1.5 py-1 -ml-1.5 text-left transition-colors hover:bg-accent"
          aria-label={s.edit_title_aria}
        >
          <span className="truncate text-sm font-semibold">{title}</span>
          {isArchived && (
            <span className="shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
              {s.archived_badge}
            </span>
          )}
          <Pencil className="size-3 shrink-0 text-muted-foreground/0 transition-colors group-hover:text-muted-foreground" />
        </button>
      )}

      <div className="flex shrink-0 items-center gap-1">
        {/* JEH-736 — session price chip with token breakdown tooltip.
            Hidden until the session has logged at least one task so a brand-new
            session doesn't carry a misleading "$0.00" badge. */}
        {usage && usage.task_count > 0 && (
          <Tooltip>
            <TooltipTrigger
              render={
                <span
                  aria-label={s.session_price_aria}
                  className="rounded-md bg-accent/60 px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground"
                >
                  {formatChatCost(usage.cost_cents)}
                </span>
              }
            />
            <TooltipContent side="bottom" className="text-xs">
              <div className="font-medium">{s.session_price_label}: {formatChatCost(usage.cost_cents)}</div>
              <div className="mt-1 grid grid-cols-[auto_1fr] gap-x-2 text-[11px] text-muted-foreground">
                <span>{s.session_token_breakdown_input}</span>
                <span className="text-right">{formatChatTokens(usage.total_input_tokens)}</span>
                <span>{s.session_token_breakdown_output}</span>
                <span className="text-right">{formatChatTokens(usage.total_output_tokens)}</span>
                <span>{s.session_token_breakdown_cache_read}</span>
                <span className="text-right">{formatChatTokens(usage.total_cache_read_tokens)}</span>
                <span>{s.session_token_breakdown_cache_write}</span>
                <span className="text-right">{formatChatTokens(usage.total_cache_write_tokens)}</span>
                <span>{s.session_token_breakdown_runs}</span>
                <span className="text-right">{usage.task_count}</span>
              </div>
            </TooltipContent>
          </Tooltip>
        )}
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground"
                onClick={() => setPending("convert")}
              />
            }
          >
            <FileText />
          </TooltipTrigger>
          <TooltipContent side="bottom">{s.convert_to_issue}</TooltipContent>
        </Tooltip>
        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger
              render={
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      className="text-muted-foreground"
                      aria-label={s.more_actions}
                    />
                  }
                />
              }
            >
              <MoreHorizontal />
            </TooltipTrigger>
            <TooltipContent side="bottom">{s.more_actions}</TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="end" className="min-w-44">
            {isArchived ? (
              <DropdownMenuItem onClick={() => setPending("unarchive")}>
                <ArchiveRestore className="size-3.5" />
                <span>{s.unarchive}</span>
              </DropdownMenuItem>
            ) : (
              <DropdownMenuItem onClick={() => setPending("archive")}>
                <Archive className="size-3.5" />
                <span>{s.archive}</span>
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <AlertDialog
        open={pending === "archive"}
        onOpenChange={(o) => {
          if (!o && !archive.isPending) setPending(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{s.archive_dialog_title}</AlertDialogTitle>
            <AlertDialogDescription>{s.archive_dialog_description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={archive.isPending}>{s.cancel}</AlertDialogCancel>
            <AlertDialogAction onClick={onArchiveConfirm} disabled={archive.isPending}>
              {archive.isPending ? s.archive_confirming : s.archive_confirm}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={pending === "unarchive"}
        onOpenChange={(o) => {
          if (!o && !update.isPending) setPending(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{s.unarchive_dialog_title}</AlertDialogTitle>
            <AlertDialogDescription>{s.unarchive_dialog_description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={update.isPending}>{s.cancel}</AlertDialogCancel>
            <AlertDialogAction onClick={onUnarchiveConfirm} disabled={update.isPending}>
              {s.unarchive_confirm}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={pending === "convert"}
        onOpenChange={(o) => {
          if (!o && !convert.isPending) setPending(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{s.convert_dialog_title}</AlertDialogTitle>
            <AlertDialogDescription>{s.convert_dialog_description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={convert.isPending}>{s.cancel}</AlertDialogCancel>
            <AlertDialogAction onClick={onConvertConfirm} disabled={convert.isPending}>
              {convert.isPending ? s.convert_confirming : s.convert_confirm}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// JEH-736 — keep the formatters local so this component does not depend
// on @multica/views (which would create a circular package edge).
function formatChatCost(cents: number): string {
  const usd = cents / 100;
  if (usd === 0) return "$0.00";
  if (usd < 0.01) return "<$0.01";
  return `$${usd.toFixed(2)}`;
}

function formatChatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}
