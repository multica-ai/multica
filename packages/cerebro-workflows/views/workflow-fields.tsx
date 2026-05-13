"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import cronParser from "cron-parser";

import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { Switch } from "@multica/ui/components/ui/switch";

import {
  FORBIDDEN_OUTBOUND_HEADER_PREFIXES,
  FORBIDDEN_OUTBOUND_HEADERS,
  HEADER_NAME_RE,
  cerebroWorkflowDetailOptions,
  useRegenerateInboundSigningSecretMutation,
  useRegenerateInboundTokenMutation,
  useRegenerateOutboundSecretMutation,
} from "../core";
import type {
  CerebroAssigneeType,
  CerebroCommentMatchMode,
} from "../core/types";

// FormState extension fields used by the phase-3 trigger/action editors.
// Kept here so the form-builder + the canvas inspector share the same
// shape definitions, avoiding parallel drift.
export interface WorkflowPhase3FormFields {
  // cron trigger
  cronSchedule: string;
  cronTimezone: string;
  // webhook_inbound trigger — only the freshly-minted secret (one-time
  // reveal) lives in form state. The token + secret-set bools come from
  // the workflow row via the detail query.
  inboundOneTimeSecret: string;
  inboundHmacToggled: boolean;
  // comment_mention trigger
  commentMatchMode: CerebroCommentMatchMode;
  commentMatchTarget: string;
  // sub_issue_created trigger
  subIssueCreatedParentId: string;
  // webhook_outbound action
  webhookOutboundUrl: string;
  webhookOutboundHeaders: Array<{ key: string; value: string }>;
  webhookOutboundIncludeIssueSnapshot: boolean;
  // reassign_issue action
  reassignAssigneeType: CerebroAssigneeType;
  reassignAssigneeId: string;
}

export const PHASE3_EMPTY: WorkflowPhase3FormFields = {
  cronSchedule: "0 9 * * *",
  cronTimezone: "Europe/Copenhagen",
  inboundOneTimeSecret: "",
  inboundHmacToggled: false,
  commentMatchMode: "agent",
  commentMatchTarget: "",
  subIssueCreatedParentId: "",
  webhookOutboundUrl: "",
  webhookOutboundHeaders: [],
  webhookOutboundIncludeIssueSnapshot: true,
  reassignAssigneeType: "agent",
  reassignAssigneeId: "",
};

export function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label className="text-xs">{label}</Label>
      {children}
    </div>
  );
}

// --- Cron preview ---

interface CronPreviewProps {
  schedule: string;
  timezone: string;
}

// CronPreview shows the next 5 fire-times so the user can sanity-check a
// schedule before saving. cron-parser v4 throws on invalid expressions —
// we display the message inline (server-side validateCronSchedule will
// reject too, but instant feedback is friendlier).
export function CronPreview({ schedule, timezone }: CronPreviewProps) {
  const result = useMemo(() => previewNextFires(schedule, timezone, 5), [schedule, timezone]);
  if ("error" in result) {
    return (
      <div className="rounded-md border border-destructive/40 bg-destructive/5 px-2 py-1 text-[11px] text-destructive">
        Invalid cron expression: {result.error}
      </div>
    );
  }
  return (
    <ol className="flex list-decimal flex-col gap-0.5 pl-4 text-[11px] text-muted-foreground">
      {result.fires.map((d) => (
        <li key={d}>{d}</li>
      ))}
    </ol>
  );
}

function previewNextFires(
  expr: string,
  tz: string,
  n: number,
): { fires: string[] } | { error: string } {
  if (!expr.trim()) return { error: "tom — angiv et cron-udtryk" };
  try {
    const opts = tz.trim() ? { tz: tz.trim() } : undefined;
    // cron-parser v4 exposes parseExpression on the default export.
    const it = cronParser.parseExpression(expr.trim(), opts);
    const out: string[] = [];
    for (let i = 0; i < n; i++) {
      out.push(it.next().toDate().toISOString());
    }
    return { fires: out };
  } catch (err) {
    return { error: err instanceof Error ? err.message : "unknown parse error" };
  }
}

// --- Cron trigger fields ---

interface CronTriggerFieldsProps {
  schedule: string;
  timezone: string;
  onChangeSchedule: (v: string) => void;
  onChangeTimezone: (v: string) => void;
}

export function CronTriggerFields({
  schedule,
  timezone,
  onChangeSchedule,
  onChangeTimezone,
}: CronTriggerFieldsProps) {
  return (
    <>
      <Field label="Cron-udtryk (m h dom mon dow)">
        <Input
          value={schedule}
          onChange={(e) => onChangeSchedule(e.target.value)}
          required
          placeholder="0 9 * * *"
          data-testid="cron-schedule-expr"
        />
      </Field>
      <Field label="Tidszone (IANA, fx Europe/Copenhagen)">
        <Input
          value={timezone}
          onChange={(e) => onChangeTimezone(e.target.value)}
          placeholder="Europe/Copenhagen"
          data-testid="cron-timezone"
        />
      </Field>
      <Field label="Næste 5 fire-tider">
        <CronPreview schedule={schedule} timezone={timezone} />
      </Field>
    </>
  );
}

// --- Webhook-inbound trigger fields ---

interface WebhookInboundFieldsProps {
  workflowId?: string;
  wsId: string;
  hmacToggled: boolean;
  onChangeHmacToggled: (v: boolean) => void;
  oneTimeSecret: string;
  onChangeOneTimeSecret: (v: string) => void;
}

export function WebhookInboundFields({
  workflowId,
  wsId,
  hmacToggled,
  onChangeHmacToggled,
  oneTimeSecret,
  onChangeOneTimeSecret,
}: WebhookInboundFieldsProps) {
  const detail = useQuery({
    ...cerebroWorkflowDetailOptions(wsId, workflowId ?? ""),
    enabled: !!workflowId && !!wsId,
  });

  const token = detail.data?.inbound_webhook_token ?? "";
  const signingSecretSet = detail.data?.inbound_signing_secret_set === true;
  const origin = typeof window !== "undefined" ? window.location.origin : "";
  const url = token ? `${origin}/api/cerebro/workflows/webhook/${token}` : "";

  const [localToken, setLocalToken] = useState(token);
  const [localUrl, setLocalUrl] = useState(url);
  useEffect(() => {
    setLocalToken(token);
    setLocalUrl(url);
  }, [token, url]);

  // Canonical mutation hooks from core/queries.ts — invalidate the detail
  // query on success so the masked *_set bool flips from false → true
  // automatically. We layer local-state side effects in onSuccess for the
  // one-time plaintext reveal (regenerate response is only ever returned
  // once, so we stash it client-side until the user navigates away).
  const tokenMutation = useRegenerateInboundTokenMutation(wsId, workflowId ?? "");
  const secretMutation = useRegenerateInboundSigningSecretMutation(wsId, workflowId ?? "");

  const runTokenRegen = () => {
    if (!workflowId) return;
    tokenMutation.mutate(undefined, {
      onSuccess: (data) => {
        setLocalToken(data.inbound_webhook_token);
        setLocalUrl(data.inbound_webhook_url);
      },
    });
  };

  const runSecretRegen = () => {
    if (!workflowId) return;
    secretMutation.mutate(undefined, {
      onSuccess: (data) => {
        onChangeOneTimeSecret(data.inbound_signing_secret);
      },
    });
  };

  const copy = () => {
    if (localUrl && typeof navigator !== "undefined" && navigator.clipboard) {
      void navigator.clipboard.writeText(localUrl);
    }
  };

  return (
    <div className="flex flex-col gap-3">
      {!workflowId && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/5 px-2 py-1 text-[11px] text-amber-700 dark:text-amber-300">
          Gem workflowet først for at få genereret et webhook-token og en URL.
        </div>
      )}

      <Field label="Webhook-URL (kopiér til kilde-systemet)">
        <div className="flex items-center gap-2">
          <Input value={localUrl} readOnly placeholder="Genereres når token er sat" />
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!localUrl}
            onClick={copy}
            data-testid="inbound-webhook-copy"
          >
            Kopiér
          </Button>
        </div>
      </Field>

      <Field label="Token">
        <div className="flex items-center gap-2">
          <Input value={localToken} readOnly placeholder="Generér for at få et token" />
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!workflowId || tokenMutation.isPending}
            onClick={runTokenRegen}
            data-testid="inbound-webhook-rotate-token"
          >
            {tokenMutation.isPending ? "…" : "Rotér"}
          </Button>
        </div>
      </Field>

      <Field label="HMAC-armer modtagelse (anbefales i prod)">
        <Switch
          checked={hmacToggled || signingSecretSet}
          onCheckedChange={(v) => onChangeHmacToggled(v === true)}
        />
      </Field>

      {(hmacToggled || signingSecretSet) && (
        <Field label="Signing-secret">
          {oneTimeSecret ? (
            <div className="rounded-md border border-emerald-500/40 bg-emerald-500/5 px-2 py-1 font-mono text-[11px] text-emerald-700 dark:text-emerald-300">
              {oneTimeSecret}
              <div className="mt-1 text-[10px] text-muted-foreground">
                Gem denne nu — secret vises kun denne ene gang.
              </div>
            </div>
          ) : signingSecretSet ? (
            <div className="text-[11px] text-muted-foreground">
              Secret armeret — rotér for at se en ny.
            </div>
          ) : (
            <div className="text-[11px] text-muted-foreground">Secret ikke sat.</div>
          )}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="mt-2 w-fit"
            disabled={!workflowId || secretMutation.isPending}
            onClick={runSecretRegen}
            data-testid="inbound-webhook-rotate-secret"
          >
            {secretMutation.isPending ? "…" : "Generér signing-secret"}
          </Button>
        </Field>
      )}
    </div>
  );
}

// --- Comment-mention trigger fields ---

interface CommentMentionFieldsProps {
  matchMode: CerebroCommentMatchMode;
  matchTarget: string;
  onChangeMatchMode: (v: CerebroCommentMatchMode) => void;
  onChangeMatchTarget: (v: string) => void;
}

export function CommentMentionFields({
  matchMode,
  matchTarget,
  onChangeMatchMode,
  onChangeMatchTarget,
}: CommentMentionFieldsProps) {
  const regexError = useMemo(() => {
    if (matchMode !== "regex" || !matchTarget) return null;
    try {
      new RegExp(matchTarget);
      return null;
    } catch (err) {
      return err instanceof Error ? err.message : "ugyldig regex";
    }
  }, [matchMode, matchTarget]);

  const placeholder =
    matchMode === "agent" || matchMode === "member"
      ? "Agent/member UUID"
      : matchMode === "keyword"
        ? "Søgeord (case-insensitive substring)"
        : "Regex-mønster, fx ^/skill\\s+(\\w+)";

  const label =
    matchMode === "agent" || matchMode === "member"
      ? "UUID"
      : matchMode === "keyword"
        ? "Søgeord"
        : "Regex-mønster";

  return (
    <>
      <Field label="Match-mode">
        <NativeSelect
          value={matchMode}
          onChange={(e) => onChangeMatchMode(e.target.value as CerebroCommentMatchMode)}
          data-testid="comment-mention-match-mode"
        >
          <option value="agent">Agent (@mention)</option>
          <option value="member">Member (@mention)</option>
          <option value="keyword">Keyword (substring)</option>
          <option value="regex">Regex</option>
        </NativeSelect>
      </Field>
      <Field label={label}>
        <Input
          value={matchTarget}
          onChange={(e) => onChangeMatchTarget(e.target.value)}
          required
          placeholder={placeholder}
          className={matchMode === "regex" ? "font-mono" : undefined}
          data-testid="comment-mention-target"
        />
        {regexError && (
          <div className="text-[11px] text-destructive">Regex-fejl: {regexError}</div>
        )}
      </Field>
    </>
  );
}

// --- All-children-done trigger fields (no config) ---

export function AllChildrenDoneFields() {
  return (
    <div className="rounded-md border border-muted bg-muted/30 px-2 py-2 text-[11px] text-muted-foreground">
      Ingen yderligere config — workflowet fyrer når alle sub-issues af det
      trigger-issue når status &apos;done&apos;.
    </div>
  );
}

// --- Sub-issue-created trigger fields ---

interface SubIssueCreatedFieldsProps {
  parentIssueId: string;
  onChangeParentIssueId: (v: string) => void;
}

export function SubIssueCreatedFields({
  parentIssueId,
  onChangeParentIssueId,
}: SubIssueCreatedFieldsProps) {
  return (
    <Field label="Parent issue UUID (tom = alle sub-issues i workspace)">
      <Input
        value={parentIssueId}
        onChange={(e) => onChangeParentIssueId(e.target.value)}
        placeholder="<uuid>"
        data-testid="sub-issue-created-parent"
      />
    </Field>
  );
}

// --- Webhook-outbound action fields ---

interface WebhookOutboundFieldsProps {
  workflowId?: string;
  wsId: string;
  url: string;
  headers: Array<{ key: string; value: string }>;
  includeIssueSnapshot: boolean;
  onChangeUrl: (v: string) => void;
  onChangeHeaders: (h: Array<{ key: string; value: string }>) => void;
  onChangeIncludeIssueSnapshot: (v: boolean) => void;
}

export function WebhookOutboundFields({
  workflowId,
  wsId,
  url,
  headers,
  includeIssueSnapshot,
  onChangeUrl,
  onChangeHeaders,
  onChangeIncludeIssueSnapshot,
}: WebhookOutboundFieldsProps) {
  const detail = useQuery({
    ...cerebroWorkflowDetailOptions(wsId, workflowId ?? ""),
    enabled: !!workflowId && !!wsId,
  });
  const outboundSet = detail.data?.outbound_webhook_secret_set === true;
  const [oneTimeSecret, setOneTimeSecret] = useState("");

  const secretMutation = useRegenerateOutboundSecretMutation(wsId, workflowId ?? "");

  const runSecretRegen = () => {
    if (!workflowId) return;
    secretMutation.mutate(undefined, {
      onSuccess: (data) => {
        setOneTimeSecret(data.outbound_webhook_secret);
      },
    });
  };

  const urlError = useMemo(() => {
    if (!url) return "URL er påkrævet";
    try {
      const u = new URL(url);
      if (u.protocol !== "https:" && u.protocol !== "http:") return "kun http(s) tilladt";
      return null;
    } catch {
      return "ugyldig URL";
    }
  }, [url]);

  const addHeader = () => {
    onChangeHeaders([...headers, { key: "", value: "" }]);
  };

  const updateHeader = (i: number, patch: { key?: string; value?: string }) => {
    const next = headers.map((h, idx) => (idx === i ? { ...h, ...patch } : h));
    onChangeHeaders(next);
  };

  const removeHeader = (i: number) => {
    onChangeHeaders(headers.filter((_, idx) => idx !== i));
  };

  return (
    <>
      <Field label="URL">
        <Input
          value={url}
          onChange={(e) => onChangeUrl(e.target.value)}
          required
          placeholder="https://example.com/webhook"
          data-testid="webhook-outbound-url"
        />
        {urlError && <div className="text-[11px] text-destructive">{urlError}</div>}
      </Field>

      <Field label="Custom headers (key/value)">
        <div className="flex flex-col gap-2">
          {headers.map((h, i) => {
            const headerErr = validateHeaderName(h.key);
            return (
              <div key={i} className="flex items-start gap-2">
                <div className="flex flex-1 flex-col">
                  <Input
                    value={h.key}
                    onChange={(e) => updateHeader(i, { key: e.target.value })}
                    placeholder="X-My-Header"
                  />
                  {headerErr && (
                    <div className="text-[10px] text-destructive">{headerErr}</div>
                  )}
                </div>
                <Input
                  value={h.value}
                  onChange={(e) => updateHeader(i, { value: e.target.value })}
                  placeholder="value"
                  className="flex-1"
                />
                <Button type="button" variant="outline" size="sm" onClick={() => removeHeader(i)}>
                  Fjern
                </Button>
              </div>
            );
          })}
          <Button type="button" variant="outline" size="sm" className="w-fit" onClick={addHeader}>
            Tilføj header
          </Button>
        </div>
      </Field>

      <Field label="Inkludér issue-snapshot i body">
        <Switch
          checked={includeIssueSnapshot}
          onCheckedChange={(v) => onChangeIncludeIssueSnapshot(v === true)}
        />
      </Field>

      <Field label="Outbound signing-secret (HMAC af request body)">
        {oneTimeSecret ? (
          <div className="rounded-md border border-emerald-500/40 bg-emerald-500/5 px-2 py-1 font-mono text-[11px] text-emerald-700 dark:text-emerald-300">
            {oneTimeSecret}
            <div className="mt-1 text-[10px] text-muted-foreground">
              Gem denne nu — secret vises kun denne ene gang.
            </div>
          </div>
        ) : outboundSet ? (
          <div className="text-[11px] text-muted-foreground">
            Secret armeret — rotér for at se en ny.
          </div>
        ) : (
          <div className="text-[11px] text-muted-foreground">Secret ikke sat.</div>
        )}
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-2 w-fit"
          disabled={!workflowId || secretMutation.isPending}
          onClick={runSecretRegen}
          data-testid="webhook-outbound-rotate-secret"
        >
          {secretMutation.isPending ? "…" : "Generér outbound-secret"}
        </Button>
      </Field>
    </>
  );
}

function validateHeaderName(name: string): string | null {
  if (!name) return null;
  if (!HEADER_NAME_RE.test(name)) return "kun A-Z, a-z, 0-9 og '-' tilladt";
  const lower = name.toLowerCase();
  if (FORBIDDEN_OUTBOUND_HEADERS.includes(lower)) {
    return `'${name}' er reserveret`;
  }
  for (const prefix of FORBIDDEN_OUTBOUND_HEADER_PREFIXES) {
    if (lower.startsWith(prefix)) return `'${name}' bruger reserveret prefix`;
  }
  return null;
}

// --- Reassign-issue action fields ---

interface ReassignIssueFieldsProps {
  assigneeType: CerebroAssigneeType;
  assigneeId: string;
  onChangeAssigneeType: (v: CerebroAssigneeType) => void;
  onChangeAssigneeId: (v: string) => void;
}

export function ReassignIssueFields({
  assigneeType,
  assigneeId,
  onChangeAssigneeType,
  onChangeAssigneeId,
}: ReassignIssueFieldsProps) {
  return (
    <>
      <Field label="Assignee-type">
        <NativeSelect
          value={assigneeType}
          onChange={(e) => onChangeAssigneeType(e.target.value as CerebroAssigneeType)}
          data-testid="reassign-assignee-type"
        >
          <option value="agent">Agent</option>
          <option value="member">Member</option>
        </NativeSelect>
      </Field>
      <Field label="Assignee UUID">
        <Input
          value={assigneeId}
          onChange={(e) => onChangeAssigneeId(e.target.value)}
          required
          placeholder="<uuid>"
          data-testid="reassign-assignee-id"
        />
      </Field>
    </>
  );
}
