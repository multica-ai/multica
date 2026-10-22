"use client";

import { useEffect, useState, type ReactNode } from "react";
import {
  CheckCircle2,
  ChevronRight,
  CircleAlert,
  FolderGit2,
  Loader2,
  Plug,
  Settings2,
  ShieldCheck,
} from "lucide-react";
import {
  MAX_CONCURRENCY,
  MAX_RUN_TIMEOUT,
  MIN_RUN_TIMEOUT,
  QODER_REGIONS,
  useQoderEnvironments,
  useSaveQoder,
  type QoderConnection,
  type QoderRegion,
} from "@multica/core/runtimes/qoder";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const DEFAULT_NAME = "Qoder Cloud Agents";

export function regionOf(baseUrl: string): QoderRegion {
  return (
    (Object.keys(QODER_REGIONS) as QoderRegion[]).find(
      (key) => QODER_REGIONS[key] === baseUrl,
    ) ?? "global"
  );
}

export function RegionToggle({
  value,
  onChange,
  disabled,
  labelledBy,
}: {
  value: QoderRegion;
  onChange: (region: QoderRegion) => void;
  disabled?: boolean;
  labelledBy: string;
}) {
  const { t } = useT("runtimes");
  return (
    <div
      role="radiogroup"
      aria-labelledby={labelledBy}
      className="grid grid-cols-2 gap-0.5 rounded-lg bg-muted p-0.5"
    >
      {(["global", "cn"] as const).map((region) => (
        <button
          key={region}
          type="button"
          role="radio"
          aria-checked={value === region}
          disabled={disabled}
          onClick={() => onChange(region)}
          className={cn(
            "truncate rounded-md px-2 py-1.5 text-caption text-muted-foreground transition-colors",
            "hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            value === region && "bg-background font-medium text-foreground shadow-sm",
          )}
        >
          {region === "global"
            ? t(($) => $.qoder.region_global)
            : t(($) => $.qoder.region_cn)}
        </button>
      ))}
    </div>
  );
}

/**
 * First-time Qoder connection: region, PAT and environment up front, the
 * optional GitHub and execution settings folded away. Saving starts the
 * managed runtime; editing an existing connection stays in
 * QoderConfiguration.
 */
export function QoderConnectForm({
  wsId,
  connection,
  submitLabel,
  onConnected,
}: {
  wsId: string;
  connection: QoderConnection;
  submitLabel: string;
  onConnected?: () => void;
}) {
  const { t } = useT("runtimes");
  const save = useSaveQoder(wsId);
  const environments = useQoderEnvironments(wsId);
  const { mutate: loadEnvironments, reset: resetEnvironments } = environments;

  const [region, setRegion] = useState<QoderRegion>(() =>
    regionOf(connection.baseUrl),
  );
  const [token, setToken] = useState("");
  const [environmentId, setEnvironmentId] = useState("");
  const [showRepos, setShowRepos] = useState(false);
  const [repos, setRepos] = useState<{ url: string; token: string }[]>([]);
  const [showExecution, setShowExecution] = useState(false);
  const [maxConcurrency, setMaxConcurrency] = useState(
    connection.maxConcurrency,
  );
  const [runTimeoutMinutes, setRunTimeoutMinutes] = useState(
    connection.runTimeoutMinutes,
  );

  const baseUrl = QODER_REGIONS[region];
  const trimmedToken = token.trim();

  useEffect(() => {
    resetEnvironments();
    setEnvironmentId("");
    if (!trimmedToken) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      loadEnvironments(
        { token: trimmedToken, baseUrl, signal: controller.signal },
        {
          onSuccess: (items) => {
            if (controller.signal.aborted) return;
            if (items.length === 1) setEnvironmentId(items[0]!.id);
          },
        },
      );
    }, 600);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [trimmedToken, baseUrl, loadEnvironments, resetEnvironments]);

  const environmentValid =
    !!environmentId &&
    environments.data?.some((item) => item.id === environmentId) === true;
  const reposValid = repos.every(
    (row) => row.url.trim().length > 0 && row.token.trim().length > 0,
  );
  const limitsValid =
    maxConcurrency >= 1 &&
    maxConcurrency <= MAX_CONCURRENCY &&
    runTimeoutMinutes >= MIN_RUN_TIMEOUT &&
    runTimeoutMinutes <= MAX_RUN_TIMEOUT;
  const canSubmit =
    environmentValid && reposValid && limitsValid && !save.isPending;
  const otherRegion: QoderRegion = region === "cn" ? "global" : "cn";

  const submit = async () => {
    if (!canSubmit) return;
    try {
      await save.mutateAsync({
        name: connection.name || DEFAULT_NAME,
        baseUrl,
        environmentId,
        qoderToken: trimmedToken,
        githubTokens: Object.fromEntries(
          repos.map((row) => [row.url.trim(), row.token.trim()]),
        ),
        maxConcurrency,
        runTimeoutMinutes,
      });
      setToken("");
      onConnected?.();
    } catch {
      /* Mutation error is rendered below. */
    }
  };

  return (
    <form
      className="flex min-h-0 flex-1 flex-col gap-5"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <div className="space-y-2">
        <p id="qoder-connect-region" className="text-caption font-medium">
          {t(($) => $.qoder.region)}
        </p>
        <RegionToggle
          value={region}
          onChange={setRegion}
          disabled={save.isPending}
          labelledBy="qoder-connect-region"
        />
        <p className="text-caption text-muted-foreground">
          {t(($) => $.qoder.region_hint)}
        </p>
      </div>

      <label className="block space-y-2">
        <span className="block text-caption font-medium">
          {t(($) => $.qoder.connect.token)}
        </span>
        <Input
          type="password"
          autoComplete="new-password"
          value={token}
          // eslint-disable-next-line no-restricted-syntax -- Qoder token prefix
          placeholder="pt-…"
          aria-invalid={environments.isError}
          disabled={save.isPending}
          className="font-mono"
          onChange={(event) => setToken(event.target.value)}
        />
      </label>
      {environments.isError ? (
        <div className="-mt-3 space-y-2">
          <p
            role="alert"
            className="flex items-start gap-1.5 text-caption text-destructive"
          >
            <CircleAlert aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
            <span className="min-w-0 break-words">
              {t(($) => $.qoder.connect.token_rejected, {
                region:
                  region === "global"
                    ? t(($) => $.qoder.region_global)
                    : t(($) => $.qoder.region_cn),
              })}
            </span>
          </p>
          <Button
            type="button"
            size="xs"
            variant="outline"
            onClick={() => setRegion(otherRegion)}
          >
            {otherRegion === "cn"
              ? t(($) => $.qoder.connect.retry_cn)
              : t(($) => $.qoder.connect.retry_global)}
          </Button>
        </div>
      ) : null}

      <div className="space-y-2">
        <p id="qoder-connect-environment" className="text-caption font-medium">
          {t(($) => $.qoder.environment)}
        </p>
        {environments.isPending ? (
          <div
            role="status"
            className="flex h-9 items-center gap-2 rounded-lg border bg-muted/30 px-3 text-caption text-muted-foreground"
          >
            <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
            {t(($) => $.qoder.connect.verifying)}
          </div>
        ) : environments.isSuccess && environments.data.length > 1 ? (
          <Select
            items={environments.data.map((item) => ({
              value: item.id,
              label: item.name,
            }))}
            value={environmentId || null}
            onValueChange={(value) => setEnvironmentId(value ?? "")}
            disabled={save.isPending}
          >
            <SelectTrigger
              className="w-full"
              aria-labelledby="qoder-connect-environment"
            >
              <SelectValue placeholder={t(($) => $.qoder.select_environment)} />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              {environments.data.map((item) => (
                <SelectItem key={item.id} value={item.id}>
                  {item.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : environments.isSuccess && environments.data.length === 1 ? (
          <div className="flex h-9 items-center gap-2 rounded-lg border px-3 text-body">
            <span className="min-w-0 flex-1 truncate">
              {environments.data[0]!.name}
            </span>
            <span className="inline-flex shrink-0 items-center gap-1 text-caption text-success">
              <CheckCircle2 aria-hidden="true" className="size-3.5" />
              {t(($) => $.qoder.connect.verified)}
            </span>
          </div>
        ) : (
          <div className="flex h-9 items-center rounded-lg border bg-muted/30 px-3 text-caption text-muted-foreground">
            {environments.isSuccess
              ? t(($) => $.qoder.no_environments)
              : t(($) => $.qoder.connect.environment_waiting)}
          </div>
        )}
      </div>

      <div className="divide-y rounded-lg border">
        <Disclosure
          icon={FolderGit2}
          title={t(($) => $.qoder.repositories)}
          summary={
            repos.length > 0
              ? t(($) => $.qoder.connect.repo_count, { count: repos.length })
              : t(($) => $.qoder.connect.optional)
          }
          open={showRepos}
          onToggle={() => setShowRepos((open) => !open)}
        >
          <p className="text-caption text-muted-foreground">
            {t(($) => $.qoder.repositories_hint)}
          </p>
          {repos.map((row, index) => (
            <div key={index} className="space-y-2 rounded-md bg-muted/40 p-2.5">
              <Input
                type="url"
                value={row.url}
                aria-label={t(($) => $.qoder.github_url)}
                placeholder="https://github.com/org/repo"
                disabled={save.isPending}
                onChange={(event) =>
                  setRepos((old) =>
                    old.map((r, i) =>
                      i === index ? { ...r, url: event.target.value } : r,
                    ),
                  )
                }
              />
              <Input
                type="password"
                autoComplete="new-password"
                value={row.token}
                aria-label={t(($) => $.qoder.github_token)}
                placeholder={t(($) => $.qoder.github_token)}
                disabled={save.isPending}
                onChange={(event) =>
                  setRepos((old) =>
                    old.map((r, i) =>
                      i === index ? { ...r, token: event.target.value } : r,
                    ),
                  )
                }
              />
              <Button
                type="button"
                size="xs"
                variant="ghost"
                disabled={save.isPending}
                onClick={() =>
                  setRepos((old) => old.filter((_, i) => i !== index))
                }
              >
                {t(($) => $.qoder.remove)}
              </Button>
            </div>
          ))}
          <Button
            type="button"
            size="xs"
            variant="outline"
            disabled={save.isPending}
            onClick={() => setRepos((old) => [...old, { url: "", token: "" }])}
          >
            {t(($) => $.qoder.add_repository)}
          </Button>
        </Disclosure>
        <Disclosure
          icon={Settings2}
          title={t(($) => $.qoder.execution)}
          summary={t(($) => $.qoder.connect.execution_summary, {
            concurrency: maxConcurrency,
            timeout: runTimeoutMinutes,
          })}
          open={showExecution}
          onToggle={() => setShowExecution((open) => !open)}
        >
          <div className="grid grid-cols-2 gap-3">
            <label className="block space-y-1 text-caption">
              {t(($) => $.qoder.concurrency)}
              <Input
                type="number"
                inputMode="numeric"
                min={1}
                max={MAX_CONCURRENCY}
                value={maxConcurrency}
                disabled={save.isPending}
                onChange={(event) =>
                  setMaxConcurrency(event.target.valueAsNumber || 0)
                }
              />
            </label>
            <label className="block space-y-1 text-caption">
              {t(($) => $.qoder.run_timeout)}
              <Input
                type="number"
                inputMode="numeric"
                min={MIN_RUN_TIMEOUT}
                max={MAX_RUN_TIMEOUT}
                step={5}
                value={runTimeoutMinutes}
                disabled={save.isPending}
                onChange={(event) =>
                  setRunTimeoutMinutes(event.target.valueAsNumber || 0)
                }
              />
            </label>
          </div>
        </Disclosure>
      </div>

      <div className="flex items-start gap-2 rounded-lg bg-muted/50 p-3 text-caption text-muted-foreground">
        <ShieldCheck aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
        <p className="leading-relaxed">
          {t(($) => $.qoder.data_disclosure)}{" "}
          {t(($) => $.qoder.shared_credentials_hint)}
        </p>
      </div>

      {save.error ? (
        <p role="alert" className="text-caption text-destructive">
          {save.error.message}
        </p>
      ) : null}

      <div className="mt-auto space-y-2">
        <Button type="submit" className="w-full" disabled={!canSubmit}>
          {save.isPending ? (
            <Loader2 aria-hidden="true" className="size-4 animate-spin" />
          ) : (
            <Plug aria-hidden="true" className="size-4" />
          )}
          {save.isPending ? t(($) => $.qoder.working) : submitLabel}
        </Button>
      </div>
    </form>
  );
}

function Disclosure({
  icon: Icon,
  title,
  summary,
  open,
  onToggle,
  children,
}: {
  icon: typeof Settings2;
  title: string;
  summary: string;
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
}) {
  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className="flex w-full items-center gap-2 px-3 py-2.5 text-left transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
      >
        <Icon aria-hidden="true" className="size-3.5 text-muted-foreground" />
        <span className="flex-1 text-label font-medium">{title}</span>
        <span className="truncate text-caption tabular-nums text-muted-foreground">
          {summary}
        </span>
        <ChevronRight
          aria-hidden="true"
          className={cn(
            "size-3.5 shrink-0 text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
      </button>
      {open ? <div className="space-y-3 px-3 pb-3">{children}</div> : null}
    </div>
  );
}
