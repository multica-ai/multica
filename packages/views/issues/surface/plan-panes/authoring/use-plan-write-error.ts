"use client";

import { useCallback, useState } from "react";
import { classifyPlanWriteError } from "@multica/core/project-plan/errors";
import { useT } from "../../../../i18n";

export interface PlanWriteErrorNotice {
  /** Rendered as a conflict, not a generic failure, when true. */
  conflict: boolean;
  message: string;
}

/**
 * Turns a failed plan write into copy a person can act on, and keeps the
 * dialog open so they can.
 *
 * The rule this exists to enforce: a 409 must read
 * as a conflict. The service's own messages are already specific — "another
 * phase already uses this position", "this project already has an active
 * plan" — so they are preferred verbatim over any local copy. Local strings
 * are the fallback for the case where the server sent a bare status with no
 * message, and they are per-kind rather than one generic "something went
 * wrong", because a conflict and a dropped connection need different actions
 * from the reader.
 */
export function usePlanWriteError() {
  const { t } = useT("issues");
  const [notice, setNotice] = useState<PlanWriteErrorNotice | null>(null);

  const report = useCallback(
    (err: unknown) => {
      const classified = classifyPlanWriteError(err);
      const fallback = (() => {
        switch (classified.kind) {
          case "active_plan_exists":
            return t(($) => $.plan.authoring.error.active_plan_exists);
          case "version_conflict":
          case "position_conflict":
            return t(($) => $.plan.authoring.error.conflict);
          case "issue_already_linked":
            return t(($) => $.plan.authoring.error.issue_already_linked);
          case "not_found":
            return t(($) => $.plan.authoring.error.not_found);
          case "not_active":
            return t(($) => $.plan.authoring.error.not_active);
          case "invalid":
            return t(($) => $.plan.authoring.error.invalid);
          case "disabled":
            return t(($) => $.plan.authoring.error.disabled);
          default:
            return t(($) => $.plan.authoring.error.unavailable);
        }
      })();
      setNotice({ conflict: classified.conflict, message: classified.message || fallback });
    },
    [t],
  );

  const clear = useCallback(() => setNotice(null), []);

  return { notice, report, clear };
}
