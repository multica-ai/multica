"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronRight, Search } from "@multica/ui/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { agentBuilderSessionListOptions } from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { qoderOptions } from "@multica/core/runtimes/qoder";
import { runtimeKeys, runtimeListOptions } from "@multica/core/runtimes/queries";
import { useWSEvent } from "@multica/core/realtime";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useBackOrReplace, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { ConnectRemoteDialog } from "../../runtimes/components/connect-remote-dialog";
import { RemoteRuntimeDialog } from "../../runtimes/components/remote-runtime-dialog";
import { QODER_RUNTIME_LOCATOR } from "../../runtimes/components/qoder-detail-page";
import { buildRuntimeMachines } from "../../runtimes/components/runtime-machines";
import { CreateFlowFooter } from "./create-agent-footer";
import { AgentCreateShell } from "./create-shell";
import { buildRuntimeChoiceGroups, type HarnessOption } from "./runtime-choice";
import { RuntimeChoiceList, qoderChoiceState } from "./runtime-choice-list";
import { createPathWithParams } from "./squad-param";
import { UnfinishedDraftsBanner } from "./unfinished-drafts";

type Selection = { kind: "runtime"; runtimeId: string } | { kind: "qoder" } | null;

/** How often a starting Qoder runtime is re-fetched while this screen shows it. */
const QODER_REGISTER_POLL_MS = 3_000;

export interface ChooseRuntimePageProps {
  /** Desktop-only: the daemon running on this device. */
  localDaemonId?: string | null;
  localMachineName?: string | null;
  /** Desktop-only: this device can run harnesses, even before any registered. */
  hasLocalMachine?: boolean;
  /** The bundled daemon is starting and has not registered yet. */
  bootstrapping?: boolean;
}

/**
 * Step 1 of agent creation: where the agent runs. Everything after this step
 * depends on the answer (which models exist, whether skills apply), so the
 * runtime is chosen before anything is typed.
 */
export function ChooseRuntimePage({
  localDaemonId = null,
  localMachineName = null,
  hasLocalMachine = false,
  bootstrapping = false,
}: ChooseRuntimePageProps = {}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const backOrReplace = useBackOrReplace();
  const squadId = navigation.searchParams.get("squad");
  const runtimeParam = navigation.searchParams.get("runtime");
  const currentUserId = useAuthStore((state) => state.user?.id ?? null);

  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: qoderConnection } = useQuery(qoderOptions(wsId));
  const { data: sessions = [] } = useQuery(agentBuilderSessionListOptions(wsId));

  useWSEvent("daemon:register", () => {
    void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  });

  const groups = useMemo(() => {
    const machines = buildRuntimeMachines(runtimes, {
      now: Date.now(),
      localDaemonId,
      localMachineName,
      currentUserId,
      ensureLocalMachine: hasLocalMachine,
    });
    return buildRuntimeChoiceGroups(machines, currentUserId);
  }, [runtimes, localDaemonId, localMachineName, currentUserId, hasLocalMachine]);

  const member = members.find((m) => m.user_id === currentUserId);
  const canManage = member?.role === "owner" || member?.role === "admin";
  const ownerName = (ownerId: string | null) =>
    members.find((m) => m.user_id === ownerId)?.name ?? null;

  const qoder = qoderConnection ?? null;
  const qoderRuntime = groups.qoderRuntime;
  const qoderState = qoder
    ? qoderChoiceState(qoder, qoderRuntime?.status === "online")
    : null;

  const [selection, setSelection] = useState<Selection>(null);
  const [query, setQuery] = useState("");
  const [showConnectRemote, setShowConnectRemote] = useState(false);
  const [qoderDialogOpen, setQoderDialogOpen] = useState(false);
  const [selectQoderWhenReady, setSelectQoderWhenReady] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);

  const options = useMemo(() => {
    const byId = new Map<string, { option: HarnessOption; where: string }>();
    for (const device of [...(groups.local ? [groups.local] : []), ...groups.remotes]) {
      for (const option of device.harnesses) {
        byId.set(option.runtime.id, { option, where: device.machine.title });
      }
    }
    const cloudTitle = t(($) => $.creation_studio.runtime_step.groups.cloud);
    for (const option of groups.cloud) {
      byId.set(option.runtime.id, { option, where: cloudTitle });
    }
    return byId;
  }, [groups, t]);

  // `?runtime=` comes back from step 2's "change runtime" and from the Qoder
  // card on the runtimes page; it preselects once the list can resolve it.
  const seeded = useRef(false);
  useEffect(() => {
    if (seeded.current || runtimesLoading) return;
    seeded.current = true;
    if (!runtimeParam) return;
    if (qoderRuntime?.id === runtimeParam) setSelection({ kind: "qoder" });
    else if (options.has(runtimeParam)) {
      setSelection({ kind: "runtime", runtimeId: runtimeParam });
    }
  }, [runtimeParam, runtimesLoading, options, qoderRuntime]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (target?.closest("input, textarea, [contenteditable='true']")) return;
      event.preventDefault();
      searchRef.current?.focus();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  // A fresh connection registers its runtime asynchronously; keep the list
  // current while Qoder starts, and select it once it can take tasks.
  useEffect(() => {
    if (qoderState !== "starting") return;
    const timer = setInterval(() => {
      void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    }, QODER_REGISTER_POLL_MS);
    return () => clearInterval(timer);
  }, [qoderState, qc, wsId]);

  useEffect(() => {
    if (!selectQoderWhenReady) return;
    if (qoderState === "ready") {
      setSelectQoderWhenReady(false);
      setSelection({ kind: "qoder" });
    } else if (qoderState === "error" || qoderState === "stopped") {
      setSelectQoderWhenReady(false);
    }
  }, [selectQoderWhenReady, qoderState]);

  const selected =
    selection?.kind === "runtime" ? options.get(selection.runtimeId) ?? null : null;
  const nextRuntimeId =
    selection?.kind === "qoder"
      ? qoderState === "ready"
        ? qoderRuntime?.id ?? null
        : null
      : selected && selected.option.runtime.status === "online"
        ? selected.option.runtime.id
        : null;

  const goNext = (runtimeId: string) =>
    navigation.push(
      createPathWithParams(paths.newAgentManual(), {
        squad: squadId,
        runtime: runtimeId,
      }),
    );

  const resumeSession = (sessionId: string) =>
    navigation.push(
      createPathWithParams(paths.newAgentAiSession(sessionId), { squad: squadId }),
    );

  const summary =
    selection?.kind === "qoder"
      ? "Qoder Cloud Agents"
      : selected
        ? `${selected.option.label} · ${selected.where}`
        : null;

  return (
    <AgentCreateShell
      title={
        squadId
          ? t(($) => $.creation_studio.squad_title)
          : t(($) => $.creation_studio.title)
      }
      step={t(($) => $.creation_studio.steps.runtime)}
      stepIndex={1}
      onClose={() => backOrReplace(paths.agents())}
    >
      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-3xl px-5 py-8 sm:px-8">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <h2 className="min-w-0 text-balance text-title font-semibold tracking-tight">
              {t(($) => $.creation_studio.runtime_step.title)}
            </h2>
            <div className="relative w-full sm:w-60">
              <Search
                aria-hidden="true"
                className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              />
              <Input
                ref={searchRef}
                type="search"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Escape") setQuery("");
                }}
                placeholder={t(($) => $.creation_studio.runtime_step.search_placeholder)}
                aria-label={t(($) => $.creation_studio.runtime_step.search_placeholder)}
                className="h-8 pl-8 pr-8"
              />
              <kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 rounded-xs border bg-muted px-1 text-micro text-muted-foreground">
                /
              </kbd>
            </div>
          </div>

          {sessions.length > 0 ? (
            <div className="mt-6">
              <UnfinishedDraftsBanner sessions={sessions} onResume={resumeSession} />
            </div>
          ) : null}

          <div className="mt-5">
            <RuntimeChoiceList
              local={groups.local}
              localAvailable={hasLocalMachine}
              localStarting={bootstrapping}
              remotes={groups.remotes}
              hiddenRemoteCount={groups.hiddenRemoteCount}
              cloud={groups.cloud}
              qoder={qoder}
              qoderState={qoderState}
              qoderSelected={selection?.kind === "qoder"}
              selectedRuntimeId={selection?.kind === "runtime" ? selection.runtimeId : null}
              query={query}
              currentUserId={currentUserId}
              canManage={canManage}
              ownerName={ownerName}
              onSelect={(runtimeId) =>
                setSelection((current) =>
                  current?.kind === "runtime" && current.runtimeId === runtimeId
                    ? null
                    : { kind: "runtime", runtimeId },
                )
              }
              onSelectQoder={() =>
                setSelection((current) => (current?.kind === "qoder" ? null : { kind: "qoder" }))
              }
              onConnectQoder={() => setQoderDialogOpen(true)}
              onManageQoder={() =>
                navigation.push(paths.runtimeDetail(QODER_RUNTIME_LOCATOR))
              }
              onAddRemote={() => setShowConnectRemote(true)}
            />
          </div>
        </div>
      </main>
      <CreateFlowFooter
        stepIndex={1}
        status={
          summary ? (
            <p className="min-w-0 truncate text-caption text-muted-foreground">
              {t(($) => $.creation_studio.runtime_step.footer_selected)}{" "}
              <span className="font-medium text-foreground">{summary}</span>
            </p>
          ) : null
        }
      >
        <Button variant="ghost" onClick={() => backOrReplace(paths.agents())}>
          {t(($) => $.creation_studio.runtime_step.cancel)}
        </Button>
        <Button disabled={!nextRuntimeId} onClick={() => nextRuntimeId && goNext(nextRuntimeId)}>
          {t(($) => $.creation_studio.runtime_step.next)}
          <ChevronRight aria-hidden="true" className="size-3.5" />
        </Button>
      </CreateFlowFooter>
      {showConnectRemote ? (
        <ConnectRemoteDialog onClose={() => setShowConnectRemote(false)} />
      ) : null}
      {qoderDialogOpen ? (
        <RemoteRuntimeDialog
          onConnected={() => setSelectQoderWhenReady(true)}
          onClose={() => setQoderDialogOpen(false)}
        />
      ) : null}
    </AgentCreateShell>
  );
}
