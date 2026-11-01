"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "@multica/ui/icons";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { isRuntimeUsableForUser, runtimeListOptions } from "@multica/core/runtimes";
import {
  planQuickstartAgents,
  SQUAD_TEMPLATES,
  useSquadQuickstart,
  type QuickstartOutcome,
  type SquadTemplate,
} from "@multica/core/squads";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";

import { RuntimePicker } from "../agents/components/runtime-picker";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";

function hasProblems(outcome: QuickstartOutcome): boolean {
  return (
    outcome.agents.some((a) => a.status === "failed") ||
    outcome.squad.status === "failed" ||
    !!outcome.squad.error ||
    outcome.workflow.status === "failed" ||
    outcome.routine.status === "failed"
  );
}

export function SquadQuickstartModal({ onClose }: { onClose: () => void }) {
  const { t } = useT("modals");
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);

  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const [templateId, setTemplateId] = useState<SquadTemplate["id"]>(
    SQUAD_TEMPLATES[0]!.id,
  );
  const [runtimeId, setRuntimeId] = useState("");
  const [createWorkflow, setCreateWorkflow] = useState(true);
  const [outcome, setOutcome] = useState<QuickstartOutcome | null>(null);
  const quickstart = useSquadQuickstart(wsId);

  const template = SQUAD_TEMPLATES.find((tpl) => tpl.id === templateId)!;
  const plan = useMemo(() => planQuickstartAgents(template, agents), [template, agents]);
  const runtime = runtimes.find((r) => r.id === runtimeId);
  const needsRuntime = plan.some((p) => !p.reuse);
  const runtimeOk = !needsRuntime || (!!runtime && isRuntimeUsableForUser(runtime, currentUserId));
  const running = quickstart.isPending;
  const canSubmit = runtimeOk && !running;

  const run = async () => {
    if (!canSubmit) return;
    try {
      const done = await quickstart.mutateAsync({
        template,
        options: {
          runtimeId,
          createWorkflow: createWorkflow && !!template.workflow,
        },
        onProgress: setOutcome,
      });
      setOutcome(done);
      if (!hasProblems(done)) toast.success(t(($) => $.squad_quickstart.toast_done));
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t(($) => $.squad_quickstart.toast_failed),
      );
    }
  };

  const finished = outcome !== null && !running;
  const statusText = {
    created: t(($) => $.squad_quickstart.status_created),
    reused: t(($) => $.squad_quickstart.status_reused),
    failed: t(($) => $.squad_quickstart.status_failed),
    existing: t(($) => $.squad_quickstart.status_existing),
    skipped: "",
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v && !running) onClose(); }}>
      <DialogContent className="p-0 gap-0 flex flex-col overflow-hidden !top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2 !w-full !max-w-2xl !max-h-[85vh]">
        <DialogHeader className="border-b px-5 py-3 space-y-0">
          <DialogTitle className="text-title-sm font-semibold">
            {t(($) => $.squad_quickstart.title)}
          </DialogTitle>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto p-5 space-y-5">
          {!outcome && (
            <>
              <div className="space-y-2">
                <Label className="text-caption text-muted-foreground">
                  {t(($) => $.squad_quickstart.template_label)}
                </Label>
                <div role="radiogroup" className="grid gap-2">
                  {SQUAD_TEMPLATES.map((tpl) => {
                    const selected = tpl.id === templateId;
                    return (
                      <button
                        key={tpl.id}
                        type="button"
                        role="radio"
                        aria-checked={selected}
                        onClick={() => setTemplateId(tpl.id)}
                        className={`flex flex-col items-start gap-0.5 rounded-lg border px-3 py-2 text-left transition-colors hover:bg-accent/50 ${
                          selected ? "border-primary bg-accent/50" : ""
                        }`}
                      >
                        <span className="text-body font-medium">
                          {t(($) => $.squad_quickstart.templates[tpl.id].name)}
                        </span>
                        <span className="text-caption text-muted-foreground">
                          {t(($) => $.squad_quickstart.templates[tpl.id].description)}
                        </span>
                      </button>
                    );
                  })}
                </div>
              </div>

              <div className="space-y-2">
                <Label className="text-caption text-muted-foreground">
                  {t(($) => $.squad_quickstart.agents_label)}
                </Label>
                <ul className="grid grid-cols-1 gap-x-4 gap-y-1 sm:grid-cols-2">
                  {plan.map((p) => (
                    <li key={p.role} className="flex items-center justify-between gap-2 text-body">
                      <span className="truncate">{p.name}</span>
                      <span className="shrink-0 text-caption text-muted-foreground">
                        {p.reuse
                          ? t(($) => $.squad_quickstart.agent_reused)
                          : t(($) => $.squad_quickstart.agent_new)}
                      </span>
                    </li>
                  ))}
                </ul>
              </div>

              {needsRuntime && (
                <RuntimePicker
                  runtimes={runtimes}
                  runtimesLoading={runtimesLoading}
                  members={members}
                  currentUserId={currentUserId}
                  selectedRuntimeId={runtimeId}
                  onSelect={setRuntimeId}
                />
              )}

              {template.workflow && (
                <div className="flex items-start gap-2">
                  <Checkbox
                    id="squad-quickstart-workflow"
                    checked={createWorkflow}
                    onCheckedChange={(v) => setCreateWorkflow(v === true)}
                    className="mt-0.5"
                  />
                  <div className="space-y-0.5">
                    <Label htmlFor="squad-quickstart-workflow" className="text-body">
                      {t(($) => $.squad_quickstart.workflow_label, {
                        name: template.workflow.name,
                      })}
                    </Label>
                    <p className="text-caption text-muted-foreground">
                      {t(($) => $.squad_quickstart.workflow_hint)}
                    </p>
                  </div>
                </div>
              )}
            </>
          )}

          {outcome && (
            <div className="space-y-4" aria-live="polite">
              <p className="text-body font-medium">
                {running
                  ? t(($) => $.squad_quickstart.creating)
                  : hasProblems(outcome)
                    ? t(($) => $.squad_quickstart.result_problems)
                    : t(($) => $.squad_quickstart.result_ready)}
              </p>
              <ul className="space-y-1">
                {outcome.agents.map((a) => (
                  <li key={a.role} className="flex items-start justify-between gap-3 text-body">
                    <span className="truncate">{a.name}</span>
                    <span
                      className={`shrink-0 text-caption ${
                        a.status === "failed" ? "text-destructive" : "text-muted-foreground"
                      }`}
                    >
                      {a.status === "failed" && a.error
                        ? `${t(($) => $.squad_quickstart.status_failed)}: ${a.error}`
                        : statusText[a.status]}
                    </span>
                  </li>
                ))}
                {outcome.squad.id !== null || outcome.squad.status === "failed" ? (
                  <li className="flex items-start justify-between gap-3 text-body font-medium">
                    <span>{t(($) => $.squad_quickstart.squad_row)}</span>
                    <span
                      className={`shrink-0 text-caption ${
                        outcome.squad.status === "failed" || outcome.squad.error
                          ? "text-destructive"
                          : "text-muted-foreground"
                      }`}
                    >
                      {outcome.squad.error
                        ? `${t(($) => $.squad_quickstart.status_problem)}: ${outcome.squad.error}`
                        : statusText[outcome.squad.status]}
                    </span>
                  </li>
                ) : null}
                {outcome.workflow.status !== "skipped" && (
                  <li className="flex items-start justify-between gap-3 text-body font-medium">
                    <span>{t(($) => $.squad_quickstart.workflow_row)}</span>
                    <span
                      className={`shrink-0 text-caption ${
                        outcome.workflow.status === "failed"
                          ? "text-destructive"
                          : "text-muted-foreground"
                      }`}
                    >
                      {outcome.workflow.status === "failed"
                        ? `${t(($) => $.squad_quickstart.status_failed)}: ${outcome.workflow.error}`
                        : outcome.workflow.note === "qa_step_left_out"
                          ? t(($) => $.squad_quickstart.workflow_without_qa)
                          : statusText[outcome.workflow.status]}
                    </span>
                  </li>
                )}
              </ul>
              {finished && !hasProblems(outcome) && (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.squad_quickstart.next_step)}
                </p>
              )}
            </div>
          )}
        </div>

        <div className="flex items-center justify-end gap-2 border-t px-5 py-3">
          {finished ? (
            <>
              {hasProblems(outcome) && (
                <Button variant="outline" onClick={() => void run()}>
                  {t(($) => $.squad_quickstart.retry)}
                </Button>
              )}
              <Button variant="outline" onClick={onClose}>
                {t(($) => $.squad_quickstart.close)}
              </Button>
              {outcome.squad.id && (
                <Button
                  onClick={() => {
                    onClose();
                    router.push(wsPaths.squadDetail(outcome.squad.id!));
                  }}
                >
                  {t(($) => $.squad_quickstart.open_squad)}
                </Button>
              )}
            </>
          ) : (
            <>
              <Button variant="outline" onClick={onClose} disabled={running}>
                {t(($) => $.squad_quickstart.cancel)}
              </Button>
              <Button onClick={() => void run()} disabled={!canSubmit}>
                {running && <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />}
                {running
                  ? t(($) => $.squad_quickstart.creating)
                  : t(($) => $.squad_quickstart.create)}
              </Button>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
