"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useForceInitiative, type PipelineStage } from "@multica/core/pipeline";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";

/**
 * The admin override: move the initiative to any other stage, with a reason
 * the server records as a forced event. Rendered for owners and admins only.
 */
export function InitiativeForce({
  initiativeId,
  currentStage,
  stages,
}: {
  initiativeId: string;
  currentStage: string;
  stages: readonly PipelineStage[];
}) {
  const { t } = useT("pipeline");
  const wsId = useWorkspaceId();
  const force = useForceInitiative(wsId);
  const [toStage, setToStage] = useState("");
  const [reason, setReason] = useState("");
  const [reasonMissing, setReasonMissing] = useState(false);
  const targets = stages.filter((stage) => stage.key !== currentStage);
  const targetName = (key: string) => stages.find((stage) => stage.key === key)?.name || key;

  const submit = () => {
    const trimmed = reason.trim();
    if (!toStage) return;
    if (!trimmed) {
      setReasonMissing(true);
      return;
    }
    force.mutate(
      { id: initiativeId, to_stage: toStage, reason: trimmed },
      {
        onSuccess: () => {
          toast.success(t(($) => $.toast.forced, { stage: targetName(toStage) }));
          setToStage("");
          setReason("");
          setReasonMissing(false);
        },
        onError: () => toast.error(t(($) => $.toast.force_failed)),
      },
    );
  };

  return (
    <section aria-labelledby="initiative-force" className="flex flex-col gap-2 border-t pt-4">
      <h3 id="initiative-force" className="text-caption font-medium text-muted-foreground">
        {t(($) => $.panel.force)}
      </h3>
      <div className="flex flex-col gap-2">
        <Select
          items={targets.map((stage) => ({ value: stage.key, label: stage.name || stage.key }))}
          value={toStage}
          onValueChange={(value) => setToStage(value ?? "")}
        >
          <SelectTrigger aria-label={t(($) => $.force.to_stage)} className="w-full">
            <SelectValue>{toStage ? targetName(toStage) : t(($) => $.force.to_stage)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {targets.map((stage) => (
              <SelectItem key={stage.key} value={stage.key}>
                {stage.name || stage.key}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <label className="flex flex-col gap-1 text-caption text-muted-foreground">
          {t(($) => $.force.reason)}
          <Textarea
            value={reason}
            rows={2}
            aria-invalid={reasonMissing || undefined}
            onChange={(event) => {
              setReason(event.target.value);
              if (event.target.value.trim()) setReasonMissing(false);
            }}
            className="text-body text-foreground"
          />
        </label>
        {reasonMissing ? (
          <p role="alert" className="text-caption text-destructive">
            {t(($) => $.force.reason_required)}
          </p>
        ) : null}
        <div className="flex justify-end">
          <Button
            type="button"
            variant="outline"
            disabled={!toStage || force.isPending}
            aria-busy={force.isPending}
            onClick={submit}
          >
            {t(($) => $.force.submit)}
          </Button>
        </div>
      </div>
    </section>
  );
}
