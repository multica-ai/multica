"use client";

import type { InitiativeEvent } from "@multica/core/pipeline";
import { useActorName } from "@multica/core/workspace/hooks";
import { useT, useTimeAgo } from "../i18n";

const KNOWN_EVENT_KINDS = [
  "created",
  "advanced",
  "sent_back",
  "shelved",
  "resumed",
  "closed",
  "forced",
  "attested",
  "artifact_waived",
] as const;

/** The stage timeline, as the server recorded it: every move, attestation and waiver, with its actor and note. */
export function InitiativeTimeline({
  events,
  stageName,
}: {
  events: readonly InitiativeEvent[];
  /** The display name of a stage key, falling back to the key itself. */
  stageName: (key: string | null) => string;
}) {
  const { t } = useT("pipeline");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();

  return (
    <section aria-labelledby="initiative-timeline" className="flex flex-col gap-2">
      <h3 id="initiative-timeline" className="text-caption font-medium text-muted-foreground">
        {t(($) => $.panel.timeline)}
      </h3>
      {events.length === 0 ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.event.none)}</p>
      ) : (
        <ol className="flex flex-col gap-3">
          {events.map((event) => {
            const kind = (KNOWN_EVENT_KINDS as readonly string[]).includes(event.kind)
              ? (event.kind as (typeof KNOWN_EVENT_KINDS)[number])
              : "unknown";
            const moved = event.from_stage || event.to_stage;
            return (
              <li key={event.id} className="flex flex-col gap-1 border-l-2 pl-3">
                <div className="flex flex-wrap items-baseline gap-x-2 text-body">
                  <span className="font-medium">{t(($) => $.event[kind])}</span>
                  {moved ? (
                    <span className="text-caption text-muted-foreground">
                      {t(($) => $.event.move, {
                        from: stageName(event.from_stage),
                        to: stageName(event.to_stage),
                      })}
                    </span>
                  ) : null}
                </div>
                <span className="text-caption text-muted-foreground">
                  {getActorName(event.actor_type, event.actor_id ?? "")}
                  {" · "}
                  {timeAgo(event.created_at)}
                </span>
                {event.note ? <p className="whitespace-pre-line text-body">{event.note}</p> : null}
              </li>
            );
          })}
        </ol>
      )}
    </section>
  );
}
