"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  buildOfficeSetupRequest,
  DEFAULT_OFFICE_SETUP_DOCS_PATH,
  isOfficeSetupReady,
  officeSetupOptions,
  parseOfficeSetupRefusal,
  useSetupOffice,
  type OfficeSetupRefusal,
} from "@multica/core/office";
import { SQUAD_TEMPLATES, useSquadQuickstart, type QuickstartOutcome } from "@multica/core/squads";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Check, Circle, ExternalLink, Loader2, RotateCw, Sparkles } from "@multica/ui/icons";
import { docsLocalePrefix } from "../../common/docs-locale";
import { useT } from "../../i18n";
import { DOCS_URL } from "../../layout/help-links";
import { useOfficeAdmin } from "../lib/use-office-admin";

const MARKET_RESEARCH = SQUAD_TEMPLATES.find((template) => template.id === "market_research")!;

/**
 * The Secretary's first-run flow, shown while the company is not set up and, for
 * an owner or admin, until the Market Research squad exists. It reads the
 * readiness checklist; owners and admins run setup and the squad offer, and
 * everyone else reads the status.
 */
export function OfficeSetupCard() {
  const { t, i18n } = useT("issues");
  const wsId = useWorkspaceId();
  const canManage = useOfficeAdmin();
  const readinessQuery = useQuery(officeSetupOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const setup = useSetupOffice(wsId);
  const quickstart = useSquadQuickstart(wsId);
  const [refusal, setRefusal] = useState<OfficeSetupRefusal | null>(null);

  const readiness = readinessQuery.data;
  if (!readiness) return null;

  const ready = isOfficeSetupReady(readiness);
  const marketResearchExists = squads.some((squad) => !squad.archived_at && squad.name === MARKET_RESEARCH.name);
  // Members see the card only while setup is incomplete; the squad offer is for the boss.
  const visible = !ready || (canManage && !marketResearchExists);
  if (!visible) return null;

  const runtimeMissing = !readiness.runtime_online || refusal?.code === "runtime_required";
  const docsPath = refusal?.docs_path || DEFAULT_OFFICE_SETUP_DOCS_PATH;
  const installUrl = `${DOCS_URL}${docsLocalePrefix(i18n.language)}${docsPath}`;
  const secretaryRuntimeId = agents.find((agent) => agent.id === readiness.secretary_id)?.runtime_id ?? "";

  const checks: { done: boolean; label: string }[] = [
    { done: readiness.runtime_online, label: t(($) => $.office.setup_check_runtime) },
    { done: readiness.secretary_id !== null, label: t(($) => $.office.setup_check_secretary) },
    { done: readiness.executive_floor, label: t(($) => $.office.setup_check_floor) },
    { done: readiness.autopilot_daily, label: t(($) => $.office.setup_check_daily) },
    { done: readiness.autopilot_weekly_paused, label: t(($) => $.office.setup_check_weekly) },
  ];

  const refusalMessage = (code: string) => {
    switch (code) {
      case "secretary_name_taken":
        return t(($) => $.office.setup_name_taken);
      case "floor_full":
        return t(($) => $.office.setup_floor_full);
      case "floor_limit":
        return t(($) => $.office.setup_floor_limit);
      default:
        return t(($) => $.office.setup_failed);
    }
  };

  const runSetup = () =>
    setup.mutate(buildOfficeSetupRequest(), {
      onSuccess: () => {
        setRefusal(null);
        toast.success(t(($) => $.office.setup_done));
      },
      onError: (err) => {
        const parsed = err instanceof ApiError ? parseOfficeSetupRefusal(err.body) : null;
        if (parsed?.code === "runtime_required") {
          setRefusal(parsed);
          return;
        }
        toast.error(parsed ? refusalMessage(parsed.code) : t(($) => $.office.setup_failed));
      },
    });

  const offerMarketResearch = () =>
    quickstart.mutate(
      {
        template: MARKET_RESEARCH,
        options: { runtimeId: secretaryRuntimeId, createWorkflow: false },
      },
      {
        onSuccess: (outcome: QuickstartOutcome) => {
          const failed = outcome.squad.status === "failed" || outcome.agents.some((agent) => agent.status === "failed");
          if (failed) toast.error(t(($) => $.office.setup_market_failed));
          else toast.success(t(($) => $.office.setup_market_done));
        },
        onError: () => toast.error(t(($) => $.office.setup_market_failed)),
      },
    );

  return (
    <section
      aria-labelledby="office-setup-title"
      className="mx-4 mb-3 flex shrink-0 flex-col gap-3 rounded-xl border bg-card p-4"
    >
      <div className="flex items-center gap-2">
        <Sparkles className="size-4 text-muted-foreground" />
        <h2 id="office-setup-title" className="text-heading">
          {t(($) => $.office.setup_title)}
        </h2>
      </div>
      <p className="text-caption text-muted-foreground">{t(($) => $.office.setup_intro)}</p>
      <ul className="flex flex-col gap-1.5">
        {checks.map((check) => (
          <li key={check.label} className="flex items-center gap-2 text-body">
            {check.done ? (
              <Check className="size-3.5 text-brand" aria-label={t(($) => $.office.setup_check_done)} />
            ) : (
              <Circle className="size-3.5 text-muted-foreground" aria-hidden />
            )}
            <span className={check.done ? undefined : "text-muted-foreground"}>{check.label}</span>
          </li>
        ))}
      </ul>

      {runtimeMissing ? (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <p className="text-caption text-muted-foreground">{t(($) => $.office.setup_runtime_missing)}</p>
          <a
            href={installUrl}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1 text-caption font-medium text-brand hover:underline"
          >
            {t(($) => $.office.setup_install_guide)}
            <ExternalLink className="size-3" />
          </a>
          <Button type="button" variant="outline" size="sm" disabled={readinessQuery.isFetching} onClick={() => readinessQuery.refetch()}>
            <RotateCw className="size-3.5" />
            {t(($) => $.office.setup_retry)}
          </Button>
        </div>
      ) : null}

      {!ready && !runtimeMissing && !canManage ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.office.setup_read_only)}</p>
      ) : null}

      {!ready && !runtimeMissing && canManage ? (
        <div>
          <Button type="button" disabled={setup.isPending} aria-busy={setup.isPending} onClick={runSetup}>
            {setup.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
            {setup.isPending ? t(($) => $.office.setup_pending) : t(($) => $.office.setup_action)}
          </Button>
        </div>
      ) : null}

      {ready && canManage && !marketResearchExists ? (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <Button
            type="button"
            variant="outline"
            disabled={!secretaryRuntimeId || quickstart.isPending}
            aria-busy={quickstart.isPending}
            onClick={offerMarketResearch}
          >
            {quickstart.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
            {t(($) => $.office.setup_market_action)}
          </Button>
          {!secretaryRuntimeId ? (
            <p className="text-caption text-muted-foreground">{t(($) => $.office.setup_market_no_runtime)}</p>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
