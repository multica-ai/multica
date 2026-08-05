// FIR-4492 — the runtime's own cheap model.
//
// A caller can ask for the provider-independent "cheap" tier instead of naming a
// model ID (`--model cheap`), and the runtime that ends up running the task
// decides which ID that is. Multica curates that ID for the four providers whose
// model list is identical on every machine: claude, codex, openai-eu,
// firtal-local. For every other provider — hermes, opencode, cursor, kimi, kiro,
// pi, openclaw, antigravity, firtal-gateway — the list is a file on the machine,
// so there was nothing to resolve to and "cheap" quietly became no override at
// all: the wakeup asked for a cheap run and got the agent's own model.
//
// The server cannot fix that by guessing. Even the model list the machine reports
// carries no price, so "cheapest" is not computable from it — it has to be chosen.
// This card is where it gets chosen, from the machine's real list rather than from
// anything typed by hand.
//
// Getting it wrong is not dangerous: the daemon checks every model against the
// runtime's live list just before spawning and degrades to the agent's own model
// (daemon.runnableTaskModel), so a stale value costs the cheap run, not the run.

"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import type { AgentRuntime } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";

interface CheapModelCardProps {
  runtime: AgentRuntime;
  wsId: string;
  canEdit: boolean;
}

export function CheapModelCard({
  runtime,
  wsId,
  canEdit,
}: CheapModelCardProps) {
  const qc = useQueryClient();
  const curated = runtime.cheap_model_curated ?? "";
  const current = runtime.cheap_model ?? "";
  const [picking, setPicking] = useState(false);
  const [selected, setSelected] = useState(current);

  // Discovery is a round trip to the machine, so it only runs once the operator
  // asks to change the value — not on every visit to the runtime page.
  const modelsQuery = useQuery({
    ...runtimeModelsOptions(picking ? runtime.id : null),
    retry: false,
  });

  const mutation = useMutation({
    mutationFn: (model: string) =>
      api.updateRuntimeCheapModel(runtime.id, model),
    onSuccess: (_data, model) => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      setPicking(false);
      toast.success(
        model ? "Cheap model updated" : "Cheap model cleared",
        {
          description: model
            ? `A cheap run on ${runtime.name} now uses ${model}.`
            : `A cheap run on ${runtime.name} falls back to the agent's own model.`,
        },
      );
    },
    onError: (err) =>
      toast.error(
        err instanceof Error ? err.message : "Couldn't update the cheap model",
      ),
  });

  const heading = (
    <div className="flex items-center gap-2">
      <span className="text-sm font-medium">Cheap model</span>
      {curated ? <Badge variant="secondary">Managed</Badge> : null}
    </div>
  );

  // Multica curates the value for this provider, and that curated value is
  // asserted against the provider's catalog — offering a picker here would show
  // a setting that never takes effect.
  if (curated) {
    return (
      <div className="space-y-1.5">
        {heading}
        <p className="text-xs text-muted-foreground">
          A cheap run on this runtime uses{" "}
          <span className="font-mono">{curated}</span>. Multica picks it for{" "}
          {runtime.provider}, so there is nothing to set here.
        </p>
      </div>
    );
  }

  const models = modelsQuery.data?.models ?? [];

  return (
    <div className="space-y-1.5">
      {heading}
      <p className="text-xs text-muted-foreground">
        {current ? (
          <>
            A cheap run on this runtime uses{" "}
            <span className="font-mono">{current}</span>.
          </>
        ) : (
          <>
            Not set — a cheap run falls back to the agent's own model, which is
            the expensive one it was trying to avoid. Multica has no model list
            for {runtime.provider}: it lives on the machine, so pick from what
            the machine reports.
          </>
        )}
      </p>

      {canEdit && !picking && (
        <Button
          variant="outline"
          size="sm"
          className="h-7"
          onClick={() => {
            setSelected(current);
            setPicking(true);
          }}
        >
          {current ? "Change" : "Set a cheap model"}
        </Button>
      )}

      {canEdit && picking && (
        <div className="space-y-2 pt-1">
          {modelsQuery.isPending && (
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              Asking {runtime.name} which models it has…
            </div>
          )}

          {modelsQuery.isError && (
            <p className="text-xs text-destructive">
              {modelsQuery.error instanceof Error
                ? modelsQuery.error.message
                : "Couldn't read this runtime's models"}
              . The runtime has to be online to answer.
            </p>
          )}

          {modelsQuery.isSuccess && models.length === 0 && (
            <p className="text-xs text-destructive">
              This runtime reported no models, so there is nothing to pick.
            </p>
          )}

          {modelsQuery.isSuccess && models.length > 0 && (
            <NativeSelect
              size="sm"
              value={selected}
              onChange={(e) => setSelected(e.target.value)}
            >
              <NativeSelectOption value="">
                Not set — use the agent's own model
              </NativeSelectOption>
              {models.map((m) => (
                <NativeSelectOption key={m.id} value={m.id}>
                  {m.label ? `${m.label} (${m.id})` : m.id}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          )}

          <div className="flex gap-2">
            <Button
              size="sm"
              className="h-7"
              disabled={
                mutation.isPending ||
                !modelsQuery.isSuccess ||
                selected === current
              }
              onClick={() => mutation.mutate(selected)}
            >
              {mutation.isPending ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                "Save"
              )}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="h-7"
              disabled={mutation.isPending}
              onClick={() => setPicking(false)}
            >
              Cancel
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
