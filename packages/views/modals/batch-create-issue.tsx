"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import type { ReactNode, UIEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Download, FileJson, Upload, X as XIcon } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import {
  BATCH_ISSUE_TEMPLATE_FILENAME,
  createBatchIssueTemplateJSON,
  parseBatchCreateIssuesJSON,
} from "@multica/core/issues/batch-create-json";
import { useBatchCreateIssues } from "@multica/core/issues/mutations";
import { useCreateModeStore } from "@multica/core/issues/stores/create-mode-store";
import type { CreateMode } from "@multica/core/issues/stores/create-mode-store";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { useQuickCreateStore } from "@multica/core/issues/stores/quick-create-store";
import type {
  Agent,
  BatchCreateIssueRowError,
  BatchCreateIssueValidationRow,
  BatchCreateIssuesRequest,
  BatchCreateIssuesResponse,
  MemberWithUser,
  Project,
} from "@multica/core/types";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { DialogTitle } from "@multica/ui/components/ui/dialog";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { CreateModeSelector } from "./create-mode-selector";
import { StatusIcon } from "../issues/components/status-icon";
import { useT } from "../i18n";

export const batchDialogContentClass = cn(
  "p-0 gap-0 flex flex-col overflow-hidden",
  "!top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2",
  "!max-w-7xl sm:!max-w-7xl !w-[calc(100vw-2rem)] !h-[min(86vh,820px)]",
  "!transition-all !duration-300 !ease-out",
);

export function BatchCreateIssuePanel({
  onClose,
  mode = "batch",
  onSwitchMode,
  data,
}: {
  onClose: () => void;
  mode?: CreateMode;
  onSwitchMode?: (mode: CreateMode, carry?: Record<string, unknown> | null) => void;
  data?: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const wsId = useWorkspaceId();
  const workspaceName = useCurrentWorkspace()?.name;
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const currentUserId = useAuthStore((s) => s.user?.id);
  const lastAgentId = useQuickCreateStore((s) => s.lastAgentId);
  const lastAssigneeType = useIssueDraftStore((s) => s.lastAssigneeType);
  const lastAssigneeId = useIssueDraftStore((s) => s.lastAssigneeId);
  const lastProjectId = useIssueDraftStore((s) => s.lastProjectId);
  const setLastMode = useCreateModeStore((s) => s.setLastMode);
  const batchCreate = useBatchCreateIssues();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [jsonText, setJsonText] = useState("");
  const [serverValidation, setServerValidation] = useState<BatchCreateIssuesResponse | null>(null);
  const [validatedRequest, setValidatedRequest] = useState<BatchCreateIssuesRequest | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [dragOver, setDragOver] = useState(false);

  const parseResult = useMemo(
    () => (jsonText.trim() ? parseBatchCreateIssuesJSON(jsonText) : null),
    [jsonText],
  );

  const projectCarry = typeof data?.project_id === "string" ? { project_id: data.project_id } : null;
  const previewRows = serverValidation?.rows ?? clientPreviewRows(parseResult);
  const errors = parseResult && !parseResult.ok ? parseResult.errors : serverValidation?.errors ?? [];
  const rowCount = serverValidation?.row_count ?? previewRows.length;
  const agentTaskCount = serverValidation?.agent_task_count ?? previewRows.filter((row) => row.will_enqueue_agent_task).length;
  const agentsById = useMemo(() => new Map(agents.map((agent) => [agent.id, agent])), [agents]);
  const membersByUserId = useMemo(() => new Map(members.map((member) => [member.user_id, member])), [members]);
  const projectsById = useMemo(() => new Map(projects.map((project) => [project.id, project])), [projects]);
  const templateAgentId = useMemo(() => {
    const rememberedAgentId = lastAssigneeType === "agent" ? lastAssigneeId : lastAgentId;
    if (rememberedAgentId && agents.some((agent) => agent.id === rememberedAgentId && !agent.archived_at)) {
      return rememberedAgentId;
    }
    return agents.find((agent) => !agent.archived_at)?.id ?? null;
  }, [agents, lastAgentId, lastAssigneeId, lastAssigneeType]);
  const templateProjectId = useMemo(() => {
    const carriedProjectId = typeof data?.project_id === "string" ? data.project_id : null;
    if (carriedProjectId && projects.some((project) => project.id === carriedProjectId)) {
      return carriedProjectId;
    }
    if (lastProjectId && projects.some((project) => project.id === lastProjectId)) {
      return lastProjectId;
    }
    return projects[0]?.id ?? null;
  }, [data?.project_id, lastProjectId, projects]);

  const handleModeSelect = (next: CreateMode) => {
    if (next === mode) return;
    setLastMode(next);
    onSwitchMode?.(next, projectCarry);
  };

  const resetServerState = () => {
    setServerValidation(null);
    setValidatedRequest(null);
  };

  const setInputText = (text: string) => {
    setJsonText(text);
    resetServerState();
  };

  const handleDownloadTemplate = () => {
    const blob = new Blob([
      createBatchIssueTemplateJSON({
        memberAssigneeId: currentUserId,
        agentAssigneeId: templateAgentId,
        projectId: templateProjectId,
      }),
    ], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = BATCH_ISSUE_TEMPLATE_FILENAME;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  };

  const readFile = async (file: File) => {
    if (!file.name.endsWith(".json") && file.type !== "application/json") {
      toast.error(t(($) => $.create_issue.batch.toast_upload_json));
      return;
    }
    setInputText(await file.text());
  };

  const handleValidate = async () => {
    if (!parseResult?.ok) {
      setServerValidation(null);
      return;
    }
    const request = parseResult.request;
    setValidatedRequest(request);
    try {
      const resp = await batchCreate.mutateAsync({
        ...request,
        validate_only: true,
      });
      setServerValidation(resp);
      if (resp.agent_task_count > 0) {
        setConfirmOpen(true);
        return;
      }
      await handleCreate(request);
    } catch (error) {
      const body = error instanceof ApiError ? error.body : undefined;
      if (isBatchCreateResponse(body)) {
        setServerValidation(body);
        return;
      }
      toast.error(t(($) => $.create_issue.batch.toast_validate_failed));
    }
  };

  const handleCreate = async (request: BatchCreateIssuesRequest) => {
    try {
      const resp = await batchCreate.mutateAsync({
        ...request,
        confirm_batch_create: true,
      });
      const warningCount = resp.warnings?.length ?? 0;
      if (warningCount > 0) {
        toast.warning(t(($) => $.create_issue.batch.toast_created_with_warnings, {
          count: resp.created ?? resp.row_count,
          warningCount,
        }));
      } else {
        toast.success(t(($) => $.create_issue.batch.toast_created, {
          count: resp.created ?? resp.row_count,
        }));
      }
      setLastMode("batch");
      setConfirmOpen(false);
      onClose();
    } catch (error) {
      const body = error instanceof ApiError ? error.body : undefined;
      if (isBatchCreateResponse(body)) {
        setServerValidation(body);
        setConfirmOpen(false);
        return;
      }
      toast.error(t(($) => $.create_issue.batch.toast_create_failed));
    }
  };

  const handleConfirm = async () => {
    if (!validatedRequest) return;
    await handleCreate(validatedRequest);
  };

  return (
    <>
      <DialogTitle className="sr-only">{t(($) => $.create_issue.sr_batch)}</DialogTitle>
      <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
        <div className="flex items-center gap-1.5 text-xs">
          <span className="text-muted-foreground">{workspaceName}</span>
          <span className="text-muted-foreground/50">/</span>
          <span className="font-medium">{t(($) => $.create_issue.batch_breadcrumb)}</span>
        </div>
        <div className="flex items-center gap-1">
          <CreateModeSelector mode={mode} onSelect={handleModeSelect} className="mr-1" />
          <button
            type="button"
            onClick={onClose}
            title={t(($) => $.common.close)}
            aria-label={t(($) => $.common.close)}
            className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
          >
            <XIcon className="size-4" />
          </button>
        </div>
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-0 border-t md:grid-cols-[minmax(380px,0.85fr)_minmax(0,1.35fr)]">
        <div className="flex min-h-0 flex-col border-b md:border-b-0 md:border-r">
          <div className="flex h-14 items-center justify-between gap-2 border-b px-5">
            <div className="flex items-center gap-2 text-sm font-medium">
              <FileJson className="size-4 text-muted-foreground" />
              JSON
            </div>
            <div className="flex items-center gap-1">
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={handleDownloadTemplate}
                aria-label={t(($) => $.create_issue.batch.download_template)}
                title={t(($) => $.create_issue.batch.download_template)}
              >
                <Download className="size-4" />
              </Button>
            </div>
          </div>

          <div
            className={cn(
              "mx-5 mb-4 mt-5 flex h-28 shrink-0 cursor-pointer flex-col items-center justify-center rounded-md border border-dashed text-center transition-colors",
              dragOver ? "border-primary bg-primary/5" : "border-border bg-muted/20 hover:bg-muted/40",
            )}
            onClick={() => fileInputRef.current?.click()}
            onDragOver={(event) => {
              event.preventDefault();
              setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(event) => {
              event.preventDefault();
              setDragOver(false);
              const file = event.dataTransfer.files[0];
              if (file) void readFile(file);
            }}
            role="button"
            tabIndex={0}
            onKeyDown={(event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                fileInputRef.current?.click();
              }
            }}
          >
            <Upload className="mb-2 size-4 text-muted-foreground" />
            <div className="text-sm font-medium">{t(($) => $.create_issue.batch.upload_json)}</div>
            <div className="text-xs text-muted-foreground">{t(($) => $.create_issue.batch.upload_json_hint)}</div>
            <input
              ref={fileInputRef}
              aria-label={t(($) => $.create_issue.batch.upload_file_aria)}
              type="file"
              accept=".json,application/json"
              className="hidden"
              onChange={(event) => {
                const file = event.target.files?.[0];
                if (file) void readFile(file);
                event.currentTarget.value = "";
              }}
            />
          </div>

          <div className="flex min-h-0 flex-1 flex-col px-5 pb-5">
            <JsonEditor value={jsonText} onChange={setInputText} />
          </div>
        </div>

        <div className="flex min-h-0 flex-col">
          <div className="flex h-14 items-center justify-between gap-2 border-b px-5">
            <div className="text-sm font-medium">{t(($) => $.create_issue.batch.preview)}</div>
            <div className="text-xs text-muted-foreground">
              {rowCount > 0
                ? t(($) => $.create_issue.batch.row_count, { count: rowCount })
                : t(($) => $.create_issue.batch.no_rows)}
              {agentTaskCount > 0
                ? ` / ${t(($) => $.create_issue.batch.agent_run_count, { count: agentTaskCount })}`
                : ""}
            </div>
          </div>

          {errors.length > 0 ? <ValidationErrors errors={errors} /> : null}

          <div className="min-h-0 flex-1 overflow-auto">
            {previewRows.length > 0 ? (
              <Table className="min-w-[760px]">
                <TableHeader className="sticky top-0 z-10 bg-background">
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="w-16 px-4 text-xs text-muted-foreground">{t(($) => $.create_issue.batch.table_row)}</TableHead>
                    <TableHead className="min-w-[260px] text-xs text-muted-foreground">{t(($) => $.create_issue.batch.table_title)}</TableHead>
                    <TableHead className="w-36 text-xs text-muted-foreground">{t(($) => $.create_issue.batch.table_status)}</TableHead>
                    <TableHead className="w-52 text-xs text-muted-foreground">{t(($) => $.create_issue.batch.table_assignee)}</TableHead>
                    <TableHead className="w-44 text-xs text-muted-foreground">{t(($) => $.create_issue.batch.table_project)}</TableHead>
                    <TableHead
                      className="w-32 pr-4 text-xs text-muted-foreground"
                      title={t(($) => $.create_issue.batch.table_agent_run_tooltip)}
                    >
                      {t(($) => $.create_issue.batch.table_agent_run)}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {previewRows.slice(0, 100).map((row) => (
                    <TableRow key={row.row} className="h-11 hover:bg-accent/40">
                      <TableCell className="px-4 text-sm tabular-nums text-muted-foreground">{row.row}</TableCell>
                      <TableCell className="max-w-[340px] truncate text-sm font-medium">{row.title}</TableCell>
                      <TableCell>
                        <StatusPill status={row.status} />
                      </TableCell>
                      <TableCell>
                        <AssigneePill
                          row={row}
                          agentsById={agentsById}
                          membersByUserId={membersByUserId}
                        />
                      </TableCell>
                      <TableCell>
                        <ProjectPill projectId={row.project_id} projectsById={projectsById} />
                      </TableCell>
                      <TableCell className="pr-4">
                        <AgentRunBadge row={row} />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            ) : (
              <div className="flex h-full items-center justify-center px-4 text-sm text-muted-foreground">
                {t(($) => $.create_issue.batch.empty_state)}
              </div>
            )}
          </div>
        </div>
      </div>

      <div className="flex shrink-0 items-center justify-end gap-2 border-t px-4 py-3">
        <Button
          type="button"
          onClick={handleValidate}
          disabled={!parseResult?.ok || batchCreate.isPending}
        >
          {batchCreate.isPending
            ? t(($) => $.create_issue.batch.checking)
            : t(($) => $.create_issue.batch.create)}
        </Button>
      </div>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.create_issue.batch.confirm_title, {
                count: serverValidation?.row_count ?? 0,
              })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.create_issue.batch.confirm_description, {
                count: serverValidation?.agent_task_count ?? 0,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={batchCreate.isPending}>
              {t(($) => $.create_issue.batch.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirm} disabled={batchCreate.isPending}>
              {batchCreate.isPending
                ? t(($) => $.create_issue.batch.creating)
                : t(($) => $.create_issue.batch.create)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function ValidationErrors({ errors }: { errors: BatchCreateIssueRowError[] }) {
  const { t } = useT("modals");
  const hasInvalidJson = errors.some((error) => error.code === "invalid_json");

  return (
    <div className="max-h-44 shrink-0 overflow-y-auto border-b bg-destructive/5 px-5 py-3">
      <div className="flex items-start gap-2">
        <AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden="true" />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium text-destructive">
            {hasInvalidJson
              ? t(($) => $.create_issue.batch.invalid_json_title)
              : t(($) => $.create_issue.batch.validation_errors_title)}
          </div>
          {hasInvalidJson ? (
            <div className="mt-1 text-xs text-muted-foreground">
              {t(($) => $.create_issue.batch.invalid_json_description)}
            </div>
          ) : null}
          <div className="mt-2 space-y-1.5">
            {errors.map((error, index) => (
              <div key={`${error.row}-${error.field}-${error.code}-${index}`} className="min-w-0">
                <div className="text-[11px] font-medium uppercase text-destructive/80">
                  {formatErrorLocation(error)}
                </div>
                <div className="truncate text-xs text-destructive" title={error.message}>
                  {error.message}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function JsonEditor({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const highlightRef = useRef<HTMLPreElement>(null);
  const lineNumberRef = useRef<HTMLPreElement>(null);
  const lineNumbers = useMemo(() => buildLineNumbers(value), [value]);

  const handleScroll = useCallback((event: UIEvent<HTMLTextAreaElement>) => {
    if (!highlightRef.current) return;
    highlightRef.current.scrollTop = event.currentTarget.scrollTop;
    highlightRef.current.scrollLeft = event.currentTarget.scrollLeft;
    if (lineNumberRef.current) {
      lineNumberRef.current.scrollTop = event.currentTarget.scrollTop;
    }
  }, []);

  return (
    <div className="relative min-h-0 flex-1 overflow-hidden rounded-md border border-input bg-muted/20 transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50">
      <pre
        ref={lineNumberRef}
        aria-hidden="true"
        data-testid="json-line-numbers"
        className="pointer-events-none absolute inset-y-0 left-0 z-10 m-0 w-12 overflow-hidden border-r bg-muted/40 py-3 pr-2 text-right font-mono text-[13px] leading-5 text-muted-foreground/70"
      >
        {lineNumbers}
      </pre>
      <pre
        ref={highlightRef}
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 m-0 overflow-hidden py-3 pl-16 pr-3 font-mono text-[13px] leading-5 text-foreground"
      >
        <code className="block min-w-max whitespace-pre">
          {value ? renderJsonSyntax(value) : null}
        </code>
      </pre>
      <textarea
        aria-label="Batch issues JSON"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onScroll={handleScroll}
        placeholder='{"issues":[{"title":"Fix login empty state copy","status":"todo"}]}'
        spellCheck={false}
        wrap="off"
        className="absolute inset-0 h-full w-full resize-none overflow-auto border-0 bg-transparent py-3 pl-16 pr-3 font-mono text-[13px] leading-5 text-transparent caret-foreground outline-none placeholder:text-muted-foreground selection:bg-primary/20"
      />
    </div>
  );
}

function buildLineNumbers(text: string) {
  const count = Math.max(1, text.split("\n").length);
  return Array.from({ length: count }, (_, index) => index + 1).join("\n");
}

function renderJsonSyntax(text: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  const tokenRe = /("(?:\\.|[^"\\])*"(?=\s*:))|("(?:\\.|[^"\\])*")|\b(true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g;
  let cursor = 0;
  for (const match of text.matchAll(tokenRe)) {
    const index = match.index ?? 0;
    if (index > cursor) nodes.push(text.slice(cursor, index));
    const token = match[0];
    if (match[1]) {
      nodes.push(<span key={index} className="text-sky-700 dark:text-sky-300">{token}</span>);
    } else if (match[2]) {
      nodes.push(<span key={index} className="text-emerald-700 dark:text-emerald-300">{token}</span>);
    } else if (/^-?\d/.test(token)) {
      nodes.push(<span key={index} className="text-amber-700 dark:text-amber-300">{token}</span>);
    } else {
      nodes.push(<span key={index} className="text-rose-700 dark:text-rose-300">{token}</span>);
    }
    cursor = index + token.length;
  }
  if (cursor < text.length) nodes.push(text.slice(cursor));
  return nodes;
}

function formatErrorLocation(error: BatchCreateIssueRowError) {
  if (error.code === "invalid_json" || error.field === "json") {
    return "JSON syntax";
  }
  if (error.row > 0) {
    return `Row ${error.row} / ${error.field}`;
  }
  return error.field;
}

function StatusPill({ status }: { status: BatchCreateIssueValidationRow["status"] }) {
  return (
    <span className="inline-flex h-6 items-center gap-1.5 rounded-md bg-muted/60 px-2 text-xs font-medium text-foreground">
      <StatusIcon status={status} className="size-3.5 shrink-0" />
      {formatStatusLabel(status)}
    </span>
  );
}

function AssigneePill({
  row,
  agentsById,
  membersByUserId,
}: {
  row: BatchCreateIssueValidationRow;
  agentsById: Map<string, Agent>;
  membersByUserId: Map<string, MemberWithUser>;
}) {
  if (!row.assignee_id || !row.assignee_type) {
    return <span className="text-muted-foreground">-</span>;
  }
  if (row.assignee_type === "agent") {
    const agent = agentsById.get(row.assignee_id);
    return (
      <ResolvedIdPill
        label={agent?.name ?? "Unknown"}
        unresolved={!agent}
      />
    );
  }

  const member = membersByUserId.get(row.assignee_id);
  return (
    <ResolvedIdPill
      label={member?.name ?? "Unknown"}
      unresolved={!member}
    />
  );
}

function ProjectPill({
  projectId,
  projectsById,
}: {
  projectId: string | null;
  projectsById: Map<string, Project>;
}) {
  if (!projectId) {
    return <span className="text-muted-foreground">-</span>;
  }
  const project = projectsById.get(projectId);
  return (
    <ResolvedIdPill
      label={project?.title ?? "Unknown"}
      unresolved={!project}
    />
  );
}

function ResolvedIdPill({
  label,
  unresolved,
}: {
  label: string;
  unresolved?: boolean;
}) {
  return (
    <span
      className={cn(
        "inline-flex h-6 max-w-[200px] items-center gap-1.5 rounded-md bg-muted/60 px-2 text-xs",
        unresolved ? "text-muted-foreground" : "text-foreground",
      )}
      title={label}
    >
      <span className="truncate">{label}</span>
    </span>
  );
}

function AgentRunBadge({ row }: { row: BatchCreateIssueValidationRow }) {
  const { t } = useT("modals");

  if (row.will_enqueue_agent_task) {
    return (
      <span className="inline-flex h-6 items-center rounded-md bg-amber-500/10 px-2 text-xs font-medium text-amber-700 dark:text-amber-300">
        {t(($) => $.create_issue.batch.starts_now)}
      </span>
    );
  }
  if (row.assignee_type === "agent" && row.assignee_id) {
    return (
      <span className="inline-flex h-6 items-center rounded-md bg-muted/60 px-2 text-xs text-muted-foreground">
        {row.status === "backlog" ? "Backlog" : "Not ready"}
      </span>
    );
  }
  return <span className="text-muted-foreground">-</span>;
}

function formatStatusLabel(status: BatchCreateIssueValidationRow["status"]) {
  return status
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function clientPreviewRows(
  result: ReturnType<typeof parseBatchCreateIssuesJSON> | null,
): BatchCreateIssueValidationRow[] {
  if (!result?.ok) return [];
  return result.request.issues.map((issue, index) => ({
    row: index + 1,
    title: issue.title,
    status: issue.status ?? "todo",
    assignee_type: issue.assignee_type ?? null,
    assignee_id: issue.assignee_id ?? null,
    project_id: issue.project_id ?? null,
    will_enqueue_agent_task: issue.assignee_type === "agent" && (issue.status ?? "todo") !== "backlog",
  }));
}

function isBatchCreateResponse(value: unknown): value is BatchCreateIssuesResponse {
  return !!value && typeof value === "object" && "row_count" in value && "agent_task_count" in value;
}
