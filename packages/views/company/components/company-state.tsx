"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { canGovern, companyStateOptions, useSetCompanyState, type CompanyState } from "@multica/core/governance";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const DOT_CLASS: Record<CompanyState, string> = {
  running: "bg-success",
  paused: "bg-warning",
  quiet: "bg-muted-foreground/50",
  unknown: "bg-muted-foreground/50",
};

/**
 * The company's state in the Company header: a chip that names it, and the
 * Pause or Resume control for owners and admins. Nothing shows until the state
 * is known, so the header never claims a state it has not read.
 */
export function CompanyState() {
  const { t } = useT("governance");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const company = useQuery(companyStateOptions(wsId));
  const setState = useSetCompanyState(wsId);

  const info = company.data;
  if (!info) return null;

  const state = info.state;
  const paused = state === "paused";
  const busy = setState.isPending;

  return (
    <>
      <span
        role="status"
        data-company-state={state}
        className="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border px-2 text-caption text-muted-foreground"
      >
        <span aria-hidden className={cn("size-1.5 shrink-0 rounded-full", DOT_CLASS[state])} />
        {t(($) => $.controls.state[state])}
      </span>
      {canGovern(role) ? (
        <Button
          type="button"
          size="sm"
          variant={paused ? "brand" : "outline"}
          disabled={busy}
          aria-busy={busy}
          onClick={() =>
            setState.mutate(
              { state: paused ? "running" : "paused" },
              { onError: () => toast.error(t(($) => $.controls.state_save_failed)) },
            )
          }
        >
          {paused ? t(($) => $.controls.resume) : t(($) => $.controls.pause)}
        </Button>
      ) : null}
    </>
  );
}
