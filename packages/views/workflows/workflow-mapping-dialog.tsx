"use client";

import { useEffect, useState } from "react";
import { ArrowRight, Check } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@multica/ui/components/ui/select";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type { IssueWorkflowMappingPlan } from "@multica/core/types";
import { StatusIcon } from "../issues/components/status-icon";
import { useStatusLabel } from "../issues/utils/status-label";
import { useT } from "../i18n";

export type WorkflowMappingVariant =
  | { kind: "switch"; projectName: string; workflowName: string }
  | { kind: "edit"; workflowName: string };

/**
 * Confirms a workflow change that can move issues (MUL-7420): switching a
 * project's workflow, or saving a workflow edit that removed statuses in use.
 * Only statuses the target lacks get a destination; the rest are listed as
 * unchanged, and the move never hands anything off.
 */
export function WorkflowMappingDialog({
  open,
  onOpenChange,
  variant,
  plan,
  targetKeys,
  pending,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  variant: WorkflowMappingVariant;
  plan: IssueWorkflowMappingPlan | null;
  /** Statuses the issues may move to, in the target workflow's order. */
  targetKeys: string[];
  pending: boolean;
  onConfirm: (mapping: Record<string, string>) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);
  const [mapping, setMapping] = useState<Record<string, string>>({});

  useEffect(() => {
    if (!plan) return;
    setMapping(
      Object.fromEntries(
        plan.required.map((r) => [
          r.status_key,
          targetKeys.includes(r.suggested_status_key) ? r.suggested_status_key : targetKeys[0] ?? "",
        ]),
      ),
    );
  }, [plan, targetKeys]);

  const icon = (key: string) => (
    <StatusIcon status={key} category={categoryOf(key)} color={colorOf(key)} icon={iconOf(key)} className="h-3.5 w-3.5 shrink-0" />
  );
  const required = plan?.required ?? [];
  const unchanged = plan?.unchanged ?? [];
  const moving = required.reduce((sum, r) => sum + r.issue_count, 0);
  const staying = unchanged.reduce((sum, r) => sum + r.issue_count, 0);
  const complete = !!plan && required.every((r) => !!mapping[r.status_key]);
  const separator = t(($) => $.workflows.list_separator);

  const title =
    variant.kind === "switch"
      ? t(($) => $.workflows.mapping.switch_title, { project: variant.projectName, workflow: variant.workflowName })
      : t(($) => $.workflows.mapping.edit_title, { workflow: variant.workflowName });
  const description =
    variant.kind === "switch"
      ? t(($) => $.workflows.mapping.switch_description, {
          project: variant.projectName,
          workflow: variant.workflowName,
          count: plan?.total_issues ?? 0,
        })
      : t(($) => $.workflows.mapping.edit_description);
  const confirmLabel =
    variant.kind === "switch"
      ? moving > 0
        ? t(($) => $.workflows.mapping.confirm_count, { count: moving })
        : t(($) => $.workflows.mapping.confirm_plain)
      : t(($) => $.workflows.mapping.confirm_save_count, { count: moving });

  return (
    <Dialog open={open} onOpenChange={(v) => !pending && onOpenChange(v)}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-1">
          {required.length > 0 && (
            <div>
              <p className="text-caption font-medium text-muted-foreground">
                {t(($) => $.workflows.mapping.needs_mapping, { count: moving })}
              </p>
              {required.map((req) => (
                <div key={req.status_key} className="flex items-center gap-3 py-2">
                  <div className="flex w-44 min-w-0 shrink-0 items-center gap-2">
                    {icon(req.status_key)}
                    <span className="truncate text-body">{labelOf(req.status_key)}</span>
                    <span className="shrink-0 text-caption tabular-nums text-muted-foreground">
                      {t(($) => $.workflows.mapping.issues, { count: req.issue_count })}
                    </span>
                  </div>
                  <ArrowRight aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
                  <Select
                    items={targetKeys.map((key) => ({ value: key, label: labelOf(key) }))}
                    value={mapping[req.status_key] ?? ""}
                    onValueChange={(value) =>
                      value && setMapping((current) => ({ ...current, [req.status_key]: value }))
                    }
                  >
                    <SelectTrigger aria-label={labelOf(req.status_key)} className="min-w-0 flex-1">
                      {mapping[req.status_key] ? (
                        <span className="flex min-w-0 items-center gap-2">
                          {icon(mapping[req.status_key]!)}
                          <span className="truncate">{labelOf(mapping[req.status_key]!)}</span>
                        </span>
                      ) : null}
                    </SelectTrigger>
                    <SelectContent>
                      {targetKeys.map((key) => (
                        <SelectItem key={key} value={key}>
                          <span className="flex items-center gap-2">
                            {icon(key)}
                            <span>{labelOf(key)}</span>
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              ))}
            </div>
          )}
          {(staying > 0 || required.length === 0) && (
            <div className="flex items-start gap-2 rounded-lg bg-muted/60 px-3 py-2.5 text-body">
              <Check aria-hidden className="mt-0.5 size-3.5 shrink-0" />
              <span>
                {staying > 0
                  ? t(($) => $.workflows.mapping.unchanged, {
                      count: staying,
                      statuses: unchanged.map((u) => labelOf(u.status_key)).join(separator),
                    })
                  : t(($) => $.workflows.mapping.nothing_moves)}
              </span>
            </div>
          )}
          <p className="text-caption leading-5 text-muted-foreground">
            {variant.kind === "switch"
              ? t(($) => $.workflows.mapping.switch_note)
              : t(($) => $.workflows.mapping.no_handoff)}
          </p>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            {t(($) => $.workflows.mapping.cancel)}
          </Button>
          <Button onClick={() => onConfirm(mapping)} disabled={!complete || pending} aria-busy={pending}>
            {pending && <Spinner className="size-3.5" />}
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
