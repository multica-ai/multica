"use client";

import { useEffect, useId, useState } from "react";
import { AlertCircle, Plus } from "@multica/ui/icons";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import {
  MAX_CONCURRENCY,
  MAX_RUN_TIMEOUT,
  MIN_RUN_TIMEOUT,
} from "@multica/core/runtimes/qoder";
import {
  CLAUDE_MANAGED_PROVIDER,
  claudeManagedOptions,
  isManagedClaudeRuntime,
  useCheckClaude,
  useClaudeEnvironments,
  useDisconnectClaude,
  useResumeClaude,
  useSaveClaude,
  useStopClaude,
  type ClaudeConnection,
  type ClaudeInput,
} from "@multica/core/runtimes/claude-managed";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { cn } from "@multica/ui/lib/utils";
import { CollectionPageState } from "../../layout/collection-page";
import {
  OBJECT_PAGE_BODY,
  ObjectIdentity,
  ObjectPage,
  ObjectSection,
} from "../../layout/object-page";
import { AppLink, useNavigation } from "../../navigation";
import { createPathWithParams } from "../../agents/create/squad-param";
import { useT } from "../../i18n";
import { ProviderLogo } from "./provider-logo";
import { QoderSection } from "./qoder-connection-cards";
import { ServingAgentList } from "./runtime-detail";
import { parseActiveAgentsConflict } from "./delete-runtime-dialog";

const LAYOUT_ID = "multica_runtime_detail_layout";

/** Route locator of the workspace's Claude Managed Agents page. */
export const CLAUDE_MANAGED_LOCATOR = "claude-managed";

/**
 * Whether a runtimes route locator names the managed Claude connection: its
 * own locator, the machine its runtime registers under, or that runtime's id.
 * A standalone Claude bridge's runtime keeps the generic machine page.
 */
export function isClaudeManagedLocator(
  locator: string,
  runtimes: AgentRuntime[],
): boolean {
  return (
    locator === CLAUDE_MANAGED_LOCATOR ||
    locator.startsWith("cloud:claude-managed-") ||
    runtimes.some(
      (runtime) => runtime.id === locator && isManagedClaudeRuntime(runtime),
    )
  );
}

export function ClaudeStatusBadge({
  connection,
}: {
  connection: ClaudeConnection;
}) {
  const { t } = useT("runtimes");
  if (!connection.configured) {
    return (
      <span className="text-caption text-muted-foreground">
        {t(($) => $.claude_managed.not_connected)}
      </span>
    );
  }
  const status = !connection.enabled ? "stopped" : connection.status;
  const tone =
    status === "online"
      ? "bg-success"
      : status === "error"
        ? "bg-destructive"
        : "bg-muted-foreground";
  const label =
    status === "online"
      ? t(($) => $.claude_managed.status.online)
      : status === "error"
        ? t(($) => $.claude_managed.status.error)
        : status === "stopped"
          ? t(($) => $.claude_managed.status.stopped)
          : status === "starting"
            ? t(($) => $.claude_managed.status.starting)
            : t(($) => $.claude_managed.status.offline);
  return (
    <span className="inline-flex items-center gap-1.5 text-caption text-muted-foreground">
      <span aria-hidden="true" className={cn("size-2 rounded-full", tone)} />
      {label}
    </span>
  );
}

type RepoRow = { url: string; token: string };

/** The saved connection as an input that keeps every stored secret. */
function savedClaudeInput(data: ClaudeConnection): ClaudeInput {
  return {
    name: data.name,
    environmentId: data.environmentId,
    anthropicToken: "",
    vaultIds: data.vaultIds,
    maxConcurrency: data.maxConcurrency,
    runTimeoutMinutes: data.runTimeoutMinutes,
  };
}

/**
 * Connect or edit the workspace's Claude Managed Agents connection. Blank
 * secrets keep the saved ones, so editing never needs the key again.
 */
export function ClaudeConnectionForm({
  wsId,
  connection,
  onSaved,
}: {
  wsId: string;
  connection: ClaudeConnection;
  onSaved?: () => void;
}) {
  const { t } = useT("runtimes");
  const save = useSaveClaude(wsId);
  const check = useCheckClaude(wsId);
  const environments = useClaudeEnvironments(wsId);
  const { mutate: loadEnvironments } = environments;
  const ids = {
    name: useId(),
    key: useId(),
    environment: useId(),
    vaults: useId(),
    concurrency: useId(),
    timeout: useId(),
  };
  const [name, setName] = useState(connection.name || "Claude Managed Agents");
  const [token, setToken] = useState("");
  const [environmentId, setEnvironmentId] = useState(connection.environmentId);
  const [vaults, setVaults] = useState(connection.vaultIds.join(", "));
  const [repos, setRepos] = useState<RepoRow[]>(
    connection.repositories.map((url) => ({ url, token: "" })),
  );
  const [maxConcurrency, setMaxConcurrency] = useState(connection.maxConcurrency);
  const [runTimeoutMinutes, setRunTimeoutMinutes] = useState(
    connection.runTimeoutMinutes,
  );
  const trimmedToken = token.trim();
  const canListEnvironments = !!trimmedToken || connection.hasToken;

  // A new key lists its own environments; a saved key lists the saved ones.
  useEffect(() => {
    if (!canListEnvironments) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      loadEnvironments(
        { token: trimmedToken, signal: controller.signal },
        {
          onSuccess: (items) => {
            if (controller.signal.aborted) return;
            setEnvironmentId((current) =>
              items.some((item) => item.id === current)
                ? current
                : items.length === 1
                  ? items[0]!.id
                  : "",
            );
          },
        },
      );
    }, 600);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [trimmedToken, canListEnvironments, loadEnvironments]);

  const known = new Set(connection.repositories);
  const reposValid = repos.every(
    (row) =>
      row.url.trim().length > 0 &&
      (row.token.trim().length > 0 || known.has(row.url.trim())),
  );
  const limitsValid =
    maxConcurrency >= 1 &&
    maxConcurrency <= MAX_CONCURRENCY &&
    runTimeoutMinutes >= MIN_RUN_TIMEOUT &&
    runTimeoutMinutes <= MAX_RUN_TIMEOUT;
  const input = (): ClaudeInput => ({
    name: name.trim(),
    environmentId,
    anthropicToken: trimmedToken,
    githubTokens: Object.fromEntries(
      repos.map((row) => [row.url.trim(), row.token.trim()]),
    ),
    vaultIds: vaults
      .split(",")
      .map((id) => id.trim())
      .filter(Boolean),
    maxConcurrency,
    runTimeoutMinutes,
  });
  const ready =
    !!name.trim() &&
    !!environmentId &&
    canListEnvironments &&
    reposValid &&
    limitsValid;
  const busy = save.isPending || check.isPending;

  return (
    <form
      className="space-y-5"
      onSubmit={(event) => {
        event.preventDefault();
        if (!ready || busy) return;
        save.mutate(input(), {
          onSuccess: () => {
            setToken("");
            toast.success(t(($) => $.claude_managed.saved));
            onSaved?.();
          },
        });
      }}
    >
      <div className="space-y-2">
        <label htmlFor={ids.name} className="text-caption font-medium">
          {t(($) => $.claude_managed.name)}
        </label>
        <Input
          id={ids.name}
          value={name}
          disabled={busy}
          onChange={(event) => setName(event.target.value)}
        />
      </div>
      <div className="space-y-2">
        <label htmlFor={ids.key} className="text-caption font-medium">
          {t(($) => $.claude_managed.api_key)}
        </label>
        <Input
          id={ids.key}
          type="password"
          autoComplete="off"
          placeholder={
            connection.hasToken
              ? t(($) => $.claude_managed.keep_secret)
              : "sk-ant-…"
          }
          value={token}
          disabled={busy}
          onChange={(event) => setToken(event.target.value)}
        />
        <p className="text-caption text-muted-foreground">
          {t(($) => $.claude_managed.api_key_hint)}
        </p>
      </div>
      <div className="space-y-2">
        <p id={ids.environment} className="text-caption font-medium">
          {t(($) => $.claude_managed.environment)}
        </p>
        <Select
          items={(environments.data ?? []).map((item) => ({
            value: item.id,
            label: item.name,
          }))}
          value={environmentId || null}
          onValueChange={(next) => setEnvironmentId(next ?? "")}
          disabled={busy || !environments.data?.length}
        >
          <SelectTrigger className="w-full" aria-labelledby={ids.environment}>
            <SelectValue
              placeholder={t(($) => $.claude_managed.select_environment)}
            />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            {(environments.data ?? []).map((item) => (
              <SelectItem key={item.id} value={item.id}>
                {item.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {environments.isPending && (
          <p role="status" className="text-caption text-muted-foreground">
            {t(($) => $.claude_managed.loading)}
          </p>
        )}
        {environments.isError && (
          <p role="alert" className="text-caption text-destructive">
            {environments.error.message}
          </p>
        )}
        {environments.isSuccess && !environments.data.length && (
          <p role="status" className="text-caption text-muted-foreground">
            {t(($) => $.claude_managed.no_environments)}
          </p>
        )}
      </div>
      <div className="space-y-2">
        <label htmlFor={ids.vaults} className="text-caption font-medium">
          {t(($) => $.claude_managed.vaults)}
        </label>
        <Input
          id={ids.vaults}
          // eslint-disable-next-line no-restricted-syntax -- the vault ID prefix, a technical value
          placeholder="vlt_…"
          autoComplete="off"
          spellCheck={false}
          value={vaults}
          disabled={busy}
          onChange={(event) => setVaults(event.target.value)}
        />
        <p className="text-caption text-muted-foreground">
          {t(($) => $.claude_managed.vaults_hint)}
        </p>
      </div>
      <fieldset className="space-y-2">
        <legend className="text-caption font-medium">
          {t(($) => $.claude_managed.repositories)}
        </legend>
        <p className="text-caption text-muted-foreground">
          {t(($) => $.claude_managed.repositories_hint)}
        </p>
        {repos.map((row, index) => (
          <div key={index} className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
            <Input
              type="url"
              aria-label={t(($) => $.claude_managed.github_url)}
              placeholder="https://github.com/owner/repo"
              value={row.url}
              disabled={busy}
              onChange={(event) =>
                setRepos(
                  repos.map((item, i) =>
                    i === index ? { ...item, url: event.target.value } : item,
                  ),
                )
              }
            />
            <Input
              type="password"
              aria-label={t(($) => $.claude_managed.github_token)}
              autoComplete="off"
              placeholder={
                known.has(row.url.trim())
                  ? t(($) => $.claude_managed.keep_secret)
                  : t(($) => $.claude_managed.github_token)
              }
              value={row.token}
              disabled={busy}
              onChange={(event) =>
                setRepos(
                  repos.map((item, i) =>
                    i === index ? { ...item, token: event.target.value } : item,
                  ),
                )
              }
            />
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={busy}
              onClick={() => setRepos(repos.filter((_, i) => i !== index))}
            >
              {t(($) => $.claude_managed.remove)}
            </Button>
          </div>
        ))}
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => setRepos([...repos, { url: "", token: "" }])}
        >
          {t(($) => $.claude_managed.add_repository)}
        </Button>
      </fieldset>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <label htmlFor={ids.concurrency} className="text-caption font-medium">
            {t(($) => $.claude_managed.concurrency)}
          </label>
          <Input
            id={ids.concurrency}
            type="number"
            min={1}
            max={MAX_CONCURRENCY}
            value={maxConcurrency}
            disabled={busy}
            onChange={(event) => setMaxConcurrency(Number(event.target.value))}
          />
        </div>
        <div className="space-y-2">
          <label htmlFor={ids.timeout} className="text-caption font-medium">
            {t(($) => $.claude_managed.run_timeout)}
          </label>
          <Input
            id={ids.timeout}
            type="number"
            min={MIN_RUN_TIMEOUT}
            max={MAX_RUN_TIMEOUT}
            value={runTimeoutMinutes}
            disabled={busy}
            onChange={(event) =>
              setRunTimeoutMinutes(Number(event.target.value))
            }
          />
        </div>
      </div>
      {(save.isError || check.isError) && (
        <p role="alert" className="text-caption text-destructive">
          {(save.error ?? check.error)?.message}
        </p>
      )}
      {check.isSuccess && (
        <p role="status" className="text-caption text-muted-foreground">
          {t(($) => $.claude_managed.check_passed)}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={!ready || busy}>
          {save.isPending
            ? t(($) => $.claude_managed.working)
            : connection.configured
              ? t(($) => $.claude_managed.save)
              : t(($) => $.claude_managed.connect)}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={!ready || busy}
          onClick={() => check.mutate(input())}
        >
          {check.isPending
            ? t(($) => $.claude_managed.working)
            : t(($) => $.claude_managed.check)}
        </Button>
      </div>
    </form>
  );
}

/** First-time connection; an existing one is managed on its runtime page. */
export function ClaudeConnectDialog({
  onClose,
  onConnected,
}: {
  onClose: () => void;
  onConnected?: () => void;
}) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const connection = useQuery(claudeManagedOptions(wsId));
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.claude_managed.connect_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.claude_managed.cloud_description)}
          </DialogDescription>
        </DialogHeader>
        {connection.data?.available === false ? (
          <p role="alert" className="text-body text-destructive">
            {t(($) => $.claude_managed.unavailable)}
          </p>
        ) : connection.data ? (
          <ClaudeConnectionForm
            wsId={wsId}
            connection={connection.data}
            onSaved={() => {
              onConnected?.();
              onClose();
            }}
          />
        ) : (
          <p role="status" className="text-caption text-muted-foreground">
            {t(($) => $.claude_managed.loading)}
          </p>
        )}
      </DialogContent>
    </Dialog>
  );
}

/** The workspace's Claude Managed Agents connection and its managed runtime. */
export function ClaudeManagedPage() {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const connection = useQuery(claudeManagedOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);
  const stop = useStopClaude(wsId);
  const resume = useResumeClaude(wsId);
  const disconnect = useDisconnectClaude(wsId);
  const [disconnectOpen, setDisconnectOpen] = useState(false);

  const title = t(($) => $.claude_managed.cloud_title);
  const segments = [{ href: paths.runtimes(), label: t(($) => $.page.title) }];
  const leaf = (
    <span className="flex min-w-0 items-center gap-1.5">
      <ProviderLogo provider={CLAUDE_MANAGED_PROVIDER} className="size-3.5 shrink-0" />
      <span className="truncate">{title}</span>
    </span>
  );

  const data = connection.data;
  if (connection.isPending) {
    return (
      <ObjectPage layoutId={LAYOUT_ID} segments={segments} leaf={leaf}>
        <ClaudeBodySkeleton />
      </ObjectPage>
    );
  }
  if (connection.isError || data?.available !== true) {
    return (
      <ObjectPage layoutId={LAYOUT_ID} segments={segments} leaf={leaf}>
        <CollectionPageState
          icon={AlertCircle}
          tone="destructive"
          title={
            connection.isError
              ? connection.error.message
              : t(($) => $.claude_managed.unavailable)
          }
          actions={
            <Button
              size="sm"
              variant="outline"
              render={<AppLink href={paths.runtimes()} />}
              nativeButton={false}
            >
              {t(($) => $.detail.all_runtimes)}
            </Button>
          }
        />
      </ObjectPage>
    );
  }

  const member = members.find((m) => m.user_id === userId);
  const isAdmin = member?.role === "owner" || member?.role === "admin";
  const runtime =
    runtimes.find((item) => isManagedClaudeRuntime(item)) ?? null;
  const boundAgents = agents.filter(
    (agent) => !agent.archived_at && agent.runtime_id === runtime?.id,
  );
  const ready =
    data.configured && data.enabled && data.status === "online" && !!runtime;
  const busy = stop.isPending || resume.isPending || disconnect.isPending;
  const failing = data.enabled && data.status === "error" && !!data.lastError;

  return (
    <ObjectPage
      layoutId={LAYOUT_ID}
      segments={segments}
      leaf={leaf}
      actions={
        ready && runtime ? (
          <Button
            size="sm"
            onClick={() =>
              navigation.push(
                createPathWithParams(paths.newAgentManual(), {
                  runtime: runtime.id,
                }),
              )
            }
          >
            <Plus aria-hidden="true" className="size-3.5" />
            {t(($) => $.claude_managed.create_agent)}
          </Button>
        ) : undefined
      }
    >
      <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
        <div className="space-y-3">
          <ObjectIdentity
            media={
              <div className="flex size-12 items-center justify-center rounded-xl border bg-muted/50">
                <ProviderLogo provider={CLAUDE_MANAGED_PROVIDER} className="size-6" />
              </div>
            }
            title={title}
            trailing={<ClaudeStatusBadge connection={data} />}
            description={
              data.configured ? (
                <span className="tabular-nums">
                  {t(($) => $.claude_managed.agent_count, {
                    count: boundAgents.length,
                  })}
                  {" · "}
                  {t(($) => $.claude_managed.usage, {
                    active: data.activeRuns,
                    limit: data.maxConcurrency,
                    queued: data.queuedRuns,
                  })}
                </span>
              ) : undefined
            }
          />
          {failing && (
            <p
              role="alert"
              className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2 text-caption text-destructive"
            >
              {data.lastError}
            </p>
          )}
        </div>

        <QoderSection title={t(($) => $.claude_managed.connection)}>
          <div className="p-4">
            {isAdmin ? (
              <ClaudeConnectionForm
                key={data.configured ? "edit" : "connect"}
                wsId={wsId}
                connection={data}
              />
            ) : (
              <p className="text-caption text-muted-foreground">
                {data.configured
                  ? data.environmentId
                  : t(($) => $.claude_managed.not_connected)}
              </p>
            )}
          </div>
        </QoderSection>

        <ObjectSection
          title={t(($) => $.page.columns.agents)}
          count={boundAgents.length}
        >
          <ServingAgentList
            agents={boundAgents}
            presenceMap={presenceMap}
            agentHref={(id) => paths.agentDetail(id)}
          />
        </ObjectSection>

        {isAdmin && data.configured && (
          <QoderSection title={t(($) => $.claude_managed.manage)}>
            <div className="flex flex-wrap gap-2 p-4">
              {data.enabled ? (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  onClick={() =>
                    stop.mutate(undefined, {
                      onError: (err) => toast.error(err.message),
                    })
                  }
                >
                  {t(($) => $.claude_managed.pause)}
                </Button>
              ) : (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  onClick={() =>
                    resume.mutate(savedClaudeInput(data), {
                      onError: (err) => toast.error(err.message),
                    })
                  }
                >
                  {t(($) => $.claude_managed.resume)}
                </Button>
              )}
              <Button
                variant="destructive"
                size="sm"
                disabled={busy}
                onClick={() => setDisconnectOpen(true)}
              >
                {t(($) => $.claude_managed.disconnect)}
              </Button>
            </div>
          </QoderSection>
        )}
      </div>
      <AlertDialog
        open={disconnectOpen}
        onOpenChange={(open) => {
          if (!open && !disconnect.isPending) setDisconnectOpen(false);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.claude_managed.disconnect_confirm)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.claude_managed.disconnect_hint)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {boundAgents.length > 0 && (
            <ul className="list-disc space-y-0.5 pl-5 text-caption">
              {boundAgents.map((agent) => (
                <li key={agent.id}>{agent.name}</li>
              ))}
            </ul>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnect.isPending}>
              {t(($) => $.qoder.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={disconnect.isPending}
              onClick={() =>
                disconnect.mutate(
                  boundAgents.map((agent) => agent.id),
                  {
                    onSuccess: () => {
                      setDisconnectOpen(false);
                      toast.success(t(($) => $.claude_managed.disconnected));
                      navigation.replace(paths.runtimes());
                    },
                    onError: (err) => {
                      setDisconnectOpen(false);
                      // A changed agent set refreshes the list for another try.
                      if (parseActiveAgentsConflict(err)) void connection.refetch();
                      toast.error(err.message);
                    },
                  },
                )
              }
            >
              {t(($) => $.claude_managed.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </ObjectPage>
  );
}

function ClaudeBodySkeleton() {
  return (
    <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
      <div className="flex items-center gap-3">
        <Skeleton className="size-12 rounded-xl" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-5 w-56" />
          <Skeleton className="h-4 w-40" />
        </div>
      </div>
      <Skeleton className="h-64 w-full rounded-lg" />
    </div>
  );
}
