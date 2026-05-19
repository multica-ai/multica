"use client";

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";

import { createAgentPass } from "../../core/api";
import { agentPassesKeys } from "../../core/queries";
import type { CreateAgentPassRequest } from "../../core/types";

interface CreateAgentPassDialogProps {
  wsId: string;
  onClose: () => void;
}

// Form for POST /api/cerebro/agent-passes. Surfaces scope-widening
// errors (HTTP 422) inline so the operator can immediately see WHICH key
// of the scope was widened against the parent. Other backend errors
// (400/404/409/500) bubble up as a generic alert above the form.
export function CreateAgentPassDialog({
  wsId,
  onClose,
}: CreateAgentPassDialogProps) {
  const queryClient = useQueryClient();
  const [agentId, setAgentId] = useState("");
  const [issueId, setIssueId] = useState("");
  const [parentPassId, setParentPassId] = useState("");
  const [scopeText, setScopeText] = useState("{}");
  const [spendCeilingUSD, setSpendCeilingUSD] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [scopeParseError, setScopeParseError] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: createAgentPass,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: agentPassesKeys.all(wsId) });
      onClose();
    },
    onError: (err: unknown) => {
      setSubmitError(extractErrorMessage(err));
    },
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitError(null);
    setScopeParseError(null);

    let parsedScope: Record<string, unknown> = {};
    const trimmed = scopeText.trim();
    if (trimmed !== "" && trimmed !== "{}") {
      try {
        parsedScope = JSON.parse(trimmed);
        if (typeof parsedScope !== "object" || parsedScope === null || Array.isArray(parsedScope)) {
          throw new Error("scope skal være et JSON-objekt");
        }
      } catch (err) {
        setScopeParseError(
          err instanceof Error ? err.message : "kunne ikke parse scope JSON",
        );
        return;
      }
    }

    const ceilingMicros = parseUSDToMicros(spendCeilingUSD);
    const body: CreateAgentPassRequest = {
      agent_id: agentId.trim(),
      issue_id: issueId.trim(),
      scope: parsedScope,
    };
    if (parentPassId.trim()) body.parent_pass_id = parentPassId.trim();
    if (ceilingMicros !== null) body.spend_ceiling_micros = ceilingMicros;
    if (expiresAt.trim()) body.expires_at = toRFC3339(expiresAt.trim());

    mutation.mutate(body);
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Udsted agent pass</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex flex-col gap-3">
          {submitError && (
            <div className="rounded border border-destructive/40 bg-destructive/5 px-3 py-2 text-sm text-destructive">
              {submitError}
            </div>
          )}
          <div className="grid gap-1.5">
            <Label htmlFor="agent-id">Agent ID</Label>
            <Input
              id="agent-id"
              value={agentId}
              onChange={(e) => setAgentId(e.target.value)}
              placeholder="uuid"
              required
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="issue-id">Issue ID</Label>
            <Input
              id="issue-id"
              value={issueId}
              onChange={(e) => setIssueId(e.target.value)}
              placeholder="uuid"
              required
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="parent-pass-id">
              Parent pass ID{" "}
              <span className="text-xs text-muted-foreground">
                (valgfri — kun ved downscoping)
              </span>
            </Label>
            <Input
              id="parent-pass-id"
              value={parentPassId}
              onChange={(e) => setParentPassId(e.target.value)}
              placeholder="uuid"
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="scope">
              Scope (JSON){" "}
              <span className="text-xs text-muted-foreground">
                fx <code>{`{"allowed_tools":["read"]}`}</code>
              </span>
            </Label>
            <Textarea
              id="scope"
              value={scopeText}
              onChange={(e) => setScopeText(e.target.value)}
              rows={4}
              className="font-mono text-xs"
            />
            {scopeParseError && (
              <p className="text-xs text-destructive">{scopeParseError}</p>
            )}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="grid gap-1.5">
              <Label htmlFor="spend-ceiling">Budget (USD)</Label>
              <Input
                id="spend-ceiling"
                type="number"
                step="0.01"
                min="0"
                value={spendCeilingUSD}
                onChange={(e) => setSpendCeilingUSD(e.target.value)}
                placeholder="—"
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="expires-at">Udløber</Label>
              <Input
                id="expires-at"
                type="datetime-local"
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
              />
            </div>
          </div>
          <DialogFooter className="mt-2">
            <Button
              type="button"
              variant="outline"
              onClick={onClose}
              disabled={mutation.isPending}
            >
              Annuller
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? "Udsteder…" : "Udsted"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function extractErrorMessage(err: unknown): string {
  if (!err) return "Ukendt fejl";
  if (err instanceof Error) {
    if (err.message.includes("scope_widening")) {
      return "Scope-fejl: barn-pas må ikke udvide forælder-passets scope. Indsnævr scope og prøv igen.";
    }
    if (err.message.includes("409")) {
      return "Der findes allerede et aktivt pas for denne agent + issue. Tilbagekald det først.";
    }
    return err.message;
  }
  return String(err);
}

function parseUSDToMicros(raw: string): number | null {
  const trimmed = raw.trim();
  if (trimmed === "") return null;
  const n = Number(trimmed);
  if (!Number.isFinite(n) || n < 0) return null;
  return Math.round(n * 1_000_000);
}

function toRFC3339(local: string): string {
  // The datetime-local input gives "YYYY-MM-DDTHH:mm" without a timezone.
  // Treat it as the user's local time and convert to an ISO/RFC3339 UTC
  // string so the backend pgtype.Timestamptz parser accepts it.
  const d = new Date(local);
  if (Number.isNaN(d.getTime())) return local;
  return d.toISOString();
}
