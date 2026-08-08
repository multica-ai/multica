"use client";

import {
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type ReactNode,
} from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertCircle,
  Blocks,
  Bot,
  CheckCircle2,
  FileJson,
  Loader2,
  Monitor,
  Upload,
  Users,
  WandSparkles,
} from "lucide-react";
import { ApiError } from "@multica/core/api";
import {
  PLATFORM_EXTENSION_MAX_IMPORT_BYTES,
  PlatformExtensionDocumentEncodingError,
  PlatformExtensionDocumentTooLargeError,
  extensionDetailOptions,
  extensionListOptions,
  useImportPlatformExtension,
  type PlatformExtensionMapping,
} from "@multica/core/extensions";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import {
  CollectionPageHeader,
  CollectionPageState,
} from "../layout/collection-page";
import { AppLink } from "../navigation";

export interface ExtensionsPageProps {
  /** Optional for direct embedding/tests; web and desktop resolve it from the workspace provider. */
  wsId?: string;
}

type ImportNotice =
  | { kind: "success"; idempotent: boolean }
  | { kind: "error"; reason: ImportErrorReason };

type ImportErrorReason =
  | "file"
  | "json"
  | "utf8"
  | "size"
  | "runtime"
  | "generic";

function isRuntimeUnavailable(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.status !== 409) return false;
  if (!error.body || typeof error.body !== "object") return false;
  return (
    (error.body as { code?: unknown }).code ===
    "PLATFORM_RUNTIME_UNAVAILABLE"
  );
}

function importErrorReason(error: unknown): ImportErrorReason {
  if (error instanceof PlatformExtensionDocumentTooLargeError) return "size";
  if (error instanceof PlatformExtensionDocumentEncodingError) return "utf8";
  if (isRuntimeUnavailable(error)) return "runtime";
  return "generic";
}

export function ExtensionsPage({ wsId }: ExtensionsPageProps = {}) {
  return wsId ? (
    <ExtensionsPageContent key={wsId} wsId={wsId} />
  ) : (
    <WorkspaceExtensionsPage />
  );
}

function WorkspaceExtensionsPage() {
  const wsId = useWorkspaceId();
  return <ExtensionsPageContent key={wsId} wsId={wsId} />;
}

function ExtensionsPageContent({ wsId }: { wsId: string }) {
  const { t } = useT("extensions");
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [selectedReleaseId, setSelectedReleaseId] = useState<string | null>(null);
  const [imported, setImported] = useState<PlatformExtensionMapping | null>(null);
  const [notice, setNotice] = useState<ImportNotice | null>(null);

  const listQuery = useQuery(extensionListOptions(wsId));
  const importMutation = useImportPlatformExtension(wsId);
  const releases = useMemo(() => {
    const listed = listQuery.data ?? [];
    if (!imported || listed.some((item) => item.release.id === imported.release.id)) {
      return listed;
    }
    return [imported, ...listed];
  }, [imported, listQuery.data]);
  const activeReleaseId = selectedReleaseId ?? releases[0]?.release.id ?? "";
  const detailQuery = useQuery(extensionDetailOptions(wsId, activeReleaseId));
  const importedIsActive = imported?.release.id === activeReleaseId;
  const activeMapping = importedIsActive ? imported : detailQuery.data;

  const setLocalError = (reason: ImportErrorReason) => {
    setNotice({ kind: "error", reason });
  };

  const handleFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const input = event.currentTarget;
    const file = input.files?.[0];
    if (!file) return;

    setNotice(null);
    if (!file.name.toLowerCase().endsWith(".json")) {
      setLocalError("file");
      input.value = "";
      return;
    }

    try {
      const bytes = new Uint8Array(await file.arrayBuffer());
      if (bytes.byteLength > PLATFORM_EXTENSION_MAX_IMPORT_BYTES) {
        setLocalError("size");
        return;
      }

      let text: string;
      try {
        text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
      } catch {
        setLocalError("utf8");
        return;
      }

      try {
        JSON.parse(text);
      } catch {
        setLocalError("json");
        return;
      }

      const result = await importMutation.mutateAsync(bytes);
      if (!result) {
        setLocalError("generic");
        return;
      }
      setImported(result);
      setSelectedReleaseId(result.release.id);
      setNotice({ kind: "success", idempotent: result.idempotent });
    } catch (error) {
      setLocalError(importErrorReason(error));
    } finally {
      input.value = "";
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader
        icon={Blocks}
        title={t(($) => $.page.title)}
        count={releases.length}
        description={t(($) => $.page.description)}
        actions={
          <>
            <input
              ref={fileInputRef}
              type="file"
              accept=".json,application/json"
              aria-label={t(($) => $.import.file_label)}
              className="sr-only"
              onChange={handleFile}
            />
            <Button
              type="button"
              size="sm"
              onClick={() => fileInputRef.current?.click()}
              disabled={importMutation.isPending}
            >
              {importMutation.isPending ? (
                <Loader2 className="animate-spin" />
              ) : (
                <Upload />
              )}
              {t(($) => $.page.import)}
            </Button>
          </>
        }
      />

      {notice ? <ImportFeedback notice={notice} /> : null}

      {importMutation.isPending ? (
        <div className="px-4 pt-4 sm:px-6" role="status">
          <Alert>
            <Loader2 className="animate-spin" />
            <AlertTitle>{t(($) => $.import.pending)}</AlertTitle>
          </Alert>
        </div>
      ) : null}

      {listQuery.isLoading && !imported ? (
        <CollectionPageState
          icon={Loader2}
          title={t(($) => $.states.loading)}
          role="status"
        />
      ) : listQuery.isError && !imported ? (
        <CollectionPageState
          icon={AlertCircle}
          title={t(($) => $.states.list_error_title)}
          description={t(($) => $.states.list_error_description)}
          tone="destructive"
          role="alert"
        />
      ) : releases.length === 0 ? (
        <CollectionPageState
          icon={FileJson}
          title={t(($) => $.states.empty_title)}
          description={t(($) => $.states.empty_description)}
        />
      ) : (
        <div className="grid min-h-0 min-w-0 flex-1 gap-4 overflow-y-auto p-4 sm:p-6 lg:grid-cols-[minmax(15rem,20rem)_minmax(0,1fr)]">
          <Card className="h-fit min-w-0 max-w-full" size="sm">
            <CardHeader>
              <CardTitle>{t(($) => $.detail.release)}</CardTitle>
            </CardHeader>
            <CardContent className="flex min-w-0 max-w-full flex-col gap-1">
              {releases.map((item) => {
                const active = item.release.id === activeReleaseId;
                return (
                  <button
                    key={item.release.id}
                    type="button"
                    aria-pressed={active}
                    onClick={() => {
                      setSelectedReleaseId(item.release.id);
                      if (item.release.id !== imported?.release.id) setImported(null);
                    }}
                    className={cn(
                      "min-w-0 max-w-full rounded-lg border px-3 py-2 text-left transition-colors",
                      active
                        ? "border-brand bg-brand/8"
                        : "border-transparent hover:bg-muted",
                    )}
                  >
                    <span className="block truncate font-medium">
                      {item.release.extension_key}
                    </span>
                    <span className="block min-w-0 break-words text-caption text-muted-foreground [overflow-wrap:anywhere]">
                      {t(($) => $.detail.version, {
                        version: item.release.version,
                      })}
                    </span>
                  </button>
                );
              })}
            </CardContent>
          </Card>

          {activeMapping ? (
            <ExtensionDetail mapping={activeMapping} />
          ) : detailQuery.isLoading ? (
            <CollectionPageState
              icon={Loader2}
              title={t(($) => $.states.detail_loading)}
              role="status"
              className="self-center"
            />
          ) : (
            <CollectionPageState
              icon={AlertCircle}
              title={t(($) => $.states.detail_error_title)}
              description={t(($) => $.states.detail_error_description)}
              tone="destructive"
              role="alert"
              className="self-center"
            />
          )}
        </div>
      )}
    </div>
  );
}

function ImportFeedback({ notice }: { notice: ImportNotice }) {
  const { t } = useT("extensions");
  if (notice.kind === "success") {
    return (
      <div className="px-4 pt-4 sm:px-6" aria-live="polite">
        <Alert>
          <CheckCircle2 />
          <AlertTitle>{t(($) => $.import.success)}</AlertTitle>
          {notice.idempotent ? (
            <AlertDescription>{t(($) => $.import.idempotent)}</AlertDescription>
          ) : null}
        </Alert>
      </div>
    );
  }

  const runtimeUnavailable = notice.reason === "runtime";
  const message = {
    file: t(($) => $.import.invalid_file),
    json: t(($) => $.import.invalid_json),
    utf8: t(($) => $.import.invalid_utf8),
    size: t(($) => $.import.too_large),
    runtime: t(($) => $.import.runtime_unavailable_title),
    generic: t(($) => $.import.generic_error),
  }[notice.reason];

  return (
    <div className="px-4 pt-4 sm:px-6" aria-live="assertive">
      <Alert variant="destructive">
        <AlertCircle />
        <AlertTitle>{message}</AlertTitle>
        {runtimeUnavailable ? (
          <AlertDescription>
            {t(($) => $.import.runtime_unavailable_description)}
          </AlertDescription>
        ) : null}
      </Alert>
    </div>
  );
}

function ExtensionDetail({ mapping }: { mapping: PlatformExtensionMapping }) {
  const { t } = useT("extensions");
  const paths = useWorkspacePaths();

  return (
    <div className="grid min-w-0 max-w-full content-start gap-4 md:grid-cols-2">
      <Card className="min-w-0 max-w-full">
        <CardHeader>
          <CardTitle>{t(($) => $.detail.release)}</CardTitle>
          <CardDescription className="min-w-0 break-words [overflow-wrap:anywhere]">
            {t(($) => $.detail.version, { version: mapping.release.version })}
          </CardDescription>
        </CardHeader>
        <CardContent className="break-all font-mono text-caption text-muted-foreground">
          {t(($) => $.detail.digest, { digest: mapping.release.digest })}
        </CardContent>
      </Card>

      <ResourceCard icon={Monitor} title={t(($) => $.detail.runtime)}>
        <AppLink
          href={paths.runtimeDetail(mapping.runtime.id)}
          className="font-medium hover:underline"
        >
          {t(($) => $.detail.platform_runtime)}
        </AppLink>
      </ResourceCard>

      <ResourceCard icon={Users} title={t(($) => $.detail.squad)}>
        <AppLink
          href={paths.squadDetail(mapping.squad.id)}
          className="block min-w-0 max-w-full break-words font-medium [overflow-wrap:anywhere] hover:underline"
        >
          {mapping.squad.name}
        </AppLink>
      </ResourceCard>

      <ResourceCard icon={Bot} title={t(($) => $.detail.agents)}>
        {mapping.agents.length === 0 ? (
          <span className="text-muted-foreground">{t(($) => $.detail.no_agents)}</span>
        ) : (
          <ul className="space-y-2">
            {mapping.agents.map((agent) => (
              <li key={agent.id} className="flex min-w-0 max-w-full items-center gap-2">
                <AppLink
                  href={paths.agentDetail(agent.id)}
                  className="block min-w-0 max-w-full break-words font-medium [overflow-wrap:anywhere] hover:underline"
                >
                  {agent.name}
                  {agent.leader ? ` — ${t(($) => $.detail.leader)}` : ""}
                </AppLink>
              </li>
            ))}
          </ul>
        )}
      </ResourceCard>

      <ResourceCard
        icon={WandSparkles}
        title={t(($) => $.detail.skills)}
        className="md:col-span-2"
      >
        {mapping.skills.length === 0 ? (
          <span className="text-muted-foreground">{t(($) => $.detail.no_skills)}</span>
        ) : (
          <div className="flex min-w-0 max-w-full flex-wrap gap-2">
            {mapping.skills.map((skill) => (
              <Badge
                key={skill.id}
                variant="outline"
                className="min-w-0 max-w-full truncate"
                render={
                  <AppLink
                    href={paths.skillDetail(skill.id)}
                    title={skill.name}
                  />
                }
              >
                {skill.name}
              </Badge>
            ))}
          </div>
        )}
      </ResourceCard>
    </div>
  );
}

function ResourceCard({
  icon: Icon,
  title,
  children,
  className,
}: {
  icon: typeof Monitor;
  title: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <Card className={cn("min-w-0 max-w-full", className)}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Icon className="size-4 text-muted-foreground" />
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="min-w-0 max-w-full">{children}</CardContent>
    </Card>
  );
}
