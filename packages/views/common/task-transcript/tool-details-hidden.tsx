"use client";

import { Lock } from "@multica/ui/icons";
import { useT } from "../../i18n";
import type { TraceCallStep } from "./build-steps";

/** The agent restricts its tool details and the server withheld this call's. */
export function callDetailsHidden(step: TraceCallStep): boolean {
  return step.call?.detailsHidden === true || step.result?.detailsHidden === true;
}

/** Stands in for a call's argument when the viewer may not read it. */
export function HiddenDetails() {
  const { t } = useT("agents");
  return (
    <span className="inline-flex items-center gap-1 font-sans text-faint-foreground">
      <Lock aria-hidden className="h-3 w-3" />
      {t(($) => $.transcript.tool_details_hidden_value)}
    </span>
  );
}

/** Stands in for a call's input and result when the viewer may not read them. */
export function HiddenDetailsNotice({ className }: { className?: string }) {
  const { t } = useT("agents");
  return (
    <div className={className}>
      <p className="text-caption font-medium text-foreground">
        {t(($) => $.transcript.tool_details_hidden_title)}
      </p>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.transcript.tool_details_hidden_body)}
      </p>
    </div>
  );
}
