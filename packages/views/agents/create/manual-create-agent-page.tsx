"use client";

import { useEffect, useState } from "react";
import { Loader2, Sparkles } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import {
  agentBuilderSessionKeys,
  toStoredAgentDraft,
} from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import type { RuntimeDevice } from "@multica/core/types";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useBackOrReplace, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { splitRuntimeName } from "../../runtimes/components/runtime-machines";
import { AgentConfigurationPanel } from "./agent-configuration-panel";
import { CreateAgentFooter } from "./create-agent-footer";
import { AgentCreateChip, AgentCreateShell } from "./create-shell";
import { createPathWithParams } from "./squad-param";
import { useCreateAgentForm } from "./use-create-agent-form";
import { useCreateAgentSubmit } from "./use-create-agent-submit";
import { useDuplicateDraftSeed } from "./use-duplicate-draft-seed";
import {
  clearManualDraftForOwner,
  useManualDraftSync,
} from "./use-manual-draft-sync";

/**
 * Step 2 of agent creation: configure an agent on the runtime chosen in step 1
 * (`?runtime=`).
 *
 * `?duplicate=<id>` seeds the same form from an existing agent instead of
 * starting empty — the two share one route because after seeding they are the
 * same screen with the same submit path. A duplicate skips step 1 and keeps the
 * inline runtime picker, since its runtime comes from the source agent.
 */
export function ManualCreateAgentPage() {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const backOrReplace = useBackOrReplace();
  const duplicateId = navigation.searchParams.get("duplicate");
  const squadId = navigation.searchParams.get("squad");
  const runtimeParam = duplicateId ? null : navigation.searchParams.get("runtime");
  // On desktop the location is the tab's URL, and it moves to the done page
  // while this route is still mounted. Its missing `?runtime=` is not a
  // skipped step 1; redirecting on it threw the user back there.
  const onThisRoute = navigation.pathname === paths.newAgentManual();
  const needsRuntime = onThisRoute && !duplicateId && !runtimeParam;

  useEffect(() => {
    if (needsRuntime) {
      navigation.replace(createPathWithParams(paths.newAgent(), { squad: squadId }));
    }
  }, [needsRuntime, navigation, paths, squadId]);

  const form = useCreateAgentForm({
    runtimeSeed: { ready: !needsRuntime, runtimeId: runtimeParam ?? "" },
  });
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const duplicateAgent = duplicateId
    ? agents.find((agent) => agent.id === duplicateId) ?? null
    : null;

  // True when a duplicate had to fall back to another runtime, which drops the
  // source's model / thinking / speed. The notice explains the empty fields.
  const [duplicateRuntimeReset, setDuplicateRuntimeReset] = useState(false);
  // A duplicate seeds asynchronously (it waits for the runtime query). Saving
  // before that lands would persist the empty form the seed is about to
  // replace, and restoring before it would be undone a tick later.
  const [duplicateSeeded, setDuplicateSeeded] = useState(false);

  useDuplicateDraftSeed({
    source: duplicateAgent,
    // `buildDuplicateDraft` decides whether the copy can stay on the source
    // runtime by looking it up in this list. An error is a decidable answer
    // (the runtime cannot be confirmed, so the fallback is right); a pending
    // query is not.
    runtimesSettled: form.runtimesSettled,
    runtimes: form.runtimes,
    currentUserId: form.currentUserId,
    fallbackRuntimeId: form.usableRuntimes[0]?.id ?? "",
    nameSuffix: t(($) => $.create_dialog.duplicate_copy_suffix),
    onSeed: (duplicated, runtimeReset) => {
      // Only the forced fallback gets the notice; a later manual runtime switch
      // is the user's own doing and needs no explanation.
      setDuplicateRuntimeReset(runtimeReset);
      form.setDraft(duplicated);
      setDuplicateSeeded(true);
    },
  });

  // What #6287 reported: the desktop shell mounts only the active tab, so
  // coming back from another tab is a remount and everything typed was gone.
  useManualDraftSync({
    duplicateId,
    draft: form.draft,
    setDraft: form.setDraft,
    ready: !duplicateId || duplicateSeeded,
    runtimeId: runtimeParam,
  });

  const submit = useCreateAgentSubmit({
    draft: form.draft,
    runtimeId: form.selectedRuntime?.id ?? null,
    runtimeProvider: form.selectedRuntime?.provider,
    squadId,
    duplicateSource: duplicateAgent,
    // The work is committed; leaving it stored would hand the finished agent's
    // fields to whoever opens this form next. Only this flow's slot — another
    // half-finished copy is still someone's unfinished work.
    onCreated: () => clearManualDraftForOwner(duplicateId),
  });

  const changeRuntime = () =>
    navigation.replace(
      createPathWithParams(paths.newAgent(), {
        squad: squadId,
        runtime: form.draft.runtimeId || runtimeParam,
      }),
    );

  // Hands the current draft to a builder conversation on the same runtime.
  const [startingAi, setStartingAi] = useState(false);
  const [aiError, setAiError] = useState<string | null>(null);
  const runtime = form.selectedRuntime;
  const canAskAi =
    !!runtime && runtime.runtime_mode !== "cloud" && runtime.status === "online";
  const askAi = async () => {
    if (!runtime) return;
    setStartingAi(true);
    setAiError(null);
    try {
      const session = await api.createAgentBuilderSession({
        runtime_id: runtime.id,
        model: form.draft.model.trim() || undefined,
      });
      if (!session.session_id) {
        throw new Error(t(($) => $.creation_studio.builder.start_failed));
      }
      await api.saveAgentBuilderDraft(
        session.session_id,
        toStoredAgentDraft(form.draft, null, null),
      );
      void qc.invalidateQueries({ queryKey: agentBuilderSessionKeys.list(wsId) });
      navigation.replace(
        createPathWithParams(paths.newAgentAiSession(session.session_id), {
          squad: squadId,
          runtime: runtime.id,
        }),
      );
    } catch (err) {
      setAiError(
        err instanceof Error
          ? err.message
          : t(($) => $.creation_studio.builder.start_failed),
      );
      setStartingAi(false);
    }
  };

  const canCreate =
    form.draft.name.trim().length > 0 && form.draftReady && !submit.creating;

  if (needsRuntime) return null;

  return (
    <AgentCreateShell
      title={
        duplicateAgent
          ? t(($) => $.creation_studio.duplicate_title, {
              name: duplicateAgent.name,
            })
          : squadId
            ? t(($) => $.creation_studio.squad_title)
            : t(($) => $.creation_studio.title)
      }
      step={t(($) => $.creation_studio.steps.configure)}
      stepIndex={duplicateId ? undefined : 2}
      // A duplicate arrives from the agents list, not from step 1, so it
      // returns to where it came from instead of offering a runtime to pick.
      onBack={() =>
        duplicateId ? backOrReplace(paths.agents()) : changeRuntime()
      }
      chips={
        duplicateId && form.selectedRuntime ? (
          <AgentCreateChip>
            {runtimeDisplayLabel(form.selectedRuntime)}
          </AgentCreateChip>
        ) : undefined
      }
    >
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-4xl px-5 py-8 sm:px-8">
          {duplicateAgent && (
            <div className="mb-5 rounded-lg border border-warning/30 bg-warning/5 px-4 py-3 text-body">
              {t(($) => $.creation_studio.duplicate_env_notice)}
            </div>
          )}
          {duplicateRuntimeReset && (
            <div className="mb-5 rounded-lg border border-warning/30 bg-warning/5 px-4 py-3 text-body">
              {t(($) => $.creation_studio.duplicate_runtime_reset_notice)}
            </div>
          )}
          {!duplicateId ? (
            <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
              <div className="min-w-0">
                <h2 className="text-title font-semibold tracking-tight">
                  {t(($) => $.creation_studio.configure_title)}
                </h2>
                <p className="mt-1 text-body text-muted-foreground">
                  {t(($) => $.creation_studio.configure_description)}
                </p>
              </div>
              {canAskAi ? (
                <div className="flex flex-col items-end gap-1">
                  <Button
                    variant="outline"
                    disabled={startingAi}
                    onClick={() => void askAi()}
                  >
                    {startingAi ? (
                      <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
                    ) : (
                      <Sparkles aria-hidden="true" className="size-3.5" />
                    )}
                    {t(($) => $.creation_studio.ask_ai)}
                  </Button>
                  {aiError ? (
                    <p role="alert" className="text-caption text-destructive">
                      {aiError}
                    </p>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : null}
          <AgentConfigurationPanel
            draft={form.draft}
            onChange={form.setDraft}
            runtimes={form.runtimes}
            runtimesLoading={form.runtimesLoading}
            members={form.members}
            currentUserId={form.currentUserId}
            nameError={submit.nameError}
            onNameChange={(name) => {
              submit.clearNameError();
              form.setDraft((current) => ({ ...current, name }));
            }}
            runtimeSummary={
              duplicateId ? undefined : (
                <RuntimeSummaryCard
                  runtime={runtime}
                  loading={form.runtimesLoading}
                  onChange={changeRuntime}
                />
              )
            }
          />
        </div>
        <CreateAgentFooter
          canCreate={canCreate}
          creating={submit.creating}
          squad={!!squadId}
          error={submit.formError}
          onCreate={() => void submit.create()}
        />
      </div>
    </AgentCreateShell>
  );
}

function RuntimeSummaryCard({
  runtime,
  loading,
  onChange,
}: {
  runtime: RuntimeDevice | null;
  loading: boolean;
  onChange: () => void;
}) {
  const { t } = useT("agents");
  const online = runtime?.status === "online";
  const where = !runtime
    ? ""
    : runtime.runtime_mode === "cloud"
      ? t(($) => $.creation_studio.runtime_step.groups.cloud)
      : splitRuntimeName(runtime.name).hostname ?? runtime.device_info;
  return (
    <div>
      <p className="mb-1.5 text-label font-medium">
        {t(($) => $.creation_studio.runtime_label)}
      </p>
      <div className="flex min-h-10 items-center gap-2.5 rounded-lg border bg-muted/30 px-3 py-2">
        {runtime ? (
          <>
            <ProviderLogo provider={runtime.provider} className="size-4 shrink-0" />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-label font-medium">
                {runtimeDisplayLabel(runtime)}
              </span>
              <span className="flex items-center gap-1.5 truncate text-caption text-muted-foreground">
                <span
                  aria-hidden="true"
                  className={cn(
                    "size-1.5 shrink-0 rounded-full",
                    online ? "bg-success" : "bg-muted-foreground/40",
                  )}
                />
                {where}
              </span>
            </span>
          </>
        ) : (
          <span className="flex-1 text-label text-muted-foreground">
            {loading
              ? t(($) => $.creation_studio.runtime_loading)
              : t(($) => $.creation_studio.runtime_missing)}
          </span>
        )}
        <Button variant="ghost" size="sm" onClick={onChange}>
          {t(($) => $.creation_studio.change_runtime)}
        </Button>
      </div>
    </div>
  );
}
