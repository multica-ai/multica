"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  buildCompanySetupRequest,
  DEFAULT_COMPANY_SETUP_DOCS_PATH,
  isCompanySetupReady,
  companySetupOptions,
  parseCompanySetupRefusal,
  useSetupCompany,
  type CompanySetupRefusal,
} from "@multica/core/company";
import { SQUAD_TEMPLATES, useSquadQuickstart, type QuickstartOutcome } from "@multica/core/squads";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Check, Circle, ExternalLink, Loader2, RotateCw, Sparkles } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { docsLocalePrefix } from "../../common/docs-locale";
import { useT } from "../../i18n";
import { DOCS_URL } from "../../layout/help-links";
import { useCompanyAdmin } from "../lib/use-company-admin";
import { CompanyStarterWizard } from "./company-starter-wizard";

const MARKET_RESEARCH = SQUAD_TEMPLATES.find((template) => template.id === "market_research")!;

/**
 * The Secretary's first-run flow. It sits in the floor header as a compact trigger, not
 * inline, so the floor plan keeps its full height. The trigger shows while the company is
 * not set up and, for an owner or admin, until the Market Research squad exists; the
 * checklist opens from it. Owners and admins run setup and the squad offer, and everyone
 * else reads the status.
 */
export function CompanySetupCard() {
  const { t, i18n } = useT("issues");
  const wsId = useWorkspaceId();
  const canManage = useCompanyAdmin();
  const readinessQuery = useQuery(companySetupOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const setup = useSetupCompany(wsId);
  const quickstart = useSquadQuickstart(wsId);
  const [refusal, setRefusal] = useState<CompanySetupRefusal | null>(null);
  const [open, setOpen] = useState(false);
  const [companyOpen, setCompanyOpen] = useState(false);

  const readiness = readinessQuery.data;
  if (!readiness) return null;

  const ready = isCompanySetupReady(readiness);
  const marketResearchExists = squads.some((squad) => !squad.archived_at && squad.name === MARKET_RESEARCH.name);
  // Members see the trigger only while setup is incomplete; the squad offer is for the boss.
  const visible = !ready || (canManage && !marketResearchExists);
  if (!visible) return null;

  const runtimeMissing = !readiness.runtime_online || refusal?.code === "runtime_required";
  const docsPath = refusal?.docs_path || DEFAULT_COMPANY_SETUP_DOCS_PATH;
  const installUrl = `${DOCS_URL}${docsLocalePrefix(i18n.language)}${docsPath}`;
  const secretaryRuntimeId = agents.find((agent) => agent.id === readiness.secretary_id)?.runtime_id ?? "";

  const checks: { done: boolean; label: string }[] = [
    { done: readiness.runtime_online, label: t(($) => $.company.setup_check_runtime) },
    { done: readiness.secretary_id !== null, label: t(($) => $.company.setup_check_secretary) },
    { done: readiness.executive_floor, label: t(($) => $.company.setup_check_floor) },
    { done: readiness.autopilot_daily, label: t(($) => $.company.setup_check_daily) },
    { done: readiness.autopilot_weekly_paused, label: t(($) => $.company.setup_check_weekly) },
  ];
  const doneCount = checks.filter((check) => check.done).length;

  const refusalMessage = (code: string) => {
    switch (code) {
      case "secretary_name_taken":
        return t(($) => $.company.setup_name_taken);
      case "floor_full":
        return t(($) => $.company.setup_floor_full);
      case "floor_limit":
        return t(($) => $.company.setup_floor_limit);
      default:
        return t(($) => $.company.setup_failed);
    }
  };

  const runSetup = () =>
    setup.mutate(buildCompanySetupRequest(), {
      onSuccess: () => {
        setRefusal(null);
        toast.success(t(($) => $.company.setup_done));
      },
      onError: (err) => {
        const parsed = err instanceof ApiError ? parseCompanySetupRefusal(err.body) : null;
        if (parsed?.code === "runtime_required") {
          setRefusal(parsed);
          return;
        }
        toast.error(parsed ? refusalMessage(parsed.code) : t(($) => $.company.setup_failed));
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
          if (failed) toast.error(t(($) => $.company.setup_market_failed));
          else toast.success(t(($) => $.company.setup_market_done));
        },
        onError: () => toast.error(t(($) => $.company.setup_market_failed)),
      },
    );

  const openWizard = () => {
    setOpen(false);
    setCompanyOpen(true);
  };

  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button type="button" variant="outline" size="sm" aria-haspopup="dialog" />
          }
        >
          <span
            aria-hidden
            className={cn("size-1.5 shrink-0 rounded-full", runtimeMissing ? "bg-destructive" : "bg-brand")}
          />
          {ready ? t(($) => $.company.setup_trigger_offer) : t(($) => $.company.setup_trigger)}
          {ready ? null : (
            <span className="tabular-nums text-muted-foreground">
              {doneCount}/{checks.length}
            </span>
          )}
        </PopoverTrigger>
        <PopoverContent align="start" className="w-80 p-4">
          <section aria-labelledby="company-setup-title" className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              <Sparkles className="size-4 text-muted-foreground" />
              <h2 id="company-setup-title" className="text-heading">
                {t(($) => $.company.setup_title)}
              </h2>
            </div>
            <p className="text-caption text-muted-foreground">{t(($) => $.company.setup_intro)}</p>
            <ul className="flex flex-col gap-1.5">
              {checks.map((check) => (
                <li key={check.label} className="flex items-center gap-2 text-body">
                  {check.done ? (
                    <Check className="size-3.5 text-brand" aria-label={t(($) => $.company.setup_check_done)} />
                  ) : (
                    <Circle className="size-3.5 text-muted-foreground" aria-hidden />
                  )}
                  <span className={check.done ? undefined : "text-muted-foreground"}>{check.label}</span>
                </li>
              ))}
            </ul>

            {runtimeMissing ? (
              <div className="flex flex-col items-start gap-2">
                <p className="text-caption text-muted-foreground">{t(($) => $.company.setup_runtime_missing)}</p>
                <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
                  <a
                    href={installUrl}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-caption font-medium text-brand hover:underline"
                  >
                    {t(($) => $.company.setup_install_guide)}
                    <ExternalLink className="size-3" />
                  </a>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={readinessQuery.isFetching}
                    onClick={() => readinessQuery.refetch()}
                  >
                    <RotateCw className="size-3.5" />
                    {t(($) => $.company.setup_retry)}
                  </Button>
                </div>
              </div>
            ) : null}

            {!ready && !runtimeMissing && !canManage ? (
              <p className="text-caption text-muted-foreground">{t(($) => $.company.setup_read_only)}</p>
            ) : null}

            {!ready && !runtimeMissing && canManage ? (
              <div>
                <Button type="button" disabled={setup.isPending} aria-busy={setup.isPending} onClick={runSetup}>
                  {setup.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
                  {setup.isPending ? t(($) => $.company.setup_pending) : t(($) => $.company.setup_action)}
                </Button>
              </div>
            ) : null}

            {ready && canManage && !marketResearchExists ? (
              <div className="flex flex-col items-start gap-2">
                <Button
                  type="button"
                  variant="outline"
                  disabled={!secretaryRuntimeId || quickstart.isPending}
                  aria-busy={quickstart.isPending}
                  onClick={offerMarketResearch}
                >
                  {quickstart.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
                  {t(($) => $.company.setup_market_action)}
                </Button>
                {!secretaryRuntimeId ? (
                  <p className="text-caption text-muted-foreground">{t(($) => $.company.setup_market_no_runtime)}</p>
                ) : null}
              </div>
            ) : null}

            {ready && canManage ? (
              <Button type="button" variant="ghost" size="sm" className="self-start" onClick={openWizard}>
                {t(($) => $.company.company_entry)}
              </Button>
            ) : null}
          </section>
        </PopoverContent>
      </Popover>
      {companyOpen ? (
        <CompanyStarterWizard open onClose={() => setCompanyOpen(false)} />
      ) : null}
    </>
  );
}
