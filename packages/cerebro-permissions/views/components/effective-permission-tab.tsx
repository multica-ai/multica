"use client";

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { CheckCircle2, ShieldQuestion, XCircle } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  evaluateEffectivePermission,
  type EffectivePermissionResult,
} from "../../core";

export function EffectivePermissionTab({ wsId }: { wsId: string }) {
  const [actorType, setActorType] = useState("member");
  const [actorId, setActorId] = useState("");
  const [groupIds, setGroupIds] = useState("");
  const [roleIds, setRoleIds] = useState("");
  const [agentId, setAgentId] = useState("");
  const [capability, setCapability] = useState("issue.read");
  const [resource, setResource] = useState("*");

  const evaluate = useMutation({
    mutationFn: () =>
      evaluateEffectivePermission(wsId, {
        actor_type: actorType,
        actor_id: actorId.trim(),
        group_ids: splitIds(groupIds),
        role_ids: splitIds(roleIds),
        agent_id: agentId.trim() || null,
        capability: capability.trim(),
        resource: resource.trim(),
      }),
  });

  const canSubmit =
    actorId.trim().length > 0 &&
    capability.trim().length > 0 &&
    resource.trim().length > 0 &&
    !evaluate.isPending;

  return (
    <div className="flex flex-1 min-h-0 flex-col overflow-y-auto p-4">
      <form
        className="grid max-w-4xl gap-3 text-sm md:grid-cols-3"
        onSubmit={(event) => {
          event.preventDefault();
          if (canSubmit) evaluate.mutate();
        }}
      >
        <Field label="Actor-type">
          <Select value={actorType} onValueChange={setActorType}>
            <SelectTrigger size="sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="member">member</SelectItem>
              <SelectItem value="agent">agent</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        <Field label="Actor-ID">
          <Input value={actorId} onChange={(e) => setActorId(e.target.value)} />
        </Field>
        <Field label="Capability">
          <Input value={capability} onChange={(e) => setCapability(e.target.value)} />
        </Field>
        <Field label="Resource">
          <Input value={resource} onChange={(e) => setResource(e.target.value)} />
        </Field>
        <Field label="Group-IDs">
          <Input value={groupIds} onChange={(e) => setGroupIds(e.target.value)} />
        </Field>
        <Field label="Role-IDs">
          <Input value={roleIds} onChange={(e) => setRoleIds(e.target.value)} />
        </Field>
        <Field label="Agent-ID">
          <Input value={agentId} onChange={(e) => setAgentId(e.target.value)} />
        </Field>
        <div className="flex items-end">
          <Button type="submit" disabled={!canSubmit}>
            {evaluate.isPending ? "Evaluating..." : "Evaluate"}
          </Button>
        </div>
      </form>

      {evaluate.data && <DecisionResult result={evaluate.data} />}
      {evaluate.isError && (
        <div className="mt-4 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
          {evaluate.error instanceof Error ? evaluate.error.message : "Evaluation failed"}
        </div>
      )}
    </div>
  );
}

function DecisionResult({ result }: { result: EffectivePermissionResult }) {
  const Icon =
    result.decision === "allow"
      ? CheckCircle2
      : result.decision === "needs_approval"
        ? ShieldQuestion
        : XCircle;
  return (
    <div className="mt-4 max-w-4xl rounded-md border p-4">
      <div className="flex items-center gap-2">
        <Icon className="size-4 text-muted-foreground" />
        <div className="font-mono text-sm">{result.decision}</div>
        {result.winning_override_layer && (
          <div className="rounded-sm bg-muted px-1.5 py-0.5 text-[10px] uppercase text-muted-foreground">
            {result.winning_override_layer}
          </div>
        )}
      </div>
      <div className="mt-2 text-sm text-muted-foreground">{result.reason}</div>
      <div className="mt-3 grid gap-1 text-xs">
        <Row label="Matched grants" value={result.matched_grant_ids.join(", ") || "-"} />
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[120px_1fr] gap-2">
      <span className="text-muted-foreground">{label}</span>
      <span className="break-all font-mono">{value}</span>
    </div>
  );
}

function splitIds(value: string): string[] {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}
