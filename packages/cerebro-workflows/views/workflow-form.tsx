"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { useNavigation } from "@multica/views/navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  ACTION_OPTIONS,
  TRIGGER_OPTIONS,
  cerebroWorkflowDetailOptions,
  cerebroWorkflowsKeys,
  createWorkflow,
  updateWorkflow,
} from "../core";
import type {
  CerebroWorkflowActionType,
  CerebroWorkflowTriggerType,
  WorkflowWriteInput,
} from "../core/types";

const ISSUE_STATUSES = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
];

interface FormState {
  name: string;
  enabled: boolean;
  triggerType: CerebroWorkflowTriggerType;
  fromStatus: string;
  toStatus: string;
  actionType: CerebroWorkflowActionType;
  setStatus: string;
  subIssueTitle: string;
  subIssueDescription: string;
  reminderRecipientId: string;
  reminderRecipientType: "member" | "agent";
  reminderMessage: string;
}

const EMPTY: FormState = {
  name: "",
  enabled: true,
  triggerType: "status_changed",
  fromStatus: "",
  toStatus: "in_review",
  actionType: "create_sub_issue",
  setStatus: "todo",
  subIssueTitle: "",
  subIssueDescription: "",
  reminderRecipientId: "",
  reminderRecipientType: "member",
  reminderMessage: "",
};

interface WorkflowFormProps {
  workflowId?: string;
}

// Form-based workflow builder (JEH-1047 phase 1). Node-canvas is deferred to
// phase 2 — this form covers the same data model. When workflowId is set
// the form pre-fills from the workflow detail query and POSTs to PUT
// instead of POST.
export function WorkflowForm({ workflowId }: WorkflowFormProps) {
  const enabled = useFeatureFlag("cerebro_workflows");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  const queryClient = useQueryClient();

  const wsId = workspace?.id ?? "";

  const detail = useQuery({
    ...cerebroWorkflowDetailOptions(wsId, workflowId ?? ""),
    enabled: !!workflowId && !!wsId,
  });

  const [form, setForm] = useState<FormState>(EMPTY);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!detail.data) return;
    const w = detail.data;
    const tc = (w.trigger_config as { from_status?: string; to_status?: string } | null) ?? {};
    const ac = w.action_config as Record<string, unknown> | null;
    setForm({
      name: w.name,
      enabled: w.enabled,
      triggerType: w.trigger_type,
      fromStatus: tc.from_status ?? "",
      toStatus: tc.to_status ?? "",
      actionType: w.action_type,
      setStatus: (ac?.status as string) ?? "todo",
      subIssueTitle: (ac?.title as string) ?? "",
      subIssueDescription: (ac?.description as string) ?? "",
      reminderRecipientId: (ac?.recipient_id as string) ?? "",
      reminderRecipientType: ((ac?.recipient_type as "member" | "agent") ?? "member"),
      reminderMessage: (ac?.message as string) ?? "",
    });
  }, [detail.data]);

  const save = useMutation({
    mutationFn: async () => {
      const payload = buildPayload(form);
      if (workflowId) return updateWorkflow(workflowId, payload);
      return createWorkflow(payload);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: cerebroWorkflowsKeys.all(wsId) });
      if (workspace) navigation.push(`/${workspace.slug}/workflows`);
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Kunne ikke gemme workflow");
    },
  });

  if (!enabled) return null;
  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  const heading = workflowId ? "Rediger workflow" : "Nyt workflow";

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">{heading}</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            Trigger → conditions → action. Form-based; node-canvas følger i fase 2.
          </p>
        </div>
      </PageHeader>

      <div className="flex-1 min-h-0 overflow-y-auto">
        <form
          className="mx-auto flex max-w-2xl flex-col gap-6 p-6"
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            save.mutate();
          }}
        >
          <Section title="Grundlæggende">
            <Field label="Navn">
              <Input
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                required
                placeholder='F.eks. "Når in_review → opret QA sub-issue"'
              />
            </Field>
            <Field label="Aktiv">
              <Switch
                checked={form.enabled}
                onCheckedChange={(v) => setForm({ ...form, enabled: v === true })}
              />
            </Field>
          </Section>

          <Section title="Trigger">
            <Field label="Type">
              <NativeSelect
                value={form.triggerType}
                onChange={(e) =>
                  setForm({ ...form, triggerType: e.target.value as CerebroWorkflowTriggerType })
                }
              >
                {TRIGGER_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </NativeSelect>
            </Field>
            {form.triggerType === "status_changed" && (
              <>
                <Field label="Fra status (tom = alle)">
                  <NativeSelect
                    value={form.fromStatus}
                    onChange={(e) => setForm({ ...form, fromStatus: e.target.value })}
                  >
                    <option value="">(alle)</option>
                    {ISSUE_STATUSES.map((s) => (
                      <option key={s} value={s}>
                        {s}
                      </option>
                    ))}
                  </NativeSelect>
                </Field>
                <Field label="Til status">
                  <NativeSelect
                    value={form.toStatus}
                    onChange={(e) => setForm({ ...form, toStatus: e.target.value })}
                  >
                    {ISSUE_STATUSES.map((s) => (
                      <option key={s} value={s}>
                        {s}
                      </option>
                    ))}
                  </NativeSelect>
                </Field>
              </>
            )}
          </Section>

          <Section title="Action">
            <Field label="Type">
              <NativeSelect
                value={form.actionType}
                onChange={(e) =>
                  setForm({ ...form, actionType: e.target.value as CerebroWorkflowActionType })
                }
              >
                {ACTION_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </NativeSelect>
            </Field>

            {form.actionType === "set_status" && (
              <Field label="Sæt status til">
                <NativeSelect
                  value={form.setStatus}
                  onChange={(e) => setForm({ ...form, setStatus: e.target.value })}
                >
                  {ISSUE_STATUSES.map((s) => (
                    <option key={s} value={s}>
                      {s}
                    </option>
                  ))}
                </NativeSelect>
              </Field>
            )}

            {form.actionType === "create_sub_issue" && (
              <>
                <Field label="Sub-issue titel">
                  <Input
                    value={form.subIssueTitle}
                    onChange={(e) => setForm({ ...form, subIssueTitle: e.target.value })}
                    required
                    placeholder="F.eks. QA: {{title}}"
                  />
                </Field>
                <Field label="Sub-issue beskrivelse (valgfri)">
                  <Textarea
                    value={form.subIssueDescription}
                    onChange={(e) => setForm({ ...form, subIssueDescription: e.target.value })}
                    rows={3}
                  />
                </Field>
              </>
            )}

            {form.actionType === "send_reminder" && (
              <>
                <Field label="Modtagertype">
                  <NativeSelect
                    value={form.reminderRecipientType}
                    onChange={(e) =>
                      setForm({
                        ...form,
                        reminderRecipientType: e.target.value as "member" | "agent",
                      })
                    }
                  >
                    <option value="member">Member</option>
                    <option value="agent">Agent</option>
                  </NativeSelect>
                </Field>
                <Field label="Modtager-ID (UUID)">
                  <Input
                    value={form.reminderRecipientId}
                    onChange={(e) => setForm({ ...form, reminderRecipientId: e.target.value })}
                    required
                    placeholder="<uuid>"
                  />
                </Field>
                <Field label="Besked">
                  <Textarea
                    value={form.reminderMessage}
                    onChange={(e) => setForm({ ...form, reminderMessage: e.target.value })}
                    rows={3}
                    required
                  />
                </Field>
              </>
            )}
          </Section>

          {error && (
            <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
              {error}
            </div>
          )}

          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => navigation.push(`/${workspace.slug}/workflows`)}
            >
              Annullér
            </Button>
            <Button type="submit" disabled={save.isPending}>
              {save.isPending ? "Gemmer…" : "Gem"}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <fieldset className="flex flex-col gap-3 rounded-md border bg-card p-4">
      <legend className="px-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {title}
      </legend>
      {children}
    </fieldset>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label className="text-xs">{label}</Label>
      {children}
    </div>
  );
}

function buildPayload(f: FormState): WorkflowWriteInput {
  const triggerConfig =
    f.triggerType === "status_changed"
      ? { from_status: f.fromStatus || undefined, to_status: f.toStatus || undefined }
      : {};

  let actionConfig: Record<string, unknown> = {};
  if (f.actionType === "set_status") {
    actionConfig = { status: f.setStatus };
  } else if (f.actionType === "create_sub_issue") {
    actionConfig = {
      title: f.subIssueTitle,
      description: f.subIssueDescription || undefined,
    };
  } else if (f.actionType === "send_reminder") {
    actionConfig = {
      recipient_id: f.reminderRecipientId,
      recipient_type: f.reminderRecipientType,
      message: f.reminderMessage,
    };
  }

  return {
    name: f.name,
    enabled: f.enabled,
    trigger_type: f.triggerType,
    trigger_config: triggerConfig,
    conditions: [],
    action_type: f.actionType,
    action_config: actionConfig,
  };
}
