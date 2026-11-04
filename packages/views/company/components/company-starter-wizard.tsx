"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { isRuntimeUsableForUser, runtimeListOptions } from "@multica/core/runtimes";
import {
  buildCompanyTemplateApplyRequest,
  isCompanyTemplateComplete,
  useApplyCompanyTemplate,
  type CompanyTemplateApplyRequest,
  type CompanyTemplateApplyResult,
  type CompanyTemplateStep,
} from "@multica/core/company";
import {
  COMPANY_FLOOR_KEYS,
  COMPANY_SQUAD_CARDS,
  COMPANY_SQUAD_KEYS,
  COMPANY_START_OPTIONS,
  COMPANY_STAGE_KEYS,
  DEFAULT_RUN_COST_USD,
  estimateCompanyRuns,
  unownedStages,
  type CompanyStageKey,
  type CompanyStart,
  type CompanySquadKey,
} from "@multica/core/squads";
import { memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Check, Loader2 } from "@multica/ui/icons";
import { RuntimePicker } from "../../agents/components/runtime-picker";
import { useT } from "../../i18n";

export type CompanyWizardPhase = "loading" | "choosing" | "applying" | "failed" | "done";

/**
 * Which screen the wizard shows. A result that confirms every step is done; a
 * result with a failed step, or with no steps at all (an unreadable answer), is
 * a failure the boss can run again; a request that threw is a failure too.
 */
export function companyWizardPhase(state: {
  loading: boolean;
  applying: boolean;
  result: CompanyTemplateApplyResult | null;
  errored: boolean;
}): CompanyWizardPhase {
  if (state.applying) return "applying";
  if (state.result) return isCompanyTemplateComplete(state.result) ? "done" : "failed";
  if (state.errored) return "failed";
  if (state.loading) return "loading";
  return "choosing";
}

/** The routines a start option runs, as runs per week and cost per week, shown before the boss commits. */
function formatRuns(runs: number): string {
  return String(Math.round(runs * 10) / 10);
}

function formatUsd(amount: number): string {
  return `$${amount.toFixed(2)}`;
}

/**
 * Builds the starter company: the boss picks a runtime, the squads to install,
 * and when routines start. The apply is idempotent, so a failed run is run again
 * as it was sent.
 */
export function CompanyStarterWizard({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);

  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: squads = [], isLoading: squadsLoading } = useQuery(squadListOptions(wsId));
  const apply = useApplyCompanyTemplate(wsId);

  const [selected, setSelected] = useState<Set<CompanySquadKey>>(() => new Set(COMPANY_SQUAD_KEYS));
  const [runtimeId, setRuntimeId] = useState("");
  const [model, setModel] = useState("");
  const [start, setStart] = useState<CompanyStart>("leave_paused");
  const [lastRequest, setLastRequest] = useState<CompanyTemplateApplyRequest | null>(null);
  const [result, setResult] = useState<CompanyTemplateApplyResult | null>(null);
  const [errored, setErrored] = useState(false);

  const phase = companyWizardPhase({
    loading: runtimesLoading || squadsLoading,
    applying: apply.isPending,
    result,
    errored,
  });

  const squadKeys = useMemo(
    () => COMPANY_SQUAD_KEYS.filter((key) => selected.has(key)),
    [selected],
  );
  const estimate = useMemo(() => estimateCompanyRuns(squadKeys, start), [squadKeys, start]);
  const unowned = useMemo(() => unownedStages(squadKeys), [squadKeys]);
  const existingSquadNames = useMemo(
    () => new Set(squads.filter((s) => !s.archived_at).map((s) => s.name)),
    [squads],
  );

  const runtime = runtimes.find((r) => r.id === runtimeId);
  const runtimeOk = !!runtime && isRuntimeUsableForUser(runtime, currentUserId);
  const canSubmit = phase === "choosing" && squadKeys.length > 0 && runtimeOk;

  const toggle = (key: CompanySquadKey, on: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (on) next.add(key);
      else next.delete(key);
      return next;
    });

  const run = async (request: CompanyTemplateApplyRequest) => {
    setLastRequest(request);
    setResult(null);
    setErrored(false);
    try {
      const done = await apply.mutateAsync(request);
      setResult(done);
      if (isCompanyTemplateComplete(done)) toast.success(t(($) => $.company.company_done_toast));
    } catch {
      setErrored(true);
      toast.error(t(($) => $.company.company_error_toast));
    }
  };

  const submit = () => {
    if (!canSubmit) return;
    void run(
      buildCompanyTemplateApplyRequest({ runtimeId, model, squads: squadKeys, start }),
    );
  };

  const retry = () => {
    if (lastRequest) void run(lastRequest);
  };

  const stepLabel = (step: CompanyTemplateStep) => {
    switch (step.status) {
      case "created":
        return t(($) => $.company.company_step_created);
      case "reused":
        return t(($) => $.company.company_step_reused);
      default:
        return t(($) => $.company.company_step_failed);
    }
  };

  const startLabel: Record<CompanyStart, { title: string; hint: string }> = {
    all: {
      title: t(($) => $.company.company_start_all),
      hint: t(($) => $.company.company_start_all_hint),
    },
    per_floor: {
      title: t(($) => $.company.company_start_per_floor),
      hint: t(($) => $.company.company_start_per_floor_hint),
    },
    leave_paused: {
      title: t(($) => $.company.company_start_leave_paused),
      hint: t(($) => $.company.company_start_leave_paused_hint),
    },
  };

  const stepList = (steps: CompanyTemplateStep[]) => (
    <ul className="space-y-1" aria-label={t(($) => $.company.company_steps)}>
      {steps.map((step, index) => (
        <li key={`${step.key}-${index}`} className="flex items-start justify-between gap-3 text-body">
          <span className="min-w-0 break-words">{step.message || step.key}</span>
          <span
            className={`shrink-0 text-caption ${
              step.status === "failed" ? "text-destructive" : "text-muted-foreground"
            }`}
          >
            {stepLabel(step)}
          </span>
        </li>
      ))}
    </ul>
  );

  // The server may name a stage this build does not know; it is shown as sent.
  const stageName = (key: string) =>
    (COMPANY_STAGE_KEYS as readonly string[]).includes(key)
      ? t(($) => $.company.company_stages[key as CompanyStageKey])
      : key;

  const unownedNote =
    unowned.length > 0 ? (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.company.company_unowned, {
          stages: unowned.map((stage) => stageName(stage.key)).join(", "),
        })}
      </p>
    ) : null;

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v && phase !== "applying") onClose(); }}>
      <DialogContent className="p-0 gap-0 flex flex-col overflow-hidden !top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2 !w-full !max-w-2xl !max-h-[85vh]">
        <DialogHeader className="border-b px-5 py-3 space-y-0">
          <DialogTitle className="text-title-sm font-semibold">
            {t(($) => $.company.company_title)}
          </DialogTitle>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto p-5 space-y-5">
          {phase === "loading" && (
            <p className="flex items-center gap-2 text-body text-muted-foreground" role="status">
              <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
              {t(($) => $.company.company_loading)}
            </p>
          )}

          {phase === "choosing" && (
            <>
              <p className="text-caption text-muted-foreground">{t(($) => $.company.company_intro)}</p>

              <RuntimePicker
                runtimes={runtimes}
                runtimesLoading={runtimesLoading}
                members={members}
                currentUserId={currentUserId}
                selectedRuntimeId={runtimeId}
                onSelect={setRuntimeId}
              />

              <div className="space-y-2">
                <Label htmlFor="company-model" className="text-caption text-muted-foreground">
                  {t(($) => $.company.company_model)}
                </Label>
                <Input
                  id="company-model"
                  value={model}
                  placeholder={t(($) => $.company.company_model_placeholder)}
                  onChange={(e) => setModel(e.target.value)}
                />
              </div>

              <fieldset className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <legend className="text-caption text-muted-foreground">
                    {t(($) => $.company.company_squads)}
                  </legend>
                  <div className="flex items-center gap-1">
                    <Button type="button" variant="ghost" size="sm" onClick={() => setSelected(new Set(COMPANY_SQUAD_KEYS))}>
                      {t(($) => $.company.company_select_all)}
                    </Button>
                    <Button type="button" variant="ghost" size="sm" onClick={() => setSelected(new Set())}>
                      {t(($) => $.company.company_clear)}
                    </Button>
                  </div>
                </div>
                {COMPANY_FLOOR_KEYS.map((floor) => {
                  const cards = COMPANY_SQUAD_CARDS.filter((card) => card.floor === floor);
                  return (
                    <div key={floor} className="space-y-1">
                      <p className="text-caption font-medium">{t(($) => $.company.company_floors[floor])}</p>
                      <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                        {cards.map((card) => {
                          const id = `company-squad-${card.key}`;
                          const exists = existingSquadNames.has(card.name);
                          return (
                            <li key={card.key} className="flex items-start gap-2">
                              <Checkbox
                                id={id}
                                checked={selected.has(card.key)}
                                onCheckedChange={(v) => toggle(card.key, v === true)}
                                className="mt-0.5"
                              />
                              <div className="min-w-0 space-y-0.5">
                                <Label htmlFor={id} className="text-body">
                                  {t(($) => $.company.company_cards[card.key].name)}
                                  {exists ? (
                                    <span className="ml-2 text-caption text-muted-foreground">
                                      {t(($) => $.company.company_squad_existing)}
                                    </span>
                                  ) : null}
                                </Label>
                                <p className="text-caption text-muted-foreground">
                                  {t(($) => $.company.company_cards[card.key].description)}
                                </p>
                              </div>
                            </li>
                          );
                        })}
                      </ul>
                    </div>
                  );
                })}
                {squadKeys.length === 0 ? (
                  <p className="text-caption text-destructive">{t(($) => $.company.company_no_squad)}</p>
                ) : null}
                {unownedNote}
              </fieldset>

              <div className="space-y-2">
                <p className="text-caption text-muted-foreground">{t(($) => $.company.company_start)}</p>
                <div role="radiogroup" aria-label={t(($) => $.company.company_start)} className="grid gap-2">
                  {COMPANY_START_OPTIONS.map((option) => {
                    const selectedOption = option === start;
                    return (
                      <button
                        key={option}
                        type="button"
                        role="radio"
                        aria-checked={selectedOption}
                        onClick={() => setStart(option)}
                        className={`flex flex-col items-start gap-0.5 rounded-lg border px-3 py-2 text-left transition-colors hover:bg-accent/50 ${
                          selectedOption ? "border-primary bg-accent/50" : ""
                        }`}
                      >
                        <span className="text-body font-medium">{startLabel[option].title}</span>
                        <span className="text-caption text-muted-foreground">{startLabel[option].hint}</span>
                      </button>
                    );
                  })}
                </div>
              </div>

              <p className="text-caption text-muted-foreground" aria-live="polite">
                {estimate.runsPerWeek === 0
                  ? t(($) => $.company.company_estimate_none)
                  : t(($) => $.company.company_estimate, {
                      runs: formatRuns(estimate.runsPerWeek),
                      cost: formatUsd(estimate.costPerWeekUsd),
                    })}
                {estimate.costIsDefault && estimate.runsPerWeek > 0
                  ? ` ${t(($) => $.company.company_estimate_default, { rate: formatUsd(DEFAULT_RUN_COST_USD) })}`
                  : null}
              </p>
            </>
          )}

          {phase === "applying" && (
            <p className="flex items-center gap-2 text-body" role="status">
              <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
              {t(($) => $.company.company_applying)}
            </p>
          )}

          {(phase === "failed" || phase === "done") && (
            <div className="space-y-4" aria-live="polite">
              <p className="flex items-center gap-2 text-body font-medium">
                {phase === "done" ? <Check aria-hidden="true" className="size-4 text-brand" /> : null}
                {phase === "done"
                  ? t(($) => $.company.company_done_title)
                  : t(($) => $.company.company_failed_title)}
              </p>
              {phase === "failed" && result && result.steps.length > 0 ? (
                <p className="text-caption text-muted-foreground">{t(($) => $.company.company_failed_message)}</p>
              ) : null}
              {phase === "failed" && (!result || result.steps.length === 0) ? (
                <p className="text-caption text-destructive">
                  {errored ? t(($) => $.company.company_errored) : t(($) => $.company.company_unconfirmed)}
                </p>
              ) : null}
              {result && result.steps.length > 0 ? stepList(result.steps) : null}
              {result && result.unowned_stages.length > 0 ? (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.company.company_unowned, {
                    stages: result.unowned_stages.map(stageName).join(", "),
                  })}
                </p>
              ) : null}
            </div>
          )}
        </div>

        <div className="flex items-center justify-end gap-2 border-t px-5 py-3">
          {phase === "choosing" && (
            <>
              <Button variant="outline" onClick={onClose}>
                {t(($) => $.company.company_cancel)}
              </Button>
              <Button onClick={submit} disabled={!canSubmit}>
                {t(($) => $.company.company_submit)}
              </Button>
            </>
          )}
          {phase === "applying" && (
            <Button variant="outline" disabled>
              {t(($) => $.company.company_cancel)}
            </Button>
          )}
          {phase === "failed" && (
            <>
              <Button variant="outline" onClick={onClose}>
                {t(($) => $.company.company_close)}
              </Button>
              <Button onClick={retry} disabled={!lastRequest}>
                {t(($) => $.company.company_retry)}
              </Button>
            </>
          )}
          {phase === "done" && (
            <Button onClick={onClose}>{t(($) => $.company.company_close)}</Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
