"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { CircleDashed, Laptop, Loader2, Lock } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { api } from "@multica/core/api";
import {
  uniqueAgentName,
  type CodingToolId,
  type CodingToolRunEvent,
  type CodingToolsBridge,
} from "@multica/core/onboarding";
import { providerDisplayName, runtimeDisplayLabel } from "@multica/core/runtimes";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { AvatarUploadControl } from "../../common/avatar-upload-control";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { useT } from "../../i18n";
import { StepFooter, StepHeading } from "../components/step-shell";
import { CODING_TOOLS, codingTool, codingToolForProvider, type CodingToolInfo } from "../components/coding-tool-catalog";
import { CodingToolRow, type CodingToolRowState } from "../components/coding-tool-row";
import { type CodingToolRun, useCodingTools } from "../components/use-coding-tools";
import { useRuntimePicker } from "../components/use-runtime-picker";

export interface CreatePersonalAgentRequest {
  runtime: AgentRuntime;
  name: string;
  description: string;
  avatarUrl: string | null;
}

/** How long an installed tool may wait for its runtime before the daemon is restarted. */
const CONNECT_GRACE_MS = 6000;
const RESTART_COOLDOWN_MS = 30_000;
/** How long an installed tool may stay unregistered, a restart included, before it shows as failed. */
const STALL_MS = 30_000;

const NOT_DETECTED_RUN: CodingToolRun = {
  runId: "",
  kind: "install",
  phase: "install",
  status: "failed",
  error: { code: "not_detected", message: "" },
};

/**
 * Step 3: connect an AI coding tool and create the member's personal agent.
 *
 * With a `codingTools` bridge (Desktop) the step lists Claude Code, Codex and
 * Cursor CLI for this machine and installs or signs in to them in place. A
 * tool is ready once the daemon has registered it as an online runtime and it
 * is not known to be signed out. Any other tool the daemon found on this
 * machine is listed below them, ready to pick. Without a bridge (web, after a
 * computer has connected) it lists the member's online runtimes instead.
 */
export function StepAgentSetup({
  wsId,
  wsSlug,
  userName,
  workspaceName,
  codingTools,
  localDaemonId,
  onRuntimeRefresh,
  runtimesPending,
  onCreate,
  onSkip,
  skipHint = true,
}: {
  wsId: string;
  wsSlug: string;
  userName: string;
  /** Set when a member who joined an existing workspace sets up their agent there. */
  workspaceName?: string;
  codingTools?: CodingToolsBridge;
  /** Desktop's own daemon; its runtimes are the ones on this computer. */
  localDaemonId?: string | null;
  /** Restarts the local daemon so a newly installed tool registers. */
  onRuntimeRefresh?: () => void | Promise<void>;
  /** The local daemon is still booting or probing; don't restart it yet. */
  runtimesPending?: boolean;
  onCreate: (request: CreatePersonalAgentRequest) => Promise<void>;
  onSkip: () => Promise<void>;
  /** First-run skip writes a setup guide; say so next to the button. */
  skipHint?: boolean;
}) {
  const { t } = useT("onboarding");
  const qc = useQueryClient();
  const { runtimes: memberRuntimes } = useRuntimePicker(wsId, wsSlug);
  const isDesktop = !!codingTools;
  const runtimes = useMemo(
    () =>
      isDesktop && localDaemonId
        ? memberRuntimes.filter((runtime) => runtime.daemon_id === localDaemonId)
        : memberRuntimes,
    [isDesktop, localDaemonId, memberRuntimes],
  );

  const [selected, setSelected] = useState<string | null>(null);
  const [terminalFor, setTerminalFor] = useState<CodingToolId | null>(null);
  const [tested, setTested] = useState<CodingToolId[]>([]);
  // Installed tools the daemon still has not registered after STALL_MS, e.g.
  // a CLI that hangs on `--version`. Shown as failed instead of spinning.
  const [stalled, setStalled] = useState<CodingToolId[]>([]);

  const lastRestart = useRef(0);
  const restartDaemon = useCallback(
    async (force = false) => {
      const now = Date.now();
      if (!force && now - lastRestart.current < RESTART_COOLDOWN_MS) return;
      lastRestart.current = now;
      try {
        await onRuntimeRefresh?.();
      } finally {
        void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      }
    },
    [onRuntimeRefresh, qc, wsId],
  );

  const handleFinished = useCallback(
    (event: CodingToolRunEvent) => {
      const tool = codingTool(event.toolId);
      if (event.status === "done") {
        setTested((current) => (current.includes(event.toolId) ? current : [...current, event.toolId]));
        setSelected(event.toolId);
        toast.success(t(($) => $.step_agent.toast_ready, { tool: tool.name }));
        // A fresh binary is invisible to the daemon until it probes again.
        if (event.kind === "install") void restartDaemon(true);
      } else if (event.error?.code === "cancelled") {
        toast(t(($) => $.step_agent.toast_cancelled));
      }
    },
    [restartDaemon, t],
  );

  const runtimeFor = useCallback(
    (tool: CodingToolInfo) => {
      const matching = runtimes.filter((runtime) => runtime.provider === tool.provider);
      return matching.find((runtime) => runtime.status === "online") ?? null;
    },
    [runtimes],
  );

  // Rows and polling depend on each other: polling runs while nothing is ready.
  const [poll, setPoll] = useState(true);
  const tools = useCodingTools(codingTools, { poll, onFinished: handleFinished });

  const rowState = useCallback(
    (tool: CodingToolInfo): CodingToolRowState => {
      const run = tools.runs[tool.id];
      if (run?.status === "running") return "installing";
      if (run?.status === "failed") return "failed";
      const probe = tools.probes?.[tool.id];
      if (!tools.probes) return "checking";
      if (!probe?.installed) return "missing";
      if (probe.loggedIn === false) return "needs_login";
      if (runtimeFor(tool)) return "ready";
      return stalled.includes(tool.id) ? "failed" : "connecting";
    },
    [runtimeFor, stalled, tools.probes, tools.runs],
  );

  const states = useMemo(
    () => Object.fromEntries(CODING_TOOLS.map((tool) => [tool.id, rowState(tool)])) as Record<CodingToolId, CodingToolRowState>,
    [rowState],
  );
  const readyIds = CODING_TOOLS.filter((tool) => states[tool.id] === "ready").map((tool) => tool.id);
  const installingId = CODING_TOOLS.find((tool) => states[tool.id] === "installing")?.id;
  const failedId = CODING_TOOLS.find((tool) => tools.runs[tool.id]?.status === "failed")?.id;
  const connectingKey = CODING_TOOLS.filter((tool) => states[tool.id] === "connecting")
    .map((tool) => tool.id)
    .join(",");
  const connecting = connectingKey !== "";

  useEffect(() => {
    setPoll(readyIds.length === 0 || connecting || terminalFor !== null);
  }, [readyIds.length, connecting, terminalFor]);

  // Installed and signed in but no runtime yet: the daemon has not seen the
  // binary. Give a booting daemon a moment, then restart it.
  useEffect(() => {
    if (!isDesktop || !connecting) return;
    const refetch = window.setInterval(
      () => void qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) }),
      2000,
    );
    return () => window.clearInterval(refetch);
  }, [connecting, isDesktop, qc, wsId]);
  useEffect(() => {
    if (!isDesktop || !connecting || runtimesPending) return;
    const restart = window.setTimeout(() => void restartDaemon(), CONNECT_GRACE_MS);
    return () => window.clearTimeout(restart);
  }, [connecting, isDesktop, restartDaemon, runtimesPending]);
  useEffect(() => {
    if (!isDesktop || !connectingKey) return;
    const ids = connectingKey.split(",") as CodingToolId[];
    const stall = window.setTimeout(
      () => setStalled((current) => [...new Set([...current, ...ids])]),
      STALL_MS,
    );
    return () => window.clearTimeout(stall);
  }, [connectingKey, isDesktop]);

  // Web: rows are the member's runtimes, the coding tools this step offers first.
  // Desktop: the tools it cannot install, found by the daemon on this computer.
  const onlineRuntimes = useMemo(() => {
    const rank = (runtime: AgentRuntime) => {
      const index = CODING_TOOLS.findIndex((tool) => tool.provider === runtime.provider);
      return index < 0 ? CODING_TOOLS.length : index;
    };
    return runtimes
      .filter((runtime) => runtime.status === "online")
      .sort((a, b) => rank(a) - rank(b));
  }, [runtimes]);
  const otherRuntimes = useMemo(
    () =>
      isDesktop
        ? onlineRuntimes
            .filter((runtime) => !codingToolForProvider(runtime.provider))
            .sort((a, b) => providerDisplayName(a.provider).localeCompare(providerDisplayName(b.provider)))
        : [],
    [isDesktop, onlineRuntimes],
  );

  // Keep a valid selection: a fresh tool, else the first ready one.
  useEffect(() => {
    if (isDesktop) {
      if (selected && readyIds.includes(selected as CodingToolId)) return;
      if (selected && otherRuntimes.some((runtime) => runtime.id === selected)) return;
      const fresh = tools.fresh.find((id) => readyIds.includes(id));
      setSelected(fresh ?? readyIds[0] ?? otherRuntimes[0]?.id ?? null);
    } else {
      if (selected && onlineRuntimes.some((runtime) => runtime.id === selected)) return;
      setSelected(onlineRuntimes[0]?.id ?? null);
    }
  }, [isDesktop, onlineRuntimes, otherRuntimes, readyIds, selected, tools.fresh]);

  const selectedTool = isDesktop ? CODING_TOOLS.find((tool) => tool.id === selected) : undefined;
  const selectedRuntime = selectedTool
    ? runtimeFor(selectedTool)
    : (onlineRuntimes.find((runtime) => runtime.id === selected) ?? null);
  const selectedToolName = selectedRuntime
    ? (codingToolForProvider(selectedRuntime.provider)?.name ??
      (isDesktop ? providerDisplayName(selectedRuntime.provider) : runtimeDisplayLabel(selectedRuntime)))
    : null;

  // Default name, numbered when the workspace already has it.
  const { data: agents } = useQuery({
    queryKey: [...workspaceKeys.agents(wsId), "onboarding", wsSlug],
    queryFn: () => api.listAgents({ workspace_id: wsId, include_archived: true }, wsSlug),
  });
  const defaultName = useMemo(
    () =>
      uniqueAgentName(
        t(($) => $.step_agent.agent_default_name, { name: userName }),
        (agents ?? []).map((agent) => agent.name),
      ),
    [agents, t, userName],
  );
  const [name, setName] = useState("");
  const [nameTouched, setNameTouched] = useState(false);
  const [avatarUrl, setAvatarUrl] = useState<string | null>(null);
  useEffect(() => {
    if (!nameTouched) setName(defaultName);
  }, [defaultName, nameTouched]);

  const [creating, setCreating] = useState(false);
  const [skipping, setSkipping] = useState(false);
  const canCreate = !!selectedRuntime && name.trim().length > 0 && !creating && !skipping;

  const handleCreate = async () => {
    if (!selectedRuntime || !canCreate) return;
    setCreating(true);
    try {
      await onCreate({
        runtime: selectedRuntime,
        name: name.trim(),
        description: t(($) => $.step_agent.agent_description, { name: userName }),
        avatarUrl,
      });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.step_agent.create_failed));
    } finally {
      setCreating(false);
    }
  };

  const handleSkip = async () => {
    if (skipping || creating) return;
    setSkipping(true);
    try {
      await onSkip();
    } finally {
      setSkipping(false);
    }
  };

  const anyReady = isDesktop ? readyIds.length > 0 || otherRuntimes.length > 0 : onlineRuntimes.length > 0;

  let heading: string;
  let lede: string;
  if (workspaceName) {
    heading = t(($) => $.step_agent.heading_invitee, { workspace: workspaceName });
    lede = t(($) => $.step_agent.lede_invitee);
  } else if (!isDesktop) {
    heading = t(($) => $.step_agent.heading_web);
    lede = t(($) => $.step_agent.lede_web);
  } else if (failedId) {
    heading = t(($) => $.step_agent.heading_failed, { tool: codingTool(failedId).name });
    lede = t(($) => $.step_agent.lede_failed);
  } else if (installingId) {
    heading =
      tools.runs[installingId]?.kind === "login"
        ? t(($) => $.step_agent.heading_logging_in, { tool: codingTool(installingId).name })
        : t(($) => $.step_agent.heading_installing, { tool: codingTool(installingId).name });
    lede = t(($) => $.step_agent.lede_installing);
  } else if (selectedTool && tools.fresh.includes(selectedTool.id)) {
    heading = t(($) => $.step_agent.heading_ready_fresh, { tool: selectedTool.name });
    lede = t(($) => $.step_agent.lede_ready_fresh);
  } else if (anyReady) {
    heading = t(($) => $.step_agent.heading_pick);
    lede = t(($) => $.step_agent.lede_pick);
  } else {
    heading = t(($) => $.step_agent.heading_none);
    lede = t(($) => $.step_agent.lede_none);
  }

  const switchTarget = (failed: CodingToolId) =>
    CODING_TOOLS.find(
      (tool) => tool.id !== failed && (states[tool.id] === "missing" || states[tool.id] === "needs_login"),
    );

  const startRun = (toolId: CodingToolId, kind: "install" | "login") => {
    setTerminalFor(null);
    for (const tool of CODING_TOOLS) {
      if (tool.id !== toolId && states[tool.id] === "failed") tools.dismiss(tool.id);
    }
    void tools.start(toolId, kind);
  };

  const toolChip = selectedToolName
    ? { logo: selectedRuntime!.provider, label: t(($) => $.step_agent.chip_tool, { tool: selectedToolName }) }
    : installingId
      ? {
          logo: codingTool(installingId).provider,
          label: t(($) => $.step_agent.chip_tool_installing, { tool: codingTool(installingId).name }),
        }
      : null;

  return (
    <>
      <div className="animate-onboarding-enter flex flex-col gap-8 pt-2 sm:pt-6">
        <StepHeading title={heading} description={lede} />

        <section className="flex flex-col gap-2">
          <SectionLabel>
            {anyReady || !isDesktop ? t(($) => $.step_agent.section_tools) : t(($) => $.step_agent.section_install)}
          </SectionLabel>
          {isDesktop ? (
            <div className="flex flex-col gap-2" role="radiogroup">
              {CODING_TOOLS.map((tool) => {
                const state = states[tool.id];
                const probe = tools.probes?.[tool.id];
                const switchTo = state === "failed" ? switchTarget(tool.id) : undefined;
                const notDetected = state === "failed" && !tools.runs[tool.id];
                return (
                  <CodingToolRow
                    key={tool.id}
                    tool={tool}
                    state={state}
                    version={probe?.version ?? null}
                    fresh={tools.fresh.includes(tool.id)}
                    tested={tested.includes(tool.id)}
                    anyReady={anyReady}
                    selected={selected === tool.id}
                    busy={!!installingId && installingId !== tool.id}
                    run={notDetected ? NOT_DETECTED_RUN : tools.runs[tool.id]}
                    switchTo={switchTo}
                    switchKind={switchTo && states[switchTo.id] === "needs_login" ? "login" : "install"}
                    showTerminal={terminalFor === tool.id}
                    onSelect={() => setSelected(tool.id)}
                    onInstall={() => startRun(tool.id, "install")}
                    onLogin={() => startRun(tool.id, "login")}
                    onCancel={() => tools.cancel(tool.id)}
                    onRetry={() => {
                      if (!notDetected) {
                        startRun(tool.id, tools.runs[tool.id]?.kind ?? "install");
                        return;
                      }
                      setStalled((current) => current.filter((id) => id !== tool.id));
                      void restartDaemon(true);
                    }}
                    onSwitch={switchTo ? () => startRun(switchTo.id, states[switchTo.id] === "needs_login" ? "login" : "install") : undefined}
                    onShowTerminal={() => setTerminalFor(tool.id)}
                    onSubmitCode={(code) => tools.submitCode(tool.id, code)}
                  />
                );
              })}
              {otherRuntimes.length > 0 && (
                <>
                  <div className="pt-3">
                    <SectionLabel>{t(($) => $.step_agent.section_other_tools)}</SectionLabel>
                  </div>
                  {otherRuntimes.map((runtime) => (
                    <RuntimeRow
                      key={runtime.id}
                      runtime={runtime}
                      label={providerDisplayName(runtime.provider)}
                      selected={selected === runtime.id}
                      onSelect={() => setSelected(runtime.id)}
                    />
                  ))}
                </>
              )}
            </div>
          ) : (
            <div className="flex flex-col gap-2" role="radiogroup">
              {onlineRuntimes.map((runtime) => (
                <RuntimeRow
                  key={runtime.id}
                  runtime={runtime}
                  selected={selected === runtime.id}
                  onSelect={() => setSelected(runtime.id)}
                />
              ))}
            </div>
          )}
          {isDesktop && !anyReady && !installingId && !failedId && (
            <p className="flex items-center gap-2 pt-1 text-caption text-muted-foreground">
              <span aria-hidden className="inline-block size-1.5 rounded-full bg-success animate-pulse" />
              {t(($) => $.step_agent.detecting)}
            </p>
          )}
        </section>

        <section className="flex flex-col gap-2">
          <SectionLabel>{t(($) => $.step_agent.section_agent)}</SectionLabel>
          <div className="flex flex-col gap-3 rounded-lg border bg-card p-4">
            <div className="flex items-center gap-3">
              <AvatarUploadControl
                variant="agent"
                value={avatarUrl}
                name={name.trim() || userName}
                size={36}
                disabled={creating}
                onUploaded={setAvatarUrl}
                onEmojiSelected={setAvatarUrl}
                textIconEnabled
                onClear={() => setAvatarUrl(null)}
              />
              <Input
                aria-label={t(($) => $.step_agent.agent_name_label)}
                value={name}
                maxLength={100}
                onChange={(event) => {
                  setNameTouched(true);
                  setName(event.target.value);
                }}
                className="h-9 text-body font-medium"
              />
            </div>
            <div className="flex flex-wrap gap-1.5">
              <Chip>
                {toolChip ? (
                  <ProviderLogo provider={toolChip.logo} className="h-3 w-3" />
                ) : (
                  <CircleDashed className="h-3 w-3" />
                )}
                {toolChip ? toolChip.label : t(($) => $.step_agent.chip_tool_none)}
              </Chip>
              <Chip>
                <Laptop className="h-3 w-3" />
                {isDesktop || !selectedRuntime
                  ? t(($) => $.step_agent.chip_this_computer)
                  : t(($) => $.step_agent.chip_your_computer)}
              </Chip>
              <Chip>
                <Lock className="h-3 w-3" />
                {t(($) => $.step_agent.chip_private)}
              </Chip>
            </div>
          </div>
          <p className="text-caption text-muted-foreground">{t(($) => $.step_agent.agent_note)}</p>
        </section>
      </div>

      <StepFooter hint={!anyReady && skipHint ? t(($) => $.step_agent.skip_hint) : undefined}>
        <Button className="w-full" disabled={!canCreate} onClick={handleCreate}>
          {creating && <Loader2 className="h-4 w-4 animate-spin" />}
          {creating ? t(($) => $.step_agent.creating) : t(($) => $.step_agent.create)}
        </Button>
        {(!anyReady || workspaceName) && (
          <Button variant="ghost" className="w-full" disabled={skipping || creating} onClick={handleSkip}>
            {skipping && <Loader2 className="h-4 w-4 animate-spin" />}
            {t(($) => $.step_agent.skip)}
          </Button>
        )}
      </StepFooter>
    </>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return <div className="text-caption font-medium text-muted-foreground">{children}</div>;
}

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full border bg-background px-2.5 py-1 text-caption text-muted-foreground">
      {children}
    </span>
  );
}

function RuntimeRow({
  runtime,
  label = runtimeDisplayLabel(runtime),
  selected,
  onSelect,
}: {
  runtime: AgentRuntime;
  label?: string;
  selected: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("onboarding");
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "flex items-center gap-3 rounded-lg border bg-card px-4 py-3 text-left transition-colors hover:border-foreground/30",
        selected && "border-foreground",
      )}
    >
      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border bg-background">
        <ProviderLogo provider={runtime.provider} className="h-4 w-4" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="truncate text-body font-medium text-foreground">{label}</div>
        <div className="text-caption text-success">{t(($) => $.step_agent.status_online)}</div>
      </div>
      <span
        aria-hidden
        className={cn(
          "flex h-4 w-4 shrink-0 items-center justify-center rounded-full border-[1.5px]",
          selected ? "border-foreground" : "border-muted-foreground/40",
        )}
      >
        {selected && <span className="h-2 w-2 rounded-full bg-foreground" />}
      </span>
    </button>
  );
}
