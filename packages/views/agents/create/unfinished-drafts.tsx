"use client";

import { useState } from "react";
import { ChevronRight, MessageSquare } from "lucide-react";
import { decodeBuilderInput, stripBuilderDraft } from "@multica/core/agents";
import type { AgentBuilderSessionSummary } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Offers the conversations this user started and left, on the one screen that
 * would otherwise silently start a third one.
 *
 * A single unfinished draft opens directly — a chooser listing one item is a
 * question with one answer. Several open a dialog, because picking between them
 * needs to show what each one is about, which does not fit in a banner.
 */
export function UnfinishedDraftsBanner({
  sessions,
  onResume,
}: {
  sessions: AgentBuilderSessionSummary[];
  onResume: (sessionId: string) => void;
}) {
  const { t } = useT("agents");
  const [picking, setPicking] = useState(false);

  if (sessions.length === 0) return null;

  const openOrPick = () => {
    const only = sessions[0];
    if (sessions.length === 1 && only) {
      onResume(only.session_id);
      return;
    }
    setPicking(true);
  };

  return (
    <>
      <button
        type="button"
        onClick={openOrPick}
        className={cn(
          "mb-5 flex w-full items-center gap-3 rounded-lg border bg-muted/40 px-4 py-3 text-left transition-colors",
          "hover:border-primary/40 hover:bg-accent/30",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        )}
      >
        <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-background text-muted-foreground">
          <MessageSquare className="size-4" aria-hidden="true" />
        </span>
        <span className="min-w-0 flex-1 text-body">
          {t(($) => $.creation_studio.drafts.banner, {
            count: sessions.length,
          })}
        </span>
        <ChevronRight
          className="size-4 shrink-0 text-muted-foreground"
          aria-hidden="true"
        />
      </button>

      <Dialog open={picking} onOpenChange={setPicking}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t(($) => $.creation_studio.drafts.title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.creation_studio.drafts.pick_hint)}
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-96 space-y-2 overflow-y-auto">
            {sessions.map((session) => (
              <button
                key={session.session_id}
                type="button"
                onClick={() => {
                  setPicking(false);
                  onResume(session.session_id);
                }}
                className={cn(
                  "flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors",
                  "hover:border-primary/40 hover:bg-accent/30",
                  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                )}
              >
                <span className="min-w-0 flex-1">
                  <span className="flex items-baseline gap-2">
                    <span className="truncate text-body font-medium">
                      {draftTitle(session) ||
                        t(($) => $.creation_studio.drafts.untitled)}
                    </span>
                    {session.last_message_at ? (
                      <DraftTimestamp at={session.last_message_at} />
                    ) : null}
                  </span>
                  {/* Two lines: enough to recognise which conversation this is,
                      short enough that a long reply cannot push the next row
                      off the dialog. */}
                  <span className="mt-1 line-clamp-2 block text-caption leading-5 text-muted-foreground">
                    {draftPreview(session)}
                  </span>
                </span>
              </button>
            ))}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

function DraftTimestamp({ at }: { at: string }) {
  const timeAgo = useTimeAgo();
  return (
    <span className="ml-auto shrink-0 text-micro text-muted-foreground">
      {timeAgo(at)}
    </span>
  );
}

/**
 * What the user calls this draft. The stored configuration's name is the only
 * candidate — the server-side title is the same string on every row and would
 * make the list unreadable.
 */
function draftTitle(
  session: Pick<AgentBuilderSessionSummary, "draft">,
): string {
  return session.draft?.name?.trim() ?? "";
}

/**
 * The stored message is still in the builder wire format — the user side is a
 * JSON envelope carrying the whole draft, the assistant side ends in an
 * `<agent_draft>` block. Shown raw it is a wall of JSON, so both sides are
 * decoded through the same helpers the conversation itself uses.
 */
export function draftPreview(
  session: Pick<
    AgentBuilderSessionSummary,
    "last_message_content" | "last_message_role"
  >,
): string {
  const content = session.last_message_content;
  if (!content) return "";
  return session.last_message_role === "user"
    ? decodeBuilderInput(content)
    : stripBuilderDraft(content);
}
