"use client";

import { FolderOpen, Network, Sparkles, Users } from "@multica/ui/icons";
import { Command as CommandPrimitive } from "cmdk";
import { createShortcutChord } from "@multica/core/shortcuts";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { ShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";
import { usePalette } from "../palette-context";
import { AgentPresenceText } from "../result-rows";
import { PaletteGroup, ROW_CLASS, ROW_ICON_CLASS } from "../rows";
import type { DelegateActor } from "../types";
import type { AgentEntry } from "../use-actor-directory";
import type { DelegateAttachments } from "./use-delegate";

const ENTER = createShortcutChord("Enter");

function AttachmentChip({ icon: Icon, children }: { icon: typeof FolderOpen; children: string }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
      <Icon className="size-3 shrink-0" />
      <span className="truncate">{children}</span>
    </span>
  );
}

/**
 * "Hand to agent": shown above the results when the input reads as an
 * instruction. The first row creates the issue from the input as typed and
 * assigns it; it previews who and what context, never guessed fields.
 */
export function DelegateGroup({
  prompt,
  actor,
  agentEntry,
  attachments,
  onSubmit,
}: {
  prompt: string;
  actor: DelegateActor;
  agentEntry: AgentEntry | null;
  attachments: DelegateAttachments;
  onSubmit: (actor: DelegateActor) => void;
}) {
  const { t } = useT("search");
  const palette = usePalette();
  const hasAttachments = !!attachments.project || !!attachments.parent;

  return (
    <PaletteGroup
      heading={
        <>
          <Sparkles className="size-3 text-brand" />
          <span className="text-brand">{t(($) => $.delegate.heading)}</span>
        </>
      }
    >
      <CommandPrimitive.Item
        value="delegate:submit"
        onSelect={() => {
          palette.close();
          onSubmit(actor);
        }}
        className={cn(ROW_CLASS, "items-start py-1.5")}
      >
        <ActorAvatar
          actorType={actor.type}
          actorId={actor.id}
          size="sm"
          profileLink={false}
          showStatusDot={actor.type === "agent"}
          className="mt-0.5 shrink-0"
        />
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="truncate">
            {t(($) => $.delegate.create_and_assign, { name: actor.name })}
          </span>
          <span className="flex min-w-0 items-center gap-3">
            {actor.type === "agent" && agentEntry && (
              <AgentPresenceText
                availability={agentEntry.availability}
                runtimeLabel={agentEntry.runtimeLabel}
                className="shrink-0"
              />
            )}
            {actor.type === "squad" && (
              <span className="shrink-0 text-caption text-muted-foreground">
                {t(($) => $.delegate.squad)}
              </span>
            )}
            {hasAttachments && (
              <span className="flex min-w-0 items-center gap-2" aria-label={t(($) => $.delegate.context_aria)}>
                {attachments.project && (
                  <AttachmentChip icon={FolderOpen}>{attachments.project.title}</AttachmentChip>
                )}
                {attachments.parent && (
                  <AttachmentChip icon={Network}>
                    {t(($) => $.delegate.parent, { identifier: attachments.parent.identifier })}
                  </AttachmentChip>
                )}
              </span>
            )}
          </span>
        </div>
        <ShortcutKeycaps shortcut={ENTER} decorative className="mt-0.5 shrink-0" />
      </CommandPrimitive.Item>
      <CommandPrimitive.Item
        value="delegate:choose"
        onSelect={() =>
          palette.push({
            page: { type: "delegate-target", prompt },
            crumbs: [t(($) => $.delegate.heading), t(($) => $.delegate.pick_crumb)],
          })
        }
        className={ROW_CLASS}
      >
        <Users className={ROW_ICON_CLASS} />
        <span className="min-w-0 flex-1 truncate">{t(($) => $.delegate.pick_another)}</span>
      </CommandPrimitive.Item>
    </PaletteGroup>
  );
}
