"use client";

import type { RuntimeUnavailableModel } from "@multica/core/types";

/**
 * The "your CLI is behind" section both model pickers render under their
 * selectable rows.
 *
 * These rows exist so a missing model reads as what it is. Claude Code stops
 * offering a model its own version cannot run, and simply dropping it from the
 * list would tell the user Multica does not support the model when the truth is
 * that their CLI does not yet — with the fix one `claude update` away. The
 * reason is the runtime's own copy ("Update to 2.1.255+ to use Fable 5.1"), so
 * the guidance stays correct without Multica tracking version floors (MUL-6961).
 *
 * Rendered as plain text, never as a control: nothing here is selectable, and
 * the component takes no click handler so no caller can make it one. That is
 * also why these arrive in their own list rather than as flagged entries in the
 * model array — a flag has to be honoured by every consumer that renders a row,
 * and an already-installed client cannot honour a flag it has never heard of.
 */
export function UnavailableModelsNote({
  models,
  title,
}: {
  models: readonly RuntimeUnavailableModel[];
  title: string;
}) {
  if (models.length === 0) return null;

  return (
    <div className="mt-1 border-t border-border pt-2">
      <div className="px-3 pb-1 text-caption font-medium uppercase tracking-wide text-muted-foreground">
        {title}
      </div>
      {models.map((m) => (
        <div key={m.id} className="px-3 py-1.5">
          <div className="truncate text-body text-muted-foreground">
            {m.label}
          </div>
          {m.reason && (
            // Not truncated: the reason names the version to upgrade to, and a
            // clipped remedy is no remedy.
            <div className="text-caption text-muted-foreground">{m.reason}</div>
          )}
        </div>
      ))}
    </div>
  );
}
