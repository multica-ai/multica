"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useBackOrReplace, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { AgentConfigurationPanel } from "./agent-configuration-panel";
import { CreateAgentFooter } from "./create-agent-footer";
import { AgentCreateChip, AgentCreateShell } from "./create-shell";
import { useCreateAgentForm } from "./use-create-agent-form";
import { useCreateAgentSubmit } from "./use-create-agent-submit";
import { useDuplicateDraftSeed } from "./use-duplicate-draft-seed";
import {
  clearManualDraftForOwner,
  useManualDraftSync,
} from "./use-manual-draft-sync";

/**
 * Manual agent creation: every field filled in by hand.
 *
 * `?duplicate=<id>` seeds the same form from an existing agent instead of
 * starting empty — the two share one route because after seeding they are the
 * same screen with the same submit path.
 */
export function ManualCreateAgentPage() {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const backOrReplace = useBackOrReplace();
  const duplicateId = navigation.searchParams.get("duplicate");
  const squadId = navigation.searchParams.get("squad");

  const form = useCreateAgentForm();
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
  });

  const submit = useCreateAgentSubmit({
    draft: form.draft,
    runtimeId: form.selectedRuntime?.id ?? null,
    squadId,
    duplicateSource: duplicateAgent,
    // The work is committed; leaving it stored would hand the finished agent's
    // fields to whoever opens this form next. Only this flow's slot — another
    // half-finished copy is still someone's unfinished work.
    onCreated: () => clearManualDraftForOwner(duplicateId),
  });

  const canCreate =
    form.draft.name.trim().length > 0 && form.draftReady && !submit.creating;

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
      step={t(($) => $.creation_studio.step_configure)}
      // A duplicate arrives from the agents list, not from the chooser, so it
      // returns to where it came from instead of offering a method to pick.
      onBack={() =>
        backOrReplace(duplicateId ? paths.agents() : paths.newAgent())
      }
      chips={
        <>
          <AgentCreateChip>
            {t(($) => $.creation_studio.modes.blank.title)}
          </AgentCreateChip>
          {form.selectedRuntime && (
            <AgentCreateChip>
              {runtimeDisplayLabel(form.selectedRuntime)}
            </AgentCreateChip>
          )}
        </>
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
