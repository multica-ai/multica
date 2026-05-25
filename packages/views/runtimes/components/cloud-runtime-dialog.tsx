"use client";

import { useId, useMemo, useState } from "react";
import type { FormEvent, HTMLAttributes } from "react";
import { useQuery } from "@tanstack/react-query";
import { Cloud, Loader2, RefreshCw, Rocket, Trash2 } from "lucide-react";
import { toast } from "sonner";
import type { CloudRuntimeNode } from "@multica/core/runtimes";
import {
  cloudRuntimeNodeListOptions,
  useCreateCloudRuntimeNode,
  useDeleteCloudRuntimeNode,
} from "@multica/core/runtimes";
import { useWorkspaceId } from "@multica/core/hooks";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const CLOUD_RUNTIME_INSTANCE_TYPES = ["t4g.medium", "t4g.large"] as const;
const DEFAULT_INSTANCE_TYPE = CLOUD_RUNTIME_INSTANCE_TYPES[0];
const DEFAULT_DISK_SIZE_GB = 20;

export function CloudRuntimeDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const idPrefix = `cloud-runtime-${useId().replace(/:/g, "")}`;
  const formId = `${idPrefix}-form`;
  const [name, setName] = useState("");
  const [instanceType, setInstanceType] = useState<string>(
    DEFAULT_INSTANCE_TYPE,
  );
  const [diskSizeGB, setDiskSizeGB] = useState(String(DEFAULT_DISK_SIZE_GB));

  const nodesQuery = useQuery(
    cloudRuntimeNodeListOptions(wsId, { limit: 20, offset: 0 }),
  );
  const createNode = useCreateCloudRuntimeNode(wsId);

  const sortedNodes = useMemo(
    () =>
      [...(nodesQuery.data ?? [])].sort(
        (a, b) =>
          new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
      ),
    [nodesQuery.data],
  );

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const diskSize = diskSizeGB.trim()
      ? Number(diskSizeGB.trim())
      : DEFAULT_DISK_SIZE_GB;
    if (!Number.isInteger(diskSize) || diskSize <= 0) {
      toast.error(t(($) => $.cloud_runtime.validation.disk_size_invalid));
      return;
    }

    try {
      await createNode.mutateAsync({
        instance_type: instanceType,
        name: valueOrUndefined(name),
        disk_size_gb: diskSize,
      });
      toast.success(t(($) => $.cloud_runtime.toast_created));
      setName("");
      setInstanceType(DEFAULT_INSTANCE_TYPE);
      setDiskSizeGB(String(DEFAULT_DISK_SIZE_GB));
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.cloud_runtime.toast_create_failed),
      );
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[88vh] flex-col gap-0 p-0 sm:max-w-3xl">
        <DialogHeader className="border-b px-6 py-5">
          <DialogTitle className="flex items-center gap-2 text-base">
            <Cloud className="h-4 w-4 text-muted-foreground" />
            {t(($) => $.cloud_runtime.title)}
          </DialogTitle>
          <DialogDescription className="text-xs">
            {t(($) => $.cloud_runtime.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
          <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(280px,0.82fr)]">
            <form id={formId} onSubmit={handleSubmit} className="space-y-4">
              <div>
                <h3 className="text-sm font-medium">
                  {t(($) => $.cloud_runtime.create_title)}
                </h3>
                <p className="mt-1 text-xs text-muted-foreground">
                  {t(($) => $.cloud_runtime.create_hint)}
                </p>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <LabeledInput
                  id={`${idPrefix}-name`}
                  label={t(($) => $.cloud_runtime.fields.name)}
                  value={name}
                  onChange={setName}
                  placeholder={t(($) => $.cloud_runtime.placeholders.name)}
                />
                <LabeledInput
                  id={`${idPrefix}-instance-type`}
                  label={t(($) => $.cloud_runtime.fields.instance_type)}
                  value={instanceType}
                  onChange={setInstanceType}
                  options={CLOUD_RUNTIME_INSTANCE_TYPES}
                />
                <LabeledInput
                  id={`${idPrefix}-disk-size`}
                  label={t(($) => $.cloud_runtime.fields.disk_size)}
                  value={diskSizeGB}
                  onChange={setDiskSizeGB}
                  placeholder={String(DEFAULT_DISK_SIZE_GB)}
                  type="number"
                  inputMode="numeric"
                />
              </div>
            </form>

            <section className="min-h-0 rounded-md border bg-muted/20">
              <div className="flex items-center justify-between border-b bg-background px-3 py-2.5">
                <h3 className="text-sm font-medium">
                  {t(($) => $.cloud_runtime.nodes_title)}
                </h3>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => void nodesQuery.refetch()}
                  disabled={nodesQuery.isFetching}
                  className="h-7 px-2"
                >
                  <RefreshCw
                    className={cn(
                      "h-3.5 w-3.5",
                      nodesQuery.isFetching && "animate-spin",
                    )}
                  />
                  {t(($) => $.cloud_runtime.refresh)}
                </Button>
              </div>

              {nodesQuery.isLoading ? (
                <div className="flex h-40 items-center justify-center">
                  <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                </div>
              ) : nodesQuery.isError ? (
                <div className="flex h-40 flex-col items-center justify-center px-5 text-center">
                  <p className="text-sm font-medium">
                    {t(($) => $.cloud_runtime.nodes_failed)}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {nodesQuery.error instanceof Error
                      ? nodesQuery.error.message
                      : t(($) => $.cloud_runtime.nodes_failed_hint)}
                  </p>
                </div>
              ) : sortedNodes.length === 0 ? (
                <div className="flex h-40 flex-col items-center justify-center px-5 text-center">
                  <Cloud className="h-7 w-7 text-muted-foreground/50" />
                  <p className="mt-3 text-sm font-medium">
                    {t(($) => $.cloud_runtime.nodes_empty)}
                  </p>
                </div>
              ) : (
                <div className="max-h-[410px] overflow-y-auto p-2">
                  <div className="space-y-2">
                    {sortedNodes.map((node) => (
                      <CloudRuntimeNodeRow key={node.id} node={node} wsId={wsId} />
                    ))}
                  </div>
                </div>
              )}
            </section>
          </div>
        </div>

        <DialogFooter className="m-0 border-t bg-muted/30 px-6 py-3">
          <Button type="button" variant="outline" size="sm" onClick={onClose}>
            {t(($) => $.cloud_runtime.cancel)}
          </Button>
          <Button
            type="submit"
            size="sm"
            form={formId}
            disabled={createNode.isPending}
          >
            {createNode.isPending ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Rocket className="h-3.5 w-3.5" />
            )}
            {createNode.isPending
              ? t(($) => $.cloud_runtime.creating)
              : t(($) => $.cloud_runtime.create)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function LabeledInput({
  id,
  label,
  value,
  onChange,
  placeholder,
  required,
  type = "text",
  inputMode,
  options,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  required?: boolean;
  type?: string;
  inputMode?: HTMLAttributes<HTMLInputElement>["inputMode"];
  options?: readonly string[];
}) {
  if (options) {
    return (
      <div className="space-y-1.5">
        <Label htmlFor={id} className="text-xs text-muted-foreground">
          {label}
        </Label>
        <Select value={value} onValueChange={(next) => onChange(next ?? value)}>
          <SelectTrigger id={id} className="h-9 w-full rounded-md text-sm">
            <SelectValue>
              {() => <span className="truncate">{value}</span>}
            </SelectValue>
          </SelectTrigger>
          <SelectContent align="start">
            {options.map((option) => (
              <SelectItem key={option} value={option}>
                {option}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    );
  }

  return (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {label}
      </Label>
      <Input
        id={id}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        required={required}
        type={type}
        inputMode={inputMode}
        className="h-9 text-sm"
      />
    </div>
  );
}

function CloudRuntimeNodeRow({ node, wsId }: { node: CloudRuntimeNode; wsId: string }) {
  const { t } = useT("runtimes");
  const deleteNode = useDeleteCloudRuntimeNode(wsId);
  const title =
    node.name.trim() ||
    node.instance_id.trim() ||
    t(($) => $.cloud_runtime.node_fallback_name);
  const created = formatDateTime(node.created_at);
  return (
    <div className="rounded-md border bg-background px-3 py-2.5">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-medium">{title}</span>
            <CloudRuntimeStatusBadge status={node.status} />
          </div>
          <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
            <span>{node.instance_type}</span>
            <span className="text-muted-foreground/40">/</span>
            <span>{node.region}</span>
            {created && (
              <>
                <span className="text-muted-foreground/40">/</span>
                <span>{created}</span>
              </>
            )}
          </div>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 w-7 shrink-0 p-0 text-muted-foreground hover:text-destructive"
          disabled={deleteNode.isPending}
          onClick={() => {
            if (!confirm(t(($) => $.cloud_runtime.delete_confirm))) return;
            deleteNode.mutate(node.instance_id, {
              onSuccess: () => toast.success(t(($) => $.cloud_runtime.toast_deleted)),
              onError: (err) =>
                toast.error(
                  err instanceof Error
                    ? err.message
                    : t(($) => $.cloud_runtime.toast_delete_failed),
                ),
            });
          }}
          aria-label={t(($) => $.cloud_runtime.delete)}
        >
          {deleteNode.isPending ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Trash2 className="h-3.5 w-3.5" />
          )}
        </Button>
      </div>
      {node.instance_id && (
        <div className="mt-2 truncate font-mono text-[11px] text-muted-foreground/80">
          {node.instance_id}
        </div>
      )}
    </div>
  );
}

function CloudRuntimeStatusBadge({ status }: { status: string }) {
  const normalized = status.toLowerCase();
  const active = new Set(["running", "success"]);
  const pending = new Set([
    "launching",
    "pending",
    "starting",
    "stopping",
    "rebooting",
    "terminating",
  ]);
  const failed = new Set(["failed", "terminated", "error"]);
  return (
    <Badge
      variant="secondary"
      className={cn(
        "h-5 rounded-md px-1.5 font-mono text-[10px]",
        active.has(normalized) && "bg-success/10 text-success",
        pending.has(normalized) && "bg-warning/10 text-warning",
        failed.has(normalized) && "bg-destructive/10 text-destructive",
      )}
    >
      {status || "unknown"}
    </Badge>
  );
}

function valueOrUndefined(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

function formatDateTime(value: string): string | null {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}
