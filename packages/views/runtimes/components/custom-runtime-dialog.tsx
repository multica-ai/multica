"use client";

import { useMemo, useState } from "react";
import { Loader2, Terminal } from "lucide-react";
import type { AgentRuntime } from "@multica/core/types";
import {
  useAddCustomRuntime,
  useUpdateCustomRuntime,
} from "@multica/core/runtimes/mutations";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";

function normalizeCustomProvider(provider: string) {
  return provider
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function customAgentArgsFromText(argsText: string) {
  return argsText
    .split(/\r?\n/)
    .map((arg) => arg.trim())
    .filter(Boolean);
}

function customAgentArgsToText(args: string[]) {
  return args.join("\n");
}

function stringArrayFromUnknown(value: unknown) {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
}

export interface ManagedCustomRuntimeConfig {
  provider: string;
  name: string;
  path: string;
  args: string[];
  resumeArgs: string[];
  sessionIdRegex: string;
}

export function managedCustomRuntimeConfig(
  runtime: AgentRuntime,
): ManagedCustomRuntimeConfig | null {
  const metadata = runtime.metadata ?? {};
  if (metadata.custom_runtime_source !== "managed") return null;
  const raw = metadata.custom_runtime_config;
  if (!raw || typeof raw !== "object") return null;
  const config = raw as Record<string, unknown>;
  const provider =
    typeof config.provider === "string" ? config.provider : runtime.provider;
  const path = typeof config.path === "string" ? config.path : "";
  if (!provider || !path) return null;
  return {
    provider,
    name: typeof config.name === "string" ? config.name : runtime.name,
    path,
    args: stringArrayFromUnknown(config.args),
    resumeArgs: stringArrayFromUnknown(config.resume_args),
    sessionIdRegex:
      typeof config.session_id_regex === "string"
        ? config.session_id_regex
        : "",
  };
}

export function CustomRuntimeDialog({
  wsId,
  targetRuntime,
  editRuntime,
  onClose,
}: {
  wsId: string;
  targetRuntime: AgentRuntime;
  editRuntime?: AgentRuntime;
  onClose: () => void;
}) {
  const { t } = useT("runtimes");
  const initialConfig = useMemo(
    () => (editRuntime ? managedCustomRuntimeConfig(editRuntime) : null),
    [editRuntime],
  );
  const editing = Boolean(editRuntime && initialConfig);
  const [provider, setProvider] = useState(initialConfig?.provider ?? "");
  const [name, setName] = useState(initialConfig?.name ?? "");
  const [path, setPath] = useState(initialConfig?.path ?? "");
  const [argsText, setArgsText] = useState(
    customAgentArgsToText(initialConfig?.args ?? []),
  );
  const [resumeArgsText, setResumeArgsText] = useState(
    customAgentArgsToText(initialConfig?.resumeArgs ?? []),
  );
  const [sessionIdRegex, setSessionIdRegex] = useState(
    initialConfig?.sessionIdRegex ?? "",
  );
  const [error, setError] = useState<string | null>(null);
  const addCustomRuntime = useAddCustomRuntime(wsId);
  const updateCustomRuntime = useUpdateCustomRuntime(wsId);
  const normalizedProvider = normalizeCustomProvider(provider);
  const trimmedPath = path.trim();
  const pending = addCustomRuntime.isPending || updateCustomRuntime.isPending;
  const canSubmit = editing
    ? Boolean(editRuntime && trimmedPath)
    : Boolean(normalizedProvider && trimmedPath);

  const handleSubmit = async () => {
    if (!canSubmit || pending) return;
    setError(null);
    try {
      const payload = {
        name: name.trim() || normalizedProvider,
        path: trimmedPath,
        args: customAgentArgsFromText(argsText),
        resumeArgs: customAgentArgsFromText(resumeArgsText),
        sessionIdRegex: sessionIdRegex.trim(),
      };
      if (editing && editRuntime) {
        await updateCustomRuntime.mutateAsync({
          runtimeId: editRuntime.id,
          payload,
        });
      } else {
        await addCustomRuntime.mutateAsync({
          targetRuntimeId: targetRuntime.id,
          provider: normalizedProvider,
          ...payload,
        });
      }
      onClose();
    } catch (err) {
      const fallback = editing
        ? t(($) => $.connect.custom_update_failed)
        : t(($) => $.connect.custom_add_failed);
      setError(err instanceof Error ? err.message : fallback);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-lg">
        <DialogHeader className="px-6 pt-6 pb-2">
          <DialogTitle className="text-base text-balance">
            {editing
              ? t(($) => $.connect.custom_edit_title)
              : t(($) => $.connect.custom_tab)}
          </DialogTitle>
          <DialogDescription className="text-xs text-balance">
            {t(($) => $.connect.custom_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-6 py-4">
          <div className="flex items-start gap-3 rounded-lg border bg-muted/40 px-3 py-2.5 text-xs">
            <Terminal className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <div className="min-w-0">
              <p className="font-medium text-foreground">
                {t(($) => $.connect.custom_target_title)}
              </p>
              <p className="mt-0.5 truncate font-mono text-muted-foreground">
                {targetRuntime.name}
              </p>
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="custom-runtime-provider" className="text-xs">
                {t(($) => $.connect.custom_provider_label)}
              </Label>
              <Input
                id="custom-runtime-provider"
                value={provider}
                onChange={(e) => setProvider(e.target.value)}
                className="font-mono text-sm"
                spellCheck={false}
                disabled={editing}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="custom-runtime-name" className="text-xs">
                {t(($) => $.connect.custom_name_label)}
              </Label>
              <Input
                id="custom-runtime-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="custom-runtime-path" className="text-xs">
              {t(($) => $.connect.custom_path_label)}
            </Label>
            <Input
              id="custom-runtime-path"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              className="font-mono text-sm"
              spellCheck={false}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="custom-runtime-args" className="text-xs">
              {t(($) => $.connect.custom_args_label)}
            </Label>
            <Textarea
              id="custom-runtime-args"
              value={argsText}
              onChange={(e) => setArgsText(e.target.value)}
              className="min-h-20 resize-y font-mono text-sm"
              spellCheck={false}
            />
            <p className="text-[11px] leading-[1.55] text-muted-foreground">
              {t(($) => $.connect.custom_args_hint)}
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="custom-runtime-resume-args" className="text-xs">
              {t(($) => $.connect.custom_resume_args_label)}
            </Label>
            <Textarea
              id="custom-runtime-resume-args"
              value={resumeArgsText}
              onChange={(e) => setResumeArgsText(e.target.value)}
              className="min-h-20 resize-y font-mono text-sm"
              spellCheck={false}
            />
            <p className="text-[11px] leading-[1.55] text-muted-foreground">
              {t(($) => $.connect.custom_resume_args_hint)}
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="custom-runtime-session-regex" className="text-xs">
              {t(($) => $.connect.custom_session_regex_label)}
            </Label>
            <Input
              id="custom-runtime-session-regex"
              value={sessionIdRegex}
              onChange={(e) => setSessionIdRegex(e.target.value)}
              className="font-mono text-sm"
              spellCheck={false}
            />
            <p className="text-[11px] leading-[1.55] text-muted-foreground">
              {t(($) => $.connect.custom_session_regex_hint)}
            </p>
          </div>

          <div className="rounded-lg border bg-muted/40 px-3 py-2.5 text-xs leading-[1.55]">
            <p className="font-medium text-foreground">
              {t(($) => $.connect.custom_apply_title)}
            </p>
            <p className="mt-0.5 text-muted-foreground">
              {t(($) => $.connect.custom_apply_hint)}
            </p>
          </div>

          {error && (
            <p className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
              {error}
            </p>
          )}
        </div>

        <DialogFooter className="m-0 rounded-b-xl border-t bg-muted/30 px-6 py-3">
          <Button
            variant="outline"
            size="sm"
            onClick={onClose}
            disabled={pending}
          >
            {t(($) => $.connect.cancel)}
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={handleSubmit}
            disabled={!canSubmit || pending}
          >
            {pending && (
              <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
            )}
            {pending
              ? editing
                ? t(($) => $.connect.custom_updating_runtime)
                : t(($) => $.connect.custom_adding_runtime)
              : editing
                ? t(($) => $.connect.custom_update_runtime)
                : t(($) => $.connect.custom_add_runtime)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
