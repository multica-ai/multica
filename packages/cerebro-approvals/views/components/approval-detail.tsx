"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Check, Loader2, Share2, X } from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  approvalDetailOptions,
  approvalAuditOptions,
} from "../../core/queries";
import {
  useApproveApproval,
  useDelegateApproval,
  useRejectApproval,
} from "../../core/mutations";
import type {
  Approval,
  DelegateTargetType,
  GrantLevel,
} from "../../core/types";
import { StatusBadge } from "./status-badge";

// Duration presets for "approve for a period" → grant_for_seconds (TECH-3498).
const GRANT_DURATIONS: { label: string; seconds: number }[] = [
  { label: "Once", seconds: 0 },
  { label: "1 hour", seconds: 3600 },
  { label: "24 hours", seconds: 86400 },
  { label: "7 days", seconds: 604800 },
];

// Permission-level presets for "also allow at level" → grant_at_level. The empty
// value means "no escalation" (the default).
const GRANT_LEVELS: { label: string; value: "" | GrantLevel }[] = [
  { label: "None", value: "" },
  { label: "This agent", value: "agent" },
  { label: "This runtime", value: "runtime" },
  { label: "Whole workspace", value: "workspace" },
];

// approvalContext is the optional, untyped context map the backend attaches to an
// ask. It crosses the API boundary, so every field is read defensively (see
// CLAUDE.md API Response Compatibility): unknown shapes degrade to nothing.
interface AskContext {
  connection?: string;
  purpose?: string;
  task_id?: string;
  args?: unknown;
}

function readAskContext(raw: unknown): AskContext {
  if (!raw || typeof raw !== "object") return {};
  const obj = raw as Record<string, unknown>;
  return {
    connection: typeof obj.connection === "string" ? obj.connection : undefined,
    purpose: typeof obj.purpose === "string" ? obj.purpose : undefined,
    task_id: typeof obj.task_id === "string" ? obj.task_id : undefined,
    args: obj.args,
  };
}

// compactArgs JSON-stringifies the args blob and truncates it so a large payload
// does not blow out the sheet. Returns "" when there is nothing useful to show.
function compactArgs(args: unknown): string {
  if (args === undefined || args === null) return "";
  let json: string;
  try {
    json = JSON.stringify(args);
  } catch {
    return "";
  }
  if (!json || json === "{}" || json === "null" || json === "[]") return "";
  const max = 240;
  return json.length > max ? `${json.slice(0, max)}…` : json;
}

interface ApprovalDetailProps {
  wsId: string;
  approvalId: string;
  onClose: () => void;
}

export function ApprovalDetail({ wsId, approvalId, onClose }: ApprovalDetailProps) {
  const detail = useQuery(approvalDetailOptions(wsId, approvalId));
  const ask = detail.data ?? null;

  return (
    <Sheet open={true} onOpenChange={(o) => !o && onClose()}>
      <SheetContent side="right" className="w-full max-w-[560px] overflow-y-auto p-0">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="text-sm font-semibold">Request details</SheetTitle>
          <SheetDescription className="text-xs">
            Context for the request, then approve, reject, or delegate.
          </SheetDescription>
        </SheetHeader>

        <div className="space-y-5 p-5">
          {detail.isLoading && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" /> Loading…
            </div>
          )}
          {ask && <AskBody wsId={wsId} ask={ask} onClose={onClose} />}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function AskBody({
  wsId,
  ask,
  onClose,
}: {
  wsId: string;
  ask: Approval;
  onClose: () => void;
}) {
  const audit = useQuery(approvalAuditOptions(wsId, ask.id));
  const approve = useApproveApproval();
  const reject = useRejectApproval();
  const delegate = useDelegateApproval();

  const [note, setNote] = useState("");
  const [delegateOpen, setDelegateOpen] = useState(false);
  const [toType, setToType] = useState<DelegateTargetType>("member");
  const [toId, setToId] = useState("");
  // Approve controls: duration (grant_for_seconds) and optional level escalation.
  const [grantSeconds, setGrantSeconds] = useState(0);
  const [grantLevel, setGrantLevel] = useState<"" | GrantLevel>("");

  const ctx = readAskContext(ask.context);
  const argsPreview = compactArgs(ctx.args);

  const isPending = ask.status === "pending";
  const busy = approve.isPending || reject.isPending || delegate.isPending;

  const handleConflict = (err: unknown) => {
    const msg = err instanceof Error ? err.message : "";
    if (msg.includes("409") || /already decided/i.test(msg)) {
      toast.error("Another approver got to it first");
    } else {
      toast.error("Action failed");
    }
  };

  return (
    <>
      <Field label="Status">
        <StatusBadge status={ask.status} />
      </Field>
      <Field label="Capability">
        <code className="text-xs">{ask.capability || "—"}</code>
      </Field>
      <Field label="Resource">
        <code className="text-xs">{ask.resource || "*"}</code>
      </Field>
      {ctx.purpose && <Field label="Purpose">{ctx.purpose}</Field>}
      {ctx.connection && (
        <Field label="Connection">
          <code className="text-xs">{ctx.connection}</code>
        </Field>
      )}
      {ctx.task_id && (
        <Field label="Task">
          <code className="text-xs">{ctx.task_id}</code>
        </Field>
      )}
      {argsPreview && (
        <Field label="Arguments">
          <code className="block whitespace-pre-wrap break-all text-xs">
            {argsPreview}
          </code>
        </Field>
      )}
      <Field label="Requester">
        {ask.requester_type} · {ask.requester_id || "—"}
        {ask.agent_id ? ` (agent ${ask.agent_id})` : ""}
      </Field>
      {ask.reason && <Field label="Reason">{ask.reason}</Field>}
      {ask.matched_grant_ids.length > 0 && (
        <Field label="Matched grants">
          {ask.matched_grant_ids.join(", ")}
        </Field>
      )}
      {ask.expires_at && <Field label="Expires">{ask.expires_at}</Field>}
      {ask.decided_by_id && (
        <Field label="Decided by">
          {ask.decided_by_id}
          {ask.decision_note ? ` — “${ask.decision_note}”` : ""}
        </Field>
      )}
      {ask.delegated_to_id && (
        <Field label="Delegated to">
          {ask.delegated_to_type} · {ask.delegated_to_id}
        </Field>
      )}

      {isPending && (
        <div className="space-y-3 rounded-md border p-4">
          <Textarea
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="Note (optional) — shown in audit"
            rows={2}
          />
          <div className="grid grid-cols-2 gap-2">
            <label className="space-y-1">
              <span className="text-xs font-medium text-muted-foreground">
                Approve for
              </span>
              <Select
                value={String(grantSeconds)}
                onValueChange={(v) => setGrantSeconds(Number(v))}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {GRANT_DURATIONS.map((d) => (
                    <SelectItem key={d.seconds} value={String(d.seconds)}>
                      {d.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>
            <label className="space-y-1">
              <span className="text-xs font-medium text-muted-foreground">
                Also allow at level
              </span>
              <Select
                value={grantLevel}
                onValueChange={(v) => setGrantLevel(v as "" | GrantLevel)}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {GRANT_LEVELS.map((l) => (
                    <SelectItem key={l.value || "none"} value={l.value}>
                      {l.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              disabled={busy}
              onClick={() =>
                approve.mutate(
                  {
                    id: ask.id,
                    note,
                    grant_for_seconds: grantSeconds > 0 ? grantSeconds : undefined,
                    grant_at_level: grantLevel || undefined,
                  },
                  {
                    onSuccess: () => {
                      toast.success("Approved");
                      onClose();
                    },
                    onError: handleConflict,
                  },
                )
              }
            >
              <Check className="size-4" /> Approve
            </Button>
            <Button
              size="sm"
              variant="destructive"
              disabled={busy}
              onClick={() =>
                reject.mutate(
                  { id: ask.id, note },
                  {
                    onSuccess: () => {
                      toast.success("Rejected");
                      onClose();
                    },
                    onError: handleConflict,
                  },
                )
              }
            >
              <X className="size-4" /> Reject
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={busy}
              onClick={() => setDelegateOpen((v) => !v)}
            >
              <Share2 className="size-4" /> Delegate
            </Button>
          </div>

          {delegateOpen && (
            <div className="space-y-2 border-t pt-3">
              <div className="flex gap-2">
                <Select
                  value={toType}
                  onValueChange={(v) => setToType(v as DelegateTargetType)}
                >
                  <SelectTrigger className="w-32">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="member">Member</SelectItem>
                    <SelectItem value="group">Group</SelectItem>
                  </SelectContent>
                </Select>
                <Input
                  value={toId}
                  onChange={(e) => setToId(e.target.value)}
                  placeholder={`${toType} id (UUID)`}
                  className="flex-1"
                />
              </div>
              <Button
                size="sm"
                variant="secondary"
                disabled={busy || !toId}
                onClick={() =>
                  delegate.mutate(
                    { id: ask.id, body: { to_type: toType, to_id: toId, note } },
                    {
                      onSuccess: () => {
                        toast.success("Delegated");
                        onClose();
                      },
                      onError: handleConflict,
                    },
                  )
                }
              >
                Confirm delegation
              </Button>
            </div>
          )}
        </div>
      )}

      <div>
        <h4 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          History
        </h4>
        <ul className="space-y-1.5 text-xs text-muted-foreground">
          {(audit.data?.items ?? []).map((e) => (
            <li key={e.id} className="flex justify-between gap-2">
              <span>
                {e.action}
                {e.actor_name ? ` · ${e.actor_name}` : ""}
                {e.note ? ` — ${e.note}` : ""}
              </span>
              <span className="shrink-0">{formatWhen(e.created_at)}</span>
            </li>
          ))}
        </ul>
      </div>
    </>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[120px_1fr] items-start gap-2 text-sm">
      <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span className="min-w-0 break-words">{children}</span>
    </div>
  );
}

function formatWhen(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleString();
}
