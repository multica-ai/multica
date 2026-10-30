"use client";

import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  ExternalLink,
  FolderGit2,
  KeyRound,
  Loader2,
  MoreHorizontal,
  Pencil,
  Plus,
  RefreshCw,
  ShieldCheck,
  Trash2,
} from "@multica/ui/icons";
import { toast } from "sonner";
import {
  MAX_CONCURRENCY,
  MAX_RUN_TIMEOUT,
  MIN_RUN_TIMEOUT,
  QODER_REGIONS,
  useCheckQoder,
  useQoderEnvironments,
  useSaveQoder,
  type QoderConnection,
  type QoderInput,
} from "@multica/core/runtimes/qoder";
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
import { DeferredTooltip } from "@multica/ui/components/common/deferred-tooltip";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import { openExternal } from "../../platform";
import { useT } from "../../i18n";
import { regionOf, RegionToggle } from "./qoder-connect-form";

/**
 * The saved connection as a save payload. A blank token and omitted
 * repositories keep what the server already stores.
 */
export function savedQoderInput(
  data: QoderConnection,
  fallbackName: string,
): QoderInput {
  return {
    name: data.name || fallbackName,
    baseUrl: data.baseUrl || QODER_REGIONS.global,
    environmentId: data.environmentId,
    qoderToken: "",
    maxConcurrency: data.maxConcurrency,
    runTimeoutMinutes: data.runTimeoutMinutes,
  };
}

/** Keeps every saved repository token except the ones in `change`. */
function repositoryTokens(
  repositories: string[],
  change: Record<string, string | null>,
): Record<string, string> {
  const tokens: Record<string, string> = {};
  for (const url of repositories) tokens[url] = "";
  for (const [url, token] of Object.entries(change)) {
    if (token === null) delete tokens[url];
    else tokens[url] = token;
  }
  return tokens;
}

/** Matches the server's repository key, so a re-added repository replaces its token. */
function repositoryKey(url: string): string {
  return url
    .trim()
    .replace(/\/+$/, "")
    .replace(/\.git$/, "")
    .toLowerCase();
}

function repositoryLabel(url: string): string {
  return url.replace(/^https:\/\/github\.com\//, "");
}

export function QoderSection({
  title,
  count,
  actions,
  footer,
  className,
  titleClassName,
  children,
}: {
  title: string;
  count?: number;
  actions?: ReactNode;
  footer?: ReactNode;
  className?: string;
  titleClassName?: string;
  children: ReactNode;
}) {
  return (
    <section className={cn("rounded-lg border bg-card", className)}>
      <div className="flex min-h-10 items-center gap-2 border-b py-1.5 pl-4 pr-2">
        <h2 className={cn("text-caption font-semibold", titleClassName)}>
          {title}
        </h2>
        {count !== undefined && (
          <span className="text-caption tabular-nums text-muted-foreground">
            {count}
          </span>
        )}
        {actions && (
          <div className="ml-auto flex items-center gap-1.5">{actions}</div>
        )}
      </div>
      {children}
      {footer && (
        <div className="border-t px-4 py-2.5 text-caption text-muted-foreground">
          {footer}
        </div>
      )}
    </section>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0 px-4 py-3">
      <dt className="text-micro uppercase tracking-wider text-muted-foreground">
        {label}
      </dt>
      <dd className="mt-1 truncate text-body">{children}</dd>
    </div>
  );
}

export function QoderConnectionCard({
  wsId,
  data,
  canManage,
}: {
  wsId: string;
  data: QoderConnection;
  canManage: boolean;
}) {
  const { t } = useT("runtimes");
  const check = useCheckQoder(wsId);
  const environments = useQoderEnvironments(wsId);
  const { mutate: loadEnvironments } = environments;
  const [editing, setEditing] = useState(false);

  // Names come from Qoder with the saved token, which only admins may use.
  useEffect(() => {
    if (!canManage || !data.hasToken) return;
    const controller = new AbortController();
    loadEnvironments({
      token: "",
      baseUrl: data.baseUrl,
      signal: controller.signal,
    });
    return () => controller.abort();
  }, [canManage, data.hasToken, data.baseUrl, data.environmentId, loadEnvironments]);

  const environmentName =
    environments.data?.find((item) => item.id === data.environmentId)?.name ??
    data.environmentId;
  const region =
    regionOf(data.baseUrl) === "cn"
      ? t(($) => $.qoder.region_cn)
      : t(($) => $.qoder.region_global);

  const test = () =>
    check.mutate(savedQoderInput(data, t(($) => $.qoder.cloud_title)), {
      onSuccess: () => toast.success(t(($) => $.qoder.check_passed)),
      onError: (err) =>
        toast.error(t(($) => $.qoder.check_failed), {
          description: err.message,
        }),
    });

  return (
    <QoderSection
      title={t(($) => $.qoder.page.connection_title)}
      actions={
        canManage ? (
          <>
            <Button
              size="xs"
              variant="outline"
              disabled={check.isPending}
              aria-busy={check.isPending}
              onClick={test}
            >
              {check.isPending && (
                <Loader2 aria-hidden="true" className="animate-spin" />
              )}
              {t(($) => $.qoder.check)}
            </Button>
            <Button size="xs" variant="outline" onClick={() => setEditing(true)}>
              <Pencil aria-hidden="true" />
              {t(($) => $.qoder.page.edit)}
            </Button>
          </>
        ) : null
      }
      footer={<DataDisclosure />}
    >
      <dl className="grid grid-cols-1 divide-y sm:grid-cols-3 sm:divide-x sm:divide-y-0">
        <Fact label={t(($) => $.qoder.region)}>{region}</Fact>
        <Fact label={t(($) => $.qoder.environment)}>{environmentName}</Fact>
        <Fact label={t(($) => $.qoder.connect.token)}>
          <span className="inline-flex items-center gap-1.5">
            <KeyRound
              aria-hidden="true"
              className="size-3.5 text-muted-foreground"
            />
            {data.hasToken ? t(($) => $.qoder.page.token_saved) : "—"}
          </span>
        </Fact>
      </dl>
      {editing && (
        <EditConnectionDialog
          wsId={wsId}
          data={data}
          onClose={() => setEditing(false)}
        />
      )}
    </QoderSection>
  );
}

function DataDisclosure() {
  const { t } = useT("runtimes");
  return (
    <Popover>
      <PopoverTrigger
        render={
          <button
            type="button"
            className="inline-flex items-center gap-1.5 rounded-sm transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        }
      >
        <ShieldCheck aria-hidden="true" className="size-3.5" />
        {t(($) => $.qoder.data_disclosure_title)}
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 text-caption leading-relaxed">
        {t(($) => $.qoder.data_disclosure)}
      </PopoverContent>
    </Popover>
  );
}

function EditConnectionDialog({
  wsId,
  data,
  onClose,
}: {
  wsId: string;
  data: QoderConnection;
  onClose: () => void;
}) {
  const { t } = useT("runtimes");
  const save = useSaveQoder(wsId);
  const check = useCheckQoder(wsId);
  const environments = useQoderEnvironments(wsId);
  const { mutate: loadEnvironments, reset: resetEnvironments } = environments;
  const savedRegion = regionOf(data.baseUrl);
  const [name, setName] = useState(
    data.name || t(($) => $.qoder.cloud_title),
  );
  const [region, setRegion] = useState(savedRegion);
  const [token, setToken] = useState("");
  const [environmentId, setEnvironmentId] = useState(data.environmentId);

  const baseUrl = QODER_REGIONS[region];
  const trimmedToken = token.trim();
  // A token only works on the site that issued it.
  const needsToken = region !== savedRegion || !data.hasToken;
  const canLoad = !!trimmedToken || !needsToken;
  const busy = save.isPending || check.isPending;

  const load = useCallback(
    (signal?: AbortSignal) =>
      loadEnvironments(
        { token: trimmedToken, baseUrl, signal },
        {
          onSuccess: (items) => {
            if (signal?.aborted) return;
            setEnvironmentId((current) =>
              items.some((item) => item.id === current)
                ? current
                : items.length === 1
                  ? items[0]!.id
                  : "",
            );
          },
        },
      ),
    [loadEnvironments, trimmedToken, baseUrl],
  );

  useEffect(() => {
    resetEnvironments();
    if (!canLoad) return;
    const controller = new AbortController();
    const timer = setTimeout(
      () => load(controller.signal),
      trimmedToken ? 600 : 0,
    );
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [canLoad, load, resetEnvironments, trimmedToken]);

  const environmentValid =
    environments.data?.some((item) => item.id === environmentId) === true;
  const canSubmit =
    environmentValid && !!name.trim() && canLoad && !busy;

  const submit = async () => {
    if (!canSubmit) return;
    const input: QoderInput = {
      ...savedQoderInput(data, name.trim()),
      name: name.trim(),
      baseUrl,
      environmentId,
      qoderToken: trimmedToken,
    };
    try {
      await check.mutateAsync(input);
      await save.mutateAsync(input);
      toast.success(t(($) => $.qoder.saved));
      onClose();
    } catch {
      /* The error is rendered in the dialog. */
    }
  };

  const error = check.error ?? save.error;

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.qoder.page.edit_connection_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.qoder.page.edit_connection_hint)}
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <label className="block space-y-1.5">
            <span className="block text-caption font-medium">
              {t(($) => $.qoder.name)}
            </span>
            <Input
              value={name}
              required
              disabled={busy}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <div className="space-y-1.5">
            <p id="qoder-edit-region" className="text-caption font-medium">
              {t(($) => $.qoder.region)}
            </p>
            <RegionToggle
              value={region}
              onChange={(next) => {
                setRegion(next);
                check.reset();
                save.reset();
              }}
              disabled={busy}
              labelledBy="qoder-edit-region"
            />
          </div>
          <label className="block space-y-1.5">
            <span className="block text-caption font-medium">
              {t(($) => $.qoder.connect.token)}
            </span>
            <Input
              type="password"
              autoComplete="new-password"
              value={token}
              required={needsToken}
              placeholder={
                needsToken ? "pt-…" : t(($) => $.qoder.keep_secret)
              }
              disabled={busy}
              className="font-mono"
              onChange={(event) => {
                setToken(event.target.value);
                check.reset();
                save.reset();
              }}
            />
            {region !== savedRegion && (
              <span className="block text-caption text-muted-foreground">
                {t(($) => $.qoder.region_hint)}
              </span>
            )}
          </label>
          <div className="space-y-1.5">
            <p id="qoder-edit-environment" className="text-caption font-medium">
              {t(($) => $.qoder.environment)}
            </p>
            <div className="flex items-center gap-2">
              <Select
                items={
                  environments.data?.map((item) => ({
                    value: item.id,
                    label: item.name,
                  })) ?? []
                }
                value={environmentId || null}
                onValueChange={(value) => {
                  setEnvironmentId(value ?? "");
                  check.reset();
                  save.reset();
                }}
                disabled={busy || !environments.data?.length}
              >
                <SelectTrigger
                  className="min-w-0 flex-1"
                  aria-labelledby="qoder-edit-environment"
                >
                  <SelectValue
                    placeholder={t(($) => $.qoder.select_environment)}
                  />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {environments.data?.map((item) => (
                    <SelectItem key={item.id} value={item.id}>
                      {item.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <IconButton
                type="button"
                size="icon"
                variant="outline"
                label={t(($) => $.qoder.refresh_catalog)}
                disabled={busy || !canLoad || environments.isPending}
                onClick={() => load()}
              >
                <RefreshCw
                  aria-hidden="true"
                  className={cn(environments.isPending && "animate-spin")}
                />
              </IconButton>
            </div>
            {!canLoad ? (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.qoder.connect.environment_waiting)}
              </p>
            ) : environments.isError ? (
              <p role="alert" className="text-caption text-destructive">
                {environments.error.message}
              </p>
            ) : environments.isSuccess && !environments.data.length ? (
              <p role="status" className="text-caption text-muted-foreground">
                {t(($) => $.qoder.no_environments)}
              </p>
            ) : null}
          </div>
          {error && (
            <p role="alert" className="text-caption text-destructive">
              {error.message}
            </p>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={onClose}
            >
              {t(($) => $.qoder.cancel)}
            </Button>
            <Button type="submit" disabled={!canSubmit} aria-busy={busy}>
              {busy && <Loader2 aria-hidden="true" className="animate-spin" />}
              {busy
                ? t(($) => $.qoder.checking)
                : t(($) => $.qoder.page.test_and_save)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function QoderRepositoriesCard({
  wsId,
  data,
  canManage,
}: {
  wsId: string;
  data: QoderConnection;
  canManage: boolean;
}) {
  const { t } = useT("runtimes");
  const [editing, setEditing] = useState<{ url: string | null } | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const repositories = data.repositories;

  return (
    <QoderSection
      title={t(($) => $.qoder.repositories)}
      count={repositories.length || undefined}
      actions={
        canManage ? (
          <Button
            size="xs"
            variant="outline"
            onClick={() => setEditing({ url: null })}
          >
            <Plus aria-hidden="true" />
            {t(($) => $.qoder.add_repository)}
          </Button>
        ) : null
      }
      footer={
        repositories.length > 0
          ? t(($) => $.qoder.page.repositories_hint)
          : undefined
      }
    >
      {repositories.length === 0 ? (
        <p className="px-4 py-6 text-center text-caption text-muted-foreground">
          {t(($) => $.qoder.page.repositories_hint)}
        </p>
      ) : (
        <ul className="divide-y">
          {repositories.map((url) => (
            <li key={url} className="flex items-center gap-3 py-2 pl-4 pr-2">
              <span className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-background text-muted-foreground">
                <FolderGit2 aria-hidden="true" className="size-3.5" />
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-label font-medium">
                  {repositoryLabel(url)}
                </p>
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.qoder.page.repo_token_saved)}
                </p>
              </div>
              {canManage && (
                <DropdownMenu>
                  <DeferredTooltip
                    content={t(($) => $.qoder.page.repo_actions, {
                      repo: repositoryLabel(url),
                    })}
                    trigger={
                      <DropdownMenuTrigger
                        render={
                          <Button
                            size="icon-sm"
                            variant="ghost"
                            aria-label={t(($) => $.qoder.page.repo_actions, {
                              repo: repositoryLabel(url),
                            })}
                            className="text-muted-foreground"
                          >
                            <MoreHorizontal aria-hidden="true" />
                          </Button>
                        }
                      />
                    }
                  />
                  <DropdownMenuContent align="end" className="w-44">
                    <DropdownMenuItem onClick={() => setEditing({ url })}>
                      <KeyRound aria-hidden="true" className="size-3.5" />
                      {t(($) => $.qoder.page.replace_token)}
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={() => openExternal(url)}>
                      <ExternalLink aria-hidden="true" className="size-3.5" />
                      {t(($) => $.qoder.page.open_on_github)}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      variant="destructive"
                      onClick={() => setRemoving(url)}
                    >
                      <Trash2 aria-hidden="true" className="size-3.5" />
                      {t(($) => $.qoder.remove)}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </li>
          ))}
        </ul>
      )}
      {editing && (
        <RepositoryDialog
          wsId={wsId}
          data={data}
          url={editing.url}
          onClose={() => setEditing(null)}
        />
      )}
      {removing && (
        <RemoveRepositoryDialog
          wsId={wsId}
          data={data}
          url={removing}
          onClose={() => setRemoving(null)}
        />
      )}
    </QoderSection>
  );
}

/** Adds a repository, or replaces the token of `url` when given. */
function RepositoryDialog({
  wsId,
  data,
  url: fixedUrl,
  onClose,
}: {
  wsId: string;
  data: QoderConnection;
  url: string | null;
  onClose: () => void;
}) {
  const { t } = useT("runtimes");
  const save = useSaveQoder(wsId);
  const [url, setUrl] = useState(fixedUrl ?? "");
  const [token, setToken] = useState("");
  const canSubmit = !!url.trim() && !!token.trim() && !save.isPending;

  const submit = async () => {
    if (!canSubmit) return;
    const key =
      data.repositories.find((saved) => saved === repositoryKey(url)) ??
      url.trim();
    try {
      await save.mutateAsync({
        ...savedQoderInput(data, t(($) => $.qoder.cloud_title)),
        githubTokens: repositoryTokens(data.repositories, {
          [key]: token.trim(),
        }),
      });
      onClose();
    } catch {
      /* The error is rendered in the dialog. */
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !save.isPending) onClose();
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {fixedUrl
              ? t(($) => $.qoder.page.replace_token_title, {
                  repo: repositoryLabel(fixedUrl),
                })
              : t(($) => $.qoder.add_repository)}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.qoder.page.repositories_hint)}
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          {!fixedUrl && (
            <label className="block space-y-1.5">
              <span className="block text-caption font-medium">
                {t(($) => $.qoder.github_url)}
              </span>
              <Input
                type="url"
                required
                value={url}
                placeholder="https://github.com/org/repo"
                disabled={save.isPending}
                onChange={(event) => {
                  setUrl(event.target.value);
                  save.reset();
                }}
              />
            </label>
          )}
          <label className="block space-y-1.5">
            <span className="block text-caption font-medium">
              {t(($) => $.qoder.github_token)}
            </span>
            <Input
              type="password"
              autoComplete="new-password"
              required
              value={token}
              disabled={save.isPending}
              className="font-mono"
              onChange={(event) => {
                setToken(event.target.value);
                save.reset();
              }}
            />
          </label>
          {save.error && (
            <p role="alert" className="text-caption text-destructive">
              {save.error.message}
            </p>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={save.isPending}
              onClick={onClose}
            >
              {t(($) => $.qoder.cancel)}
            </Button>
            <Button
              type="submit"
              disabled={!canSubmit}
              aria-busy={save.isPending}
            >
              {save.isPending && (
                <Loader2 aria-hidden="true" className="animate-spin" />
              )}
              {t(($) => $.qoder.page.save)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RemoveRepositoryDialog({
  wsId,
  data,
  url,
  onClose,
}: {
  wsId: string;
  data: QoderConnection;
  url: string;
  onClose: () => void;
}) {
  const { t } = useT("runtimes");
  const save = useSaveQoder(wsId);
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !save.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t(($) => $.qoder.page.remove_repository_title, {
              repo: repositoryLabel(url),
            })}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.qoder.page.remove_repository_description)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={save.isPending}>
            {t(($) => $.qoder.cancel)}
          </AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={save.isPending}
            aria-busy={save.isPending}
            onClick={() =>
              save.mutate(
                {
                  ...savedQoderInput(data, t(($) => $.qoder.cloud_title)),
                  githubTokens: repositoryTokens(data.repositories, {
                    [url]: null,
                  }),
                },
                {
                  onSuccess: onClose,
                  onError: (err) => toast.error(err.message),
                },
              )
            }
          >
            {t(($) => $.qoder.remove)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function QoderLimitsCard({
  wsId,
  data,
  canManage,
}: {
  wsId: string;
  data: QoderConnection;
  canManage: boolean;
}) {
  const { t } = useT("runtimes");
  const [editing, setEditing] = useState(false);
  return (
    <QoderSection
      title={t(($) => $.qoder.execution)}
      actions={
        canManage ? (
          <IconButton
            label={t(($) => $.qoder.page.edit_limits)}
            className="text-muted-foreground"
            onClick={() => setEditing(true)}
          >
            <Pencil aria-hidden="true" />
          </IconButton>
        ) : null
      }
    >
      <dl className="divide-y text-body">
        <div className="flex items-center gap-3 px-4 py-2.5">
          <dt className="flex-1 text-muted-foreground">
            {t(($) => $.qoder.concurrency)}
          </dt>
          <dd className="tabular-nums">{data.maxConcurrency}</dd>
        </div>
        <div className="flex items-center gap-3 px-4 py-2.5">
          <dt className="flex-1 text-muted-foreground">
            {t(($) => $.qoder.page.run_timeout)}
          </dt>
          <dd className="tabular-nums">
            {t(($) => $.qoder.page.minutes, { count: data.runTimeoutMinutes })}
          </dd>
        </div>
      </dl>
      {editing && (
        <LimitsDialog wsId={wsId} data={data} onClose={() => setEditing(false)} />
      )}
    </QoderSection>
  );
}

function LimitsDialog({
  wsId,
  data,
  onClose,
}: {
  wsId: string;
  data: QoderConnection;
  onClose: () => void;
}) {
  const { t } = useT("runtimes");
  const save = useSaveQoder(wsId);
  const [maxConcurrency, setMaxConcurrency] = useState(data.maxConcurrency);
  const [runTimeoutMinutes, setRunTimeoutMinutes] = useState(
    data.runTimeoutMinutes,
  );
  const valid =
    maxConcurrency >= 1 &&
    maxConcurrency <= MAX_CONCURRENCY &&
    runTimeoutMinutes >= MIN_RUN_TIMEOUT &&
    runTimeoutMinutes <= MAX_RUN_TIMEOUT;

  const submit = async () => {
    if (!valid || save.isPending) return;
    try {
      await save.mutateAsync({
        ...savedQoderInput(data, t(($) => $.qoder.cloud_title)),
        maxConcurrency,
        runTimeoutMinutes,
      });
      onClose();
    } catch {
      /* The error is rendered in the dialog. */
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !save.isPending) onClose();
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.qoder.page.edit_limits)}</DialogTitle>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <label className="block space-y-1.5">
            <span className="block text-caption font-medium">
              {t(($) => $.qoder.concurrency)}
            </span>
            <Input
              type="number"
              inputMode="numeric"
              min={1}
              max={MAX_CONCURRENCY}
              step={1}
              required
              value={maxConcurrency}
              disabled={save.isPending}
              onChange={(event) =>
                setMaxConcurrency(event.target.valueAsNumber || 0)
              }
            />
            <span className="block text-caption text-muted-foreground">
              {t(($) => $.qoder.concurrency_hint)}
            </span>
          </label>
          <label className="block space-y-1.5">
            <span className="block text-caption font-medium">
              {t(($) => $.qoder.run_timeout)}
            </span>
            <Input
              type="number"
              inputMode="numeric"
              min={MIN_RUN_TIMEOUT}
              max={MAX_RUN_TIMEOUT}
              step={5}
              required
              value={runTimeoutMinutes}
              disabled={save.isPending}
              onChange={(event) =>
                setRunTimeoutMinutes(event.target.valueAsNumber || 0)
              }
            />
            <span className="block text-caption text-muted-foreground">
              {t(($) => $.qoder.run_timeout_hint)}
            </span>
          </label>
          {save.error && (
            <p role="alert" className="text-caption text-destructive">
              {save.error.message}
            </p>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={save.isPending}
              onClick={onClose}
            >
              {t(($) => $.qoder.cancel)}
            </Button>
            <Button
              type="submit"
              disabled={!valid || save.isPending}
              aria-busy={save.isPending}
            >
              {save.isPending && (
                <Loader2 aria-hidden="true" className="animate-spin" />
              )}
              {t(($) => $.qoder.page.save)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
