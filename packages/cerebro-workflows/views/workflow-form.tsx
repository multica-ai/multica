"use client";

import type { ReactNode } from "react";
import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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
  WORKFLOW_TEMPLATES,
  cerebroWorkflowDetailOptions,
  cerebroWorkflowsKeys,
  createWorkflow,
  updateWorkflow,
} from "../core";
import type {
  CerebroAssigneeType,
  CerebroCommentMatchMode,
  CerebroWorkflowActionType,
  CerebroWorkflowTriggerType,
  WorkflowWriteInput,
} from "../core/types";
import {
  AllChildrenDoneFields,
  CommentMentionFields,
  CronTriggerFields,
  PHASE3_EMPTY,
  ReassignIssueFields,
  SubIssueCreatedFields,
  WebhookInboundFields,
  WebhookOutboundFields,
  type WorkflowPhase3FormFields,
} from "./workflow-fields";

const ISSUE_STATUSES = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
];

export interface FormState extends WorkflowPhase3FormFields {
  name: string;
  enabled: boolean;
  triggerType: CerebroWorkflowTriggerType;
  fromStatus: string;
  toStatus: string;
  actionType: CerebroWorkflowActionType;
  setStatus: string;
  subIssueTitle: string;
  subIssueDescription: string;
  subIssueAssigneeId: string;
  subIssueAssigneeType: "member" | "agent";
  subIssueLabelIDs: string; // comma-separated; parsed at save-time
  reminderRecipientId: string;
  reminderRecipientType: "member" | "agent";
  reminderMessage: string;
  // Phase 2.
  skillName: string;
  skillAgentId: string;
  skillInputJSON: string;
  commentTarget: "self" | "parent";
  commentContent: string;
  // Phase 2 ext (JEH-1114, route_by_domain).
  routeLabelPrefix: string;
  routeDefaultDomain: "code" | "business" | "design" | "content";
  // Phase 2 ext (JEH-1114, validate_evidence). Conditions don't have a
  // first-class form editor yet (pre-existing gap from phase 1) — they
  // ride along opaquely so templates that pre-fill `evidence_present`
  // survive a save round-trip. Power users can hand-edit the JSON here
  // until the condition editor lands.
  conditionsJSON: string;
}

export const EMPTY_FORM: FormState = {
  ...PHASE3_EMPTY,
  name: "",
  enabled: true,
  triggerType: "status_changed",
  fromStatus: "",
  toStatus: "in_review",
  actionType: "create_sub_issue",
  setStatus: "todo",
  subIssueTitle: "",
  subIssueDescription: "",
  subIssueAssigneeId: "",
  subIssueAssigneeType: "agent",
  subIssueLabelIDs: "",
  reminderRecipientId: "",
  reminderRecipientType: "member",
  reminderMessage: "",
  skillName: "",
  skillAgentId: "",
  skillInputJSON: "{}",
  commentTarget: "self",
  commentContent: "",
  routeLabelPrefix: "domain:",
  routeDefaultDomain: "business",
  conditionsJSON: "[]",
};

interface WorkflowFormProps {
  workflowId?: string;
  /** Optional content rendered inside the page header (e.g. mode toggle). */
  headerSlot?: ReactNode;
  /**
   * If false, the component skips its own PageHeader and renders only the
   * form body. Useful when the wrapping editor-page already owns the chrome.
   */
  embedded?: boolean;
}

// Form-based workflow builder. Owns its own state. The mode toggle in the
// editor-page wrapper is responsible for switching between this and the
// node-canvas builder; switching loses unsaved edits by design (workflows
// are small enough that round-tripping through buildPayload would obscure
// the user's intent more than it'd help).
export function WorkflowForm({ workflowId, headerSlot, embedded }: WorkflowFormProps) {
  const featureEnabled = useFeatureFlag("cerebro_workflows");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  const queryClient = useQueryClient();

  const wsId = workspace?.id ?? "";

  const detail = useQuery({
    ...cerebroWorkflowDetailOptions(wsId, workflowId ?? ""),
    enabled: !!workflowId && !!wsId,
  });

  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!detail.data) return;
    setForm(formStateFromWorkflow(detail.data));
  }, [detail.data]);

  const save = useMutation({
    mutationFn: async () => {
      const payload: WorkflowWriteInput = {
        ...buildPayload(form),
        editor_mode: "form",
      };
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

  if (!featureEnabled) return null;
  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  const body = (
    <div className="flex-1 min-h-0 overflow-y-auto">
      <form
        className="mx-auto flex max-w-2xl flex-col gap-6 p-6"
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          save.mutate();
        }}
      >
        {!workflowId && (
          <Section title="Start fra en skabelon (valgfri)">
            <ul className="flex flex-col divide-y">
              {WORKFLOW_TEMPLATES.map((tpl) => (
                <li
                  key={tpl.key}
                  className="flex items-start justify-between gap-3 py-2 first:pt-0 last:pb-0"
                >
                  <div className="min-w-0">
                    <div className="text-sm font-medium">{tpl.label}</div>
                    <div className="text-[11px] text-muted-foreground">{tpl.description}</div>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={tpl.status !== "active"}
                    data-testid={`template-use-${tpl.key}`}
                    onClick={() => {
                      if (tpl.status === "active" && tpl.defaults) {
                        setForm(formStateFromInput(tpl.defaults));
                      }
                    }}
                  >
                    {tpl.status === "active" ? "Brug" : "Kommer i fase 4"}
                  </Button>
                </li>
              ))}
            </ul>
          </Section>
        )}

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
          {form.triggerType === "cron" && (
            <CronTriggerFields
              schedule={form.cronSchedule}
              timezone={form.cronTimezone}
              onChangeSchedule={(v) => setForm({ ...form, cronSchedule: v })}
              onChangeTimezone={(v) => setForm({ ...form, cronTimezone: v })}
            />
          )}
          {form.triggerType === "webhook_inbound" && (
            <WebhookInboundFields
              workflowId={workflowId}
              wsId={wsId}
              hmacToggled={form.inboundHmacToggled}
              onChangeHmacToggled={(v) => setForm({ ...form, inboundHmacToggled: v })}
              oneTimeSecret={form.inboundOneTimeSecret}
              onChangeOneTimeSecret={(v) => setForm({ ...form, inboundOneTimeSecret: v })}
            />
          )}
          {form.triggerType === "comment_mention" && (
            <CommentMentionFields
              matchMode={form.commentMatchMode}
              matchTarget={form.commentMatchTarget}
              onChangeMatchMode={(v) => setForm({ ...form, commentMatchMode: v })}
              onChangeMatchTarget={(v) => setForm({ ...form, commentMatchTarget: v })}
            />
          )}
          {form.triggerType === "all_children_done" && <AllChildrenDoneFields />}
          {form.triggerType === "sub_issue_created" && (
            <SubIssueCreatedFields
              parentIssueId={form.subIssueCreatedParentId}
              onChangeParentIssueId={(v) => setForm({ ...form, subIssueCreatedParentId: v })}
            />
          )}
        </Section>

        <Section title="Conditions (avanceret, valgfri)">
          <Field label="Conditions JSON">
            <Textarea
              value={form.conditionsJSON}
              onChange={(e) => setForm({ ...form, conditionsJSON: e.target.value })}
              rows={6}
              placeholder='[]'
              data-testid="conditions-json"
              className="font-mono text-[12px]"
            />
          </Field>
          <p className="text-[11px] text-muted-foreground">
            Conditions filtrerer efter trigger og før action. Skabeloner kan
            pre-udfylde dette felt (fx <code>evidence_present</code> til at
            gate <code>set_status: done</code>). En dedikeret editor lander
            i en senere PR; rediger JSON direkte indtil da. Tom array
            betyder "kør altid".
          </p>
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
              <Field label="Assignee-type (valgfri)">
                <NativeSelect
                  value={form.subIssueAssigneeType}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      subIssueAssigneeType: e.target.value as "member" | "agent",
                    })
                  }
                >
                  <option value="agent">Agent</option>
                  <option value="member">Member</option>
                </NativeSelect>
              </Field>
              <Field label="Assignee-ID (UUID, valgfri)">
                <Input
                  value={form.subIssueAssigneeId}
                  onChange={(e) => setForm({ ...form, subIssueAssigneeId: e.target.value })}
                  placeholder="<uuid>"
                />
              </Field>
              <Field label="Label-IDs (komma-separeret UUIDs, valgfri)">
                <Input
                  value={form.subIssueLabelIDs}
                  onChange={(e) => setForm({ ...form, subIssueLabelIDs: e.target.value })}
                  placeholder="<uuid>,<uuid>"
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

          {form.actionType === "run_skill" && (
            <>
              <Field label="Skill (navn fra workspace)">
                <Input
                  value={form.skillName}
                  onChange={(e) => setForm({ ...form, skillName: e.target.value })}
                  required
                  placeholder="firtal-data-evaluate"
                />
              </Field>
              <Field label="Agent-ID (skal have skillen attached)">
                <Input
                  value={form.skillAgentId}
                  onChange={(e) => setForm({ ...form, skillAgentId: e.target.value })}
                  required
                  placeholder="<uuid>"
                />
              </Field>
              <Field label="Skill-input (JSON)">
                <Textarea
                  value={form.skillInputJSON}
                  onChange={(e) => setForm({ ...form, skillInputJSON: e.target.value })}
                  rows={4}
                  placeholder='{"query": "..."}'
                  data-testid="run-skill-input"
                />
              </Field>
            </>
          )}

          {form.actionType === "comment_on_issue" && (
            <>
              <Field label="Target">
                <NativeSelect
                  value={form.commentTarget}
                  onChange={(e) =>
                    setForm({ ...form, commentTarget: e.target.value as "self" | "parent" })
                  }
                >
                  <option value="self">Triggerede issue</option>
                  <option value="parent">Parent-issue</option>
                </NativeSelect>
              </Field>
              <Field label="Indhold (template-felter understøttes)">
                <Textarea
                  value={form.commentContent}
                  onChange={(e) => setForm({ ...form, commentContent: e.target.value })}
                  rows={4}
                  required
                  placeholder="Opdatering på {{title}}: ..."
                />
              </Field>
            </>
          )}

          {form.actionType === "route_by_domain" && (
            <>
              <Field label="Label-præfiks">
                <Input
                  value={form.routeLabelPrefix}
                  onChange={(e) =>
                    setForm({ ...form, routeLabelPrefix: e.target.value })
                  }
                  placeholder="domain:"
                  data-testid="route-label-prefix"
                />
              </Field>
              <Field label="Default-domæne (når heuristikken ikke matcher)">
                <NativeSelect
                  value={form.routeDefaultDomain}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      routeDefaultDomain: e.target
                        .value as FormState["routeDefaultDomain"],
                    })
                  }
                  data-testid="route-default-domain"
                >
                  <option value="code">code</option>
                  <option value="business">business</option>
                  <option value="design">design</option>
                  <option value="content">content</option>
                </NativeSelect>
              </Field>
              <p className="text-[11px] text-muted-foreground">
                Workspacet skal have et label per domæne (f.eks.{" "}
                <code>domain:code</code>). Mangler labelet, fejler kørslen
                med en klar besked — opret det under Settings → Labels.
              </p>
            </>
          )}

          {form.actionType === "webhook_outbound" && (
            <WebhookOutboundFields
              workflowId={workflowId}
              wsId={wsId}
              url={form.webhookOutboundUrl}
              headers={form.webhookOutboundHeaders}
              includeIssueSnapshot={form.webhookOutboundIncludeIssueSnapshot}
              onChangeUrl={(v) => setForm({ ...form, webhookOutboundUrl: v })}
              onChangeHeaders={(h) => setForm({ ...form, webhookOutboundHeaders: h })}
              onChangeIncludeIssueSnapshot={(v) =>
                setForm({ ...form, webhookOutboundIncludeIssueSnapshot: v })
              }
            />
          )}

          {form.actionType === "reassign_issue" && (
            <ReassignIssueFields
              assigneeType={form.reassignAssigneeType}
              assigneeId={form.reassignAssigneeId}
              onChangeAssigneeType={(v) => setForm({ ...form, reassignAssigneeType: v })}
              onChangeAssigneeId={(v) => setForm({ ...form, reassignAssigneeId: v })}
            />
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
  );

  if (embedded) return body;

  const heading = workflowId ? "Rediger workflow" : "Nyt workflow";
  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">{heading}</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            Trigger → conditions → action. Form-mode (skift til canvas øverst for node-graf).
          </p>
        </div>
        {headerSlot}
      </PageHeader>
      {body}
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

// formStateFromInput unfolds the flat WorkflowWriteInput stored on a template
// back into the (looser, UI-only) FormState shape. Inverse of buildPayload.
// Unknown fields default to EMPTY_FORM so a template doesn't have to specify
// every action_config slot.
export function formStateFromInput(input: WorkflowWriteInput): FormState {
  const tc = (input.trigger_config as Record<string, unknown> | null) ?? {};
  const ac = (input.action_config as Record<string, unknown> | null) ?? {};
  const labelIDs = Array.isArray(ac.label_ids) ? (ac.label_ids as string[]).join(",") : "";
  return {
    ...EMPTY_FORM,
    name: input.name ?? "",
    enabled: input.enabled ?? true,
    triggerType: input.trigger_type,
    fromStatus: (tc.from_status as string) ?? "",
    toStatus: (tc.to_status as string) ?? EMPTY_FORM.toStatus,
    actionType: input.action_type,
    setStatus: (ac.status as string) ?? EMPTY_FORM.setStatus,
    subIssueTitle: (ac.title as string) ?? "",
    subIssueDescription: (ac.description as string) ?? "",
    subIssueAssigneeId: (ac.assignee_id as string) ?? "",
    subIssueAssigneeType: (ac.assignee_type as "member" | "agent") ?? EMPTY_FORM.subIssueAssigneeType,
    subIssueLabelIDs: labelIDs,
    reminderRecipientId: (ac.recipient_id as string) ?? "",
    reminderRecipientType: ((ac.recipient_type as "member" | "agent") ?? "member"),
    reminderMessage: (ac.message as string) ?? "",
    skillName: (ac.skill_name as string) ?? "",
    skillAgentId: (ac.agent_id as string) ?? "",
    skillInputJSON:
      ac.skill_input === undefined
        ? EMPTY_FORM.skillInputJSON
        : safeStringifyJSON(ac.skill_input),
    commentTarget: (ac.target as "self" | "parent") ?? EMPTY_FORM.commentTarget,
    commentContent: (ac.content as string) ?? "",
    routeLabelPrefix:
      (ac.label_prefix as string) ?? EMPTY_FORM.routeLabelPrefix,
    routeDefaultDomain:
      isRouteDomain(ac.default_domain)
        ? (ac.default_domain as FormState["routeDefaultDomain"])
        : EMPTY_FORM.routeDefaultDomain,
    conditionsJSON: stringifyConditions(input.conditions),
    // Phase 3.
    cronSchedule: (tc.schedule_expr as string) ?? EMPTY_FORM.cronSchedule,
    cronTimezone: (tc.timezone as string) ?? EMPTY_FORM.cronTimezone,
    commentMatchMode: (tc.match_mode as CerebroCommentMatchMode) ?? EMPTY_FORM.commentMatchMode,
    commentMatchTarget: (tc.target as string) ?? "",
    subIssueCreatedParentId: (tc.parent_issue_id as string) ?? "",
    webhookOutboundUrl: (ac.url as string) ?? "",
    webhookOutboundHeaders: headersFromAC(ac.headers),
    webhookOutboundIncludeIssueSnapshot:
      typeof ac.include_issue_snapshot === "boolean"
        ? (ac.include_issue_snapshot as boolean)
        : EMPTY_FORM.webhookOutboundIncludeIssueSnapshot,
    reassignAssigneeType: (ac.assignee_type as CerebroAssigneeType) ?? EMPTY_FORM.reassignAssigneeType,
    reassignAssigneeId:
      typeof ac.assignee_id === "string" && input.action_type === "reassign_issue"
        ? (ac.assignee_id as string)
        : EMPTY_FORM.reassignAssigneeId,
  };
}

function stringifyConditions(c: unknown): string {
  if (c === undefined || c === null) return EMPTY_FORM.conditionsJSON;
  // Accept both array and stringified array. Stringify with indent so the
  // textarea renders readably; users editing it should still produce valid
  // JSON, which buildPayload validates at save-time.
  try {
    const value = typeof c === "string" ? JSON.parse(c) : c;
    return JSON.stringify(value, null, 2);
  } catch {
    return EMPTY_FORM.conditionsJSON;
  }
}

function isRouteDomain(v: unknown): v is FormState["routeDefaultDomain"] {
  return v === "code" || v === "business" || v === "design" || v === "content";
}

function headersFromAC(raw: unknown): Array<{ key: string; value: string }> {
  if (!raw || typeof raw !== "object") return [];
  const out: Array<{ key: string; value: string }> = [];
  for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
    if (typeof k === "string" && typeof v === "string") {
      out.push({ key: k, value: v });
    }
  }
  return out;
}

// formStateFromWorkflow accepts a CerebroWorkflow row (subset of WriteInput
// fields). Tiny adapter so the detail-query result can pre-fill the form.
function formStateFromWorkflow(row: {
  name: string;
  enabled: boolean;
  trigger_type: WorkflowWriteInput["trigger_type"];
  trigger_config: unknown;
  action_type: WorkflowWriteInput["action_type"];
  action_config: unknown;
}): FormState {
  return formStateFromInput({
    name: row.name,
    enabled: row.enabled,
    trigger_type: row.trigger_type,
    trigger_config: row.trigger_config,
    action_type: row.action_type,
    action_config: row.action_config,
  });
}

export function buildPayload(f: FormState): WorkflowWriteInput {
  let triggerConfig: Record<string, unknown> = {};
  if (f.triggerType === "status_changed") {
    triggerConfig = {
      from_status: f.fromStatus || undefined,
      to_status: f.toStatus || undefined,
    };
  } else if (f.triggerType === "cron") {
    triggerConfig = {
      schedule_expr: f.cronSchedule,
      timezone: f.cronTimezone || undefined,
    };
  } else if (f.triggerType === "comment_mention") {
    triggerConfig = {
      match_mode: f.commentMatchMode,
      target: f.commentMatchTarget,
    };
  } else if (f.triggerType === "sub_issue_created") {
    triggerConfig = {
      parent_issue_id: f.subIssueCreatedParentId || undefined,
    };
  }
  // all_children_done + webhook_inbound + due_* triggers have no trigger_config.

  let actionConfig: Record<string, unknown> = {};
  if (f.actionType === "set_status") {
    actionConfig = { status: f.setStatus };
  } else if (f.actionType === "create_sub_issue") {
    const labelIDs = f.subIssueLabelIDs
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s.length > 0);
    actionConfig = {
      title: f.subIssueTitle,
      description: f.subIssueDescription || undefined,
      assignee_id: f.subIssueAssigneeId || undefined,
      assignee_type: f.subIssueAssigneeId ? f.subIssueAssigneeType : undefined,
      label_ids: labelIDs.length > 0 ? labelIDs : undefined,
    };
  } else if (f.actionType === "send_reminder") {
    actionConfig = {
      recipient_id: f.reminderRecipientId,
      recipient_type: f.reminderRecipientType,
      message: f.reminderMessage,
    };
  } else if (f.actionType === "run_skill") {
    actionConfig = {
      skill_name: f.skillName,
      agent_id: f.skillAgentId,
      skill_input: safeParseJSON(f.skillInputJSON),
    };
  } else if (f.actionType === "comment_on_issue") {
    actionConfig = {
      target: f.commentTarget,
      content: f.commentContent,
    };
  } else if (f.actionType === "route_by_domain") {
    actionConfig = {
      label_prefix: f.routeLabelPrefix || undefined,
      default_domain: f.routeDefaultDomain,
    };
  } else if (f.actionType === "webhook_outbound") {
    const headers: Record<string, string> = {};
    for (const { key, value } of f.webhookOutboundHeaders) {
      if (key.trim()) headers[key.trim()] = value;
    }
    actionConfig = {
      url: f.webhookOutboundUrl,
      headers: Object.keys(headers).length > 0 ? headers : undefined,
      include_issue_snapshot: f.webhookOutboundIncludeIssueSnapshot,
    };
  } else if (f.actionType === "reassign_issue") {
    actionConfig = {
      assignee_id: f.reassignAssigneeId,
      assignee_type: f.reassignAssigneeType,
    };
  }

  return {
    name: f.name,
    enabled: f.enabled,
    trigger_type: f.triggerType,
    trigger_config: triggerConfig,
    conditions: parseConditionsJSON(f.conditionsJSON),
    action_type: f.actionType,
    action_config: actionConfig,
  };
}

// parseConditionsJSON returns the parsed conditions array on success, or []
// on parse failure. Templates and the API ship arrays; the form's textarea
// is the only edit surface that can produce invalid JSON, so failing soft
// here keeps a half-typed value from blowing up the save click. The server
// re-validates the shape regardless.
function parseConditionsJSON(raw: string): unknown[] {
  if (!raw.trim()) return [];
  try {
    const v = JSON.parse(raw);
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}

// safeParseJSON returns the parsed object on success, an empty object on any
// parse failure. The server-side validator pins the strict shape; this side
// just shouldn't crash on a mistyped trailing comma during edit.
function safeParseJSON(raw: string): Record<string, unknown> {
  if (!raw.trim()) return {};
  try {
    const v = JSON.parse(raw);
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function safeStringifyJSON(v: unknown): string {
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return "{}";
  }
}
