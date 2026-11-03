"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useDecideInitiativeGate, type InitiativeGate } from "@multica/core/pipeline";
import type { GateOutcome } from "@multica/core/office";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";

// The outcomes an operate review offers, in the order the buttons show them.
const REVIEW_OUTCOMES: readonly GateOutcome[] = ["continue", "scale", "sunset"];

/**
 * The gate proposals of an initiative. A gate is a stage-gate proposal, so
 * Approve and Reject are the proposal decision. An operate review (30 and 90
 * days after launch) is approved with one of its three outcomes instead. A
 * rejection needs a note, which sends the initiative back with its leader woken.
 * Only an owner or admin decides.
 */
export function InitiativeGates({
  gates,
  canManage,
}: {
  gates: readonly InitiativeGate[];
  canManage: boolean;
}) {
  const { t } = useT("pipeline");
  return (
    <section aria-labelledby="initiative-gates" className="flex flex-col gap-2">
      <h3 id="initiative-gates" className="text-caption font-medium text-muted-foreground">
        {t(($) => $.panel.gates)}
      </h3>
      {gates.length === 0 ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.gate.none)}</p>
      ) : (
        <ul className="flex flex-col divide-y rounded-lg border">
          {gates.map((gate) => (
            <GateRow key={gate.id} gate={gate} canManage={canManage} />
          ))}
        </ul>
      )}
    </section>
  );
}

function GateRow({
  gate,
  canManage,
}: {
  gate: InitiativeGate;
  canManage: boolean;
}) {
  const { t } = useT("pipeline");
  const wsId = useWorkspaceId();
  const decide = useDecideInitiativeGate(wsId);
  const [rejecting, setRejecting] = useState(false);
  const [note, setNote] = useState("");
  const [noteMissing, setNoteMissing] = useState(false);
  const open = gate.state === "pending" && canManage;

  const isReview = gate.gate_day != null;

  const approve = (outcome?: GateOutcome) =>
    decide.mutate(
      { id: gate.id, decision: "approve", ...(outcome ? { outcome } : {}) },
      {
        onSuccess: () => toast.success(t(($) => $.toast.approved)),
        onError: () => toast.error(t(($) => $.toast.decide_failed)),
      },
    );

  const reject = () => {
    const trimmed = note.trim();
    if (!trimmed) {
      setNoteMissing(true);
      return;
    }
    decide.mutate(
      { id: gate.id, decision: "reject", note: trimmed },
      {
        onSuccess: () => {
          toast.success(t(($) => $.toast.rejected));
          setRejecting(false);
          setNote("");
        },
        onError: () => toast.error(t(($) => $.toast.decide_failed)),
      },
    );
  };

  return (
    <li className="flex flex-col gap-2 px-3 py-2.5">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="min-w-0 text-body">{gate.title || gate.issue_identifier}</span>
        <span className="shrink-0 text-caption text-muted-foreground">{t(($) => $.gate.state[gate.state])}</span>
      </div>
      {open ? (
        <div className="flex flex-col gap-2">
          {rejecting ? (
            <>
              <label className="flex flex-col gap-1 text-caption text-muted-foreground">
                {t(($) => $.gate.reject_note)}
                <Textarea
                  value={note}
                  rows={2}
                  aria-invalid={noteMissing || undefined}
                  onChange={(event) => {
                    setNote(event.target.value);
                    if (event.target.value.trim()) setNoteMissing(false);
                  }}
                  className="text-body text-foreground"
                />
              </label>
              {noteMissing ? (
                <p role="alert" className="text-caption text-destructive">
                  {t(($) => $.gate.reject_note_required)}
                </p>
              ) : null}
            </>
          ) : null}
          {isReview && !rejecting ? (
            <p className="text-caption text-muted-foreground">{t(($) => $.gate.outcome_hint)}</p>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            {rejecting ? (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  setRejecting(false);
                  setNote("");
                  setNoteMissing(false);
                }}
              >
                {t(($) => $.gate.cancel)}
              </Button>
            ) : (
              <Button type="button" variant="outline" size="sm" disabled={decide.isPending} onClick={() => setRejecting(true)}>
                {t(($) => $.gate.reject)}
              </Button>
            )}
            {rejecting ? (
              <Button type="button" variant="destructive" size="sm" disabled={decide.isPending} aria-busy={decide.isPending} onClick={reject}>
                {t(($) => $.gate.reject_submit)}
              </Button>
            ) : isReview ? (
              REVIEW_OUTCOMES.map((outcome) => (
                <Button
                  key={outcome}
                  type="button"
                  size="sm"
                  disabled={decide.isPending}
                  aria-busy={decide.isPending}
                  onClick={() => approve(outcome)}
                >
                  {t(($) => $.gate.outcome[outcome])}
                </Button>
              ))
            ) : (
              <Button type="button" size="sm" disabled={decide.isPending} aria-busy={decide.isPending} onClick={() => approve()}>
                {t(($) => $.gate.approve)}
              </Button>
            )}
          </div>
        </div>
      ) : null}
    </li>
  );
}
