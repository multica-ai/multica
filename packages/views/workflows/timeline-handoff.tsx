"use client";

import { useState } from "react";
import { CornerDownRight } from "lucide-react";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../common/actor-avatar";
import { useT } from "../i18n";
import { BriefBlock } from "./workflow-editor-page";

/**
 * The handoff half of a status-change timeline entry (MUL-7420): who the
 * workflow step handed the issue to, whether their run started, and the brief
 * that run received. Renders nothing for an ordinary status change.
 */
export function TimelineHandoff({ details }: { details: Record<string, string> }) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const [open, setOpen] = useState(false);
  const type = details.handoff_to_type;
  const id = details.handoff_to_id;
  if (!type || !id) return null;
  const note = details.handoff_note ?? "";
  const ran = details.handoff_run === "true";
  const name = getActorName(type, id);
  return (
    <div className="mt-1 space-y-2">
      <div className="flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
        <CornerDownRight aria-hidden className="size-3 shrink-0" />
        <span>{t(($) => $.workflows.timeline.handed_to)}</span>
        <ActorAvatar actorType={type} actorId={id} size="xs" profileLink={false} />
        <span className="font-medium text-foreground">{name}</span>
        {type === "member" ? (
          <span>{t(($) => $.workflows.timeline.member_no_run)}</span>
        ) : ran ? (
          <>
            <span>{t(($) => $.workflows.timeline.run_started)}</span>
            {note && (
              <button
                type="button"
                onClick={() => setOpen((v) => !v)}
                aria-expanded={open}
                className="text-brand hover:underline"
              >
                {open ? t(($) => $.workflows.timeline.hide_brief) : t(($) => $.workflows.timeline.show_brief)}
              </button>
            )}
          </>
        ) : null}
      </div>
      {open && note && (
        <div className="space-y-1.5">
          <div className="flex items-center gap-1.5 text-caption text-muted-foreground">
            <ActorAvatar actorType={type} actorId={id} size="xs" profileLink={false} />
            <span className="font-medium text-foreground">{name}</span>
            {t(($) => $.workflows.timeline.brief_header)}
          </div>
          <BriefBlock brief={note} />
        </div>
      )}
    </div>
  );
}
