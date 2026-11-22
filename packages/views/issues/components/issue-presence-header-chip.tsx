"use client";

import { memo, useEffect, useReducer, useState, type CSSProperties, type ReactNode } from "react";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import type { DocCollaborator } from "../../docs/components/use-doc-collab";
import { useT } from "../../i18n";

/** Heads shown in the chip; a narrow header shows one fewer. */
const HEADS = 3;
/** People listed under "Viewing" before the rest are only named. */
const LISTED_VIEWERS = 6;
/** Names after the listed viewers, before the list ends in an ellipsis. */
const NAMED_REST = 4;

/**
 * Who else has the issue open, in the issue header beside the agent chip.
 * People with a caret in the description come first, ringed in their caret's
 * colour so the chip matches the carets in the text. Hovering lists everyone,
 * the editors with the section they are in; clicking an editor scrolls to
 * their caret. Renders nothing when the reader is alone.
 */
export const IssuePresenceHeaderChip = memo(function IssuePresenceHeaderChip({
  collaborators,
  locate,
  watchCarets,
  onJump,
}: {
  /** Everyone but the reader. */
  collaborators: readonly DocCollaborator[];
  locate: (key: number) => string | null;
  watchCarets: (listener: () => void) => () => void;
  onJump: (collaborator: DocCollaborator) => void;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  // Where each editor is changes with every caret move; only an open list
  // follows it.
  const [, refresh] = useReducer((n: number) => n + 1, 0);
  useEffect(() => {
    if (!open) return;
    let frame = 0;
    const stop = watchCarets(() => {
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        refresh();
      });
    });
    return () => {
      stop();
      cancelAnimationFrame(frame);
    };
  }, [open, watchCarets]);

  if (collaborators.length === 0) return null;
  const editing = collaborators.filter((c) => c.editing);
  const viewing = collaborators.filter((c) => !c.editing);
  const ordered = [...editing, ...viewing];
  const label = t(($) => $.presence.viewing, { count: collaborators.length });
  const rest = viewing.slice(LISTED_VIEWERS);
  const separator = t(($) => $.presence.name_separator);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        openOnHover
        delay={150}
        closeDelay={200}
        render={
          <button
            type="button"
            aria-label={label}
            className="flex h-9 min-w-9 items-center justify-center gap-1.5 rounded-md px-2 text-muted-foreground outline-none transition-colors hover:bg-accent/60 focus-visible:ring-2 focus-visible:ring-ring md:h-7 md:min-w-0 md:justify-start md:px-1.5"
          />
        }
      >
        <span className="inline-flex items-center">
          {ordered.slice(0, HEADS).map((c, i) => (
            <span
              key={c.key}
              className={cn(
                "inline-flex rounded-full ring-2",
                !c.editing && "ring-background",
                i > 0 && "-ml-1",
                i === HEADS - 1 && "@max-[40rem]/issue-header:hidden",
              )}
              style={c.editing ? ({ "--tw-ring-color": c.color } as CSSProperties) : undefined}
            >
              <ActorAvatar actorType={c.kind} actorId={c.id} name={c.name} size="sm" profileLink={false} />
            </span>
          ))}
        </span>
        <span className="whitespace-nowrap text-caption @max-[40rem]/issue-header:hidden">{label}</span>
        <span className="hidden text-caption tabular-nums @max-[40rem]/issue-header:inline">
          {collaborators.length}
        </span>
      </PopoverTrigger>
      <PopoverContent align="end" className="max-h-(--available-height) w-72 overflow-y-auto">
        {editing.length > 0 && (
          <PresenceGroup title={t(($) => $.presence.editing)} count={editing.length}>
            {editing.map((c) => {
              const section = locate(c.key);
              return (
                <button
                  key={c.key}
                  type="button"
                  data-presence-row={c.name}
                  className="flex items-center gap-2 rounded-md px-1.5 py-1 text-left outline-none hover:bg-accent/60 focus-visible:bg-accent/60"
                  onClick={() => {
                    setOpen(false);
                    onJump(c);
                  }}
                >
                  <PresenceAvatar collaborator={c} />
                  <span className="min-w-0 flex-1 truncate text-foreground">{c.name}</span>
                  {section && (
                    <span className="min-w-0 max-w-[50%] shrink-0 truncate text-caption text-muted-foreground">
                      {t(($) => $.presence.where, { section })}
                    </span>
                  )}
                </button>
              );
            })}
          </PresenceGroup>
        )}
        {viewing.length > 0 && (
          <PresenceGroup title={t(($) => $.presence.watching)} count={viewing.length}>
            {viewing.slice(0, LISTED_VIEWERS).map((c) => (
              <div key={c.key} data-presence-row={c.name} className="flex items-center gap-2 px-1.5 py-1">
                <PresenceAvatar collaborator={c} />
                <span className="min-w-0 flex-1 truncate text-foreground">{c.name}</span>
              </div>
            ))}
            {rest.length > 0 && (
              <div className="px-1.5 pt-1 text-caption text-muted-foreground">
                {t(($) => $.presence.more, {
                  count: rest.length,
                  names:
                    rest.slice(0, NAMED_REST).map((c) => c.name).join(separator) +
                    (rest.length > NAMED_REST ? "..." : ""),
                })}
              </div>
            )}
          </PresenceGroup>
        )}
        {editing.length > 0 && (
          <div className="border-t border-border px-1.5 pt-2 text-caption text-muted-foreground">
            {t(($) => $.presence.jump_hint)}
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
});

function PresenceGroup({ title, count, children }: { title: string; count: number; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <div className="px-1.5 text-caption font-medium text-muted-foreground">
        {title} · {count}
      </div>
      {children}
    </div>
  );
}

function PresenceAvatar({ collaborator: c }: { collaborator: DocCollaborator }) {
  return (
    <span
      className={cn("inline-flex shrink-0 rounded-full ring-2", !c.editing && "ring-background")}
      style={c.editing ? ({ "--tw-ring-color": c.color } as CSSProperties) : undefined}
    >
      <ActorAvatar actorType={c.kind} actorId={c.id} name={c.name} size="sm" profileLink={false} />
    </span>
  );
}
