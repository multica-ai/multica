"use client";

import { useState } from "react";
import { CornerDownRight } from "@multica/ui/icons";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../common/actor-avatar";
import { useT } from "../i18n";
import { BriefBlock } from "./handoff-form";

/**
 * The handoff half of a status-change or project-entry timeline row: who the
 * workflow step handed the issue to, the enqueue prediction and its reason,
 * and the brief. Actual enqueue/execution is recorded by runs.
 * Renders nothing for an ordinary status change.
 */
export function TimelineHandoff({ details }: { details: Record<string, string> }) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const [open, setOpen] = useState(false);
  const type = details.handoff_to_type;
  const id = details.handoff_to_id;
  if (!type || !id) return null;
  const note = details.handoff_note ?? "";
  const predicted = details.handoff_run === "true";
  const name = getActorName(type, id);
  const reason = details.handoff_run_reason;
  const explanation = (() => {
    switch (reason) {
      case "enqueue_allowed": return t(($) => $.workflows.timeline.run_predicted);
      case "pending_task": return t(($) => $.workflows.timeline.pending_run);
      case "pending_check_failed":
      case "agent_check_failed": return t(($) => $.workflows.timeline.check_failed);
      case "no_runtime": return t(($) => $.workflows.timeline.no_runtime);
      case "suppressed": return t(($) => $.workflows.timeline.run_suppressed);
      case "agent_unavailable": return t(($) => $.workflows.timeline.agent_unavailable);
      case "invocation_not_allowed": return t(($) => $.workflows.timeline.not_allowed);
      case "active_self_assignment":
      case "not_triggered": return t(($) => $.workflows.timeline.no_run);
      case undefined:
      case "": return predicted ? t(($) => $.workflows.timeline.run_predicted) : null;
      default: return t(($) => $.workflows.timeline.unknown_reason);
    }
  })();
  return (
    <div className="mt-1 space-y-2">
      <div className="flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
        <CornerDownRight aria-hidden className="size-3 shrink-0" />
        <span>{t(($) => $.workflows.timeline.handed_to)}</span>
        <ActorAvatar actorType={type} actorId={id} size="xs" profileLink={false} />
        <span className="font-medium text-foreground">{name}</span>
        {type === "member" ? (
          <span>{t(($) => $.workflows.timeline.member_no_run)}</span>
        ) : (
          <>
            {explanation && <span>{explanation}</span>}
            {predicted && note && (
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
        )}
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
