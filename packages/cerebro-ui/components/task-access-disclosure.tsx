"use client";

import { useCallback, useEffect, useState } from "react";
import { ChevronDown, LockKeyhole, RefreshCw } from "lucide-react";
import { api } from "@multica/core/api";
import type { AccessDiagnostic, TaskAccessSnapshot } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import { AccessDiagnostics } from "./access-diagnostics";

type TaskAccessLoader = (taskId: string) => Promise<TaskAccessSnapshot>;

const defaultTaskAccessLoader: TaskAccessLoader = (taskId) => api.getTaskAccess(taskId);

export function TaskAccessDisclosure({
  taskId,
  load = defaultTaskAccessLoader,
}: {
  taskId: string;
  load?: TaskAccessLoader;
}) {
  const [snapshot, setSnapshot] = useState<TaskAccessSnapshot | null>(null);
  const [errorDiagnostics, setErrorDiagnostics] = useState<AccessDiagnostic[] | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    setLoading(true);
    setErrorDiagnostics(null);
    try {
      setSnapshot(await load(taskId));
    } catch (cause) {
      setSnapshot(null);
      setErrorDiagnostics(diagnosticsFromError(cause, taskId));
    } finally {
      setLoading(false);
    }
  }, [load, taskId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  if (loading && !snapshot) {
    return (
      <div className="rounded-md border bg-muted/20 p-3 text-xs text-muted-foreground" role="status">
        Loading Task access…
      </div>
    );
  }

  if (errorDiagnostics || !snapshot) {
    const diagnostics: AccessDiagnostic[] = errorDiagnostics ?? [{
      code: "task_access_unavailable",
      state: "unavailable",
      title: "Task access unavailable",
      message: "The snapshot did not return data.",
      affected_capability: `task:${taskId}`,
      source_policy: "Task Mandate API",
      recovery_action: "Retry the task claim and investigate Task Mandate persistence before enabling enforcement.",
    }];
    return (
      <div className="rounded-md border border-destructive/40 bg-card p-3">
        <div className="space-y-3">
          <AccessDiagnostics diagnostics={diagnostics} />
          <Button size="sm" variant="outline" className="h-7 gap-1.5 text-xs" onClick={() => void refresh()}>
            <RefreshCw className="size-3" /> Retry
          </Button>
        </div>
      </div>
    );
  }

  const diagnostics = taskDiagnostics(snapshot);
  const isActive = snapshot.status === "active";
  return (
    <Collapsible defaultOpen className="rounded-md border bg-muted/20 text-xs">
      <CollapsibleTrigger className="flex w-full items-center justify-between gap-3 p-3 text-left hover:bg-muted/40">
        <span className="flex min-w-0 items-center gap-2">
          <LockKeyhole className="size-3.5 shrink-0 text-muted-foreground" />
          <span>
            <span className="font-medium">Task access</span>
            <span className="ml-2 text-muted-foreground">
              {snapshot.allowed_tools.length} capabilities · {isActive ? "active" : "ended"}
            </span>
          </span>
        </span>
        <span className="flex shrink-0 items-center gap-2">
          <Badge variant={snapshot.enforcement_enabled ? "default" : "secondary"} className="text-[10px]">
            {snapshot.enforcement_enabled ? "Enforced" : "Observation only"}
          </Badge>
          <ChevronDown className="size-3.5 text-muted-foreground" />
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="space-y-3 border-t p-3">
          <p className="leading-relaxed text-muted-foreground">
            Settings → Permissions is live: a later Deny or safety ceiling can still tighten this run, but a later Allow cannot widen its frozen Task Mandate. Start a new task to capture newly allowed access.
          </p>

          <div className="flex flex-wrap gap-1.5 text-[10px] text-muted-foreground">
            <span className="rounded border bg-background px-1.5 py-0.5">
              Generation {snapshot.claim_generation ?? 0}
            </span>
            <span className="rounded border bg-background px-1.5 py-0.5">
              {snapshot.lifecycle_state ?? "legacy"}
            </span>
            <span className="rounded border bg-background px-1.5 py-0.5">
              {snapshot.offered_count ?? snapshot.allowed_tools.length} offered
            </span>
            <span className="rounded border bg-background px-1.5 py-0.5">
              {snapshot.authorized_count ?? snapshot.allowed_tools.length} authorized
            </span>
          </div>

          <AccessDiagnostics diagnostics={diagnostics} />

          <div className="flex max-h-28 flex-wrap gap-1.5 overflow-y-auto">
            {snapshot.allowed_tools.length === 0 ? (
              <span className="rounded-md border border-dashed p-2 text-muted-foreground">
                No capabilities were frozen for this run.
              </span>
            ) : snapshot.allowed_tools.map((tool) => (
              <span key={tool} className="rounded border bg-background px-1.5 py-0.5 font-mono text-[10px]">
                {tool}
              </span>
            ))}
          </div>

          <p className="text-[10px] text-muted-foreground">
            Issued {formatTimestamp(snapshot.issued_at)} · expires {formatTimestamp(snapshot.expires_at)}
          </p>
          {(snapshot.producer || snapshot.finalizer || snapshot.inventory_version || snapshot.discovery_version) ? (
            <p className="break-all text-[10px] text-muted-foreground">
              Producer {snapshot.producer ?? "—"} · finalizer {snapshot.finalizer ?? "—"} · inventory {snapshot.inventory_version ?? "—"} · discovery {snapshot.discovery_version ?? "—"}
            </p>
          ) : null}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

function taskDiagnostics(snapshot: TaskAccessSnapshot): AccessDiagnostic[] {
  if (snapshot.diagnostics && snapshot.diagnostics.length > 0) return snapshot.diagnostics;
  const diagnostics: AccessDiagnostic[] = [];
  if (!snapshot.enforcement_enabled) {
    diagnostics.push({
      code: "task_observation_only",
      state: "info",
      title: "Observation only",
      message: "Task Mandate enforcement is off. This snapshot cannot reject calls.",
      affected_capability: "task:*",
      source_policy: "Task Mandate",
      recovery_action: "Keep rollout off until provider, discovery and policy parity are clean.",
    });
  }
  if (snapshot.status === "expired") {
    diagnostics.push({
      code: "task_expired",
      state: "stale",
      title: "Task access expired",
      message: "This historical Task Mandate is outside its active window.",
      affected_capability: "task:*",
      source_policy: "Task Mandate",
      recovery_action: "Start a new task to receive a current Task Mandate.",
    });
  }
  const offered = snapshot.offered_count ?? snapshot.allowed_tools.length;
  const authorized = snapshot.authorized_count ?? snapshot.allowed_tools.length;
  if (offered > authorized) {
    diagnostics.push({
      code: "task_partial",
      state: "partial",
      title: "Partial task access",
      message: `${offered - authorized} offered capabilities were removed before the Task Mandate was finalized.`,
      affected_capability: `${offered - authorized} capabilities`,
      source_policy: "Task Mandate",
      recovery_action: "Review Settings → Permissions and provider discovery, then start a new task after changing access.",
    });
  }
  return diagnostics;
}

function diagnosticsFromError(cause: unknown, taskId: string): AccessDiagnostic[] {
  if (cause && typeof cause === "object" && "body" in cause) {
    const body = cause.body;
    if (body && typeof body === "object" && "diagnostics" in body && Array.isArray(body.diagnostics)) {
      const diagnostics = body.diagnostics.filter(isAccessDiagnostic);
      if (diagnostics.length > 0) return diagnostics;
    }
  }
  return [{
    code: "task_access_unavailable",
    state: "unavailable",
    title: "Task access unavailable",
    message: cause instanceof Error ? cause.message : "Unknown error",
    affected_capability: `task:${taskId}`,
    source_policy: "Task Mandate API",
    recovery_action: "Retry the task claim and investigate Task Mandate persistence before enabling enforcement.",
  }];
}

function isAccessDiagnostic(value: unknown): value is AccessDiagnostic {
  if (!value || typeof value !== "object") return false;
  const diagnostic = value as Partial<AccessDiagnostic>;
  return typeof diagnostic.code === "string"
    && typeof diagnostic.state === "string"
    && typeof diagnostic.title === "string"
    && typeof diagnostic.message === "string"
    && typeof diagnostic.source_policy === "string"
    && typeof diagnostic.recovery_action === "string";
}

function formatTimestamp(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}
