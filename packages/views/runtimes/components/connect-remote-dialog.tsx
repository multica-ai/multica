"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Check, ChevronRight, Copy, Terminal } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import { useWSEvent } from "@multica/core/realtime";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { useConfigStore } from "@multica/core/config";
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
import { CODE_LIGATURE_CLASS } from "@multica/ui/lib/code-style";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

type Step = "instructions" | "success";
type ConnectMode = "computer" | "custom";

const INSTALL_CMD =
  "curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash";
const CLOUD_SERVER_URL = "https://api.multica.ai";
const CLOUD_APP_URL = "https://multica.ai";
const CUSTOM_PROMPT_PLACEHOLDER = "{{prompt}}";
const DEFAULT_CUSTOM_PROVIDER = "king";
const DEFAULT_CUSTOM_NAME = "Code King";
const DEFAULT_CUSTOM_PATH = "king";
const DEFAULT_CUSTOM_ARGS = `-p\n${CUSTOM_PROMPT_PLACEHOLDER}`;

function normalizeCommandURL(url: string | undefined) {
  return url?.trim().replace(/\/+$/, "") ?? "";
}

function daemonCommands(serverUrl: string | undefined, appUrl: string | undefined) {
  const normalizedServerUrl = normalizeCommandURL(serverUrl);
  const normalizedAppUrl = normalizeCommandURL(appUrl);
  if (normalizedServerUrl && normalizedAppUrl) {
    return {
      setupCmd: `multica setup self-host --server-url ${normalizedServerUrl} --app-url ${normalizedAppUrl}`,
      tokenCmd: `multica config set server_url ${normalizedServerUrl}
multica config set app_url ${normalizedAppUrl}
multica login --token <YOUR_TOKEN>
multica daemon start`,
    };
  }

  return {
    setupCmd: "multica setup",
    tokenCmd: `multica config set server_url ${CLOUD_SERVER_URL}
multica config set app_url ${CLOUD_APP_URL}
multica login --token <YOUR_TOKEN>
multica daemon start`,
  };
}

function normalizeCustomProvider(provider: string) {
  const cleaned = provider
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return cleaned || "custom";
}

function customAgentArgsFromText(argsText: string) {
  return argsText
    .split(/\r?\n/)
    .map((arg) => arg.trim())
    .filter(Boolean);
}

function shellSingleQuote(value: string) {
  return `'${value.replaceAll("'", "'\"'\"'")}'`;
}

function customAgentEnvJSON({
  provider,
  name,
  path,
  args,
}: {
  provider: string;
  name: string;
  path: string;
  args: string[];
}) {
  const normalizedProvider = normalizeCustomProvider(provider);
  return JSON.stringify([
    {
      provider: normalizedProvider,
      name: name.trim() || normalizedProvider,
      path: path.trim() || normalizedProvider,
      args,
    },
  ]);
}

function customAgentEnvCommand(envJSON: string) {
  return `export MULTICA_CUSTOM_AGENTS=${shellSingleQuote(envJSON)}
multica daemon restart`;
}

export function ConnectRemoteDialog({ onClose }: { onClose: () => void }) {
  const [step, setStep] = useState<Step>("instructions");
  const wsId = useWorkspaceId();
  const slug = useWorkspaceSlug();
  const qc = useQueryClient();
  const navigation = useNavigation();
  const newRuntimeIdRef = useRef<string | null>(null);

  // `multica setup` is one blocking command that handles config + login
  // + daemon start; the dialog passively listens for the resulting
  // `daemon:register` WS event and auto-advances to success.
  const handleDaemonRegister = useCallback(
    (payload: unknown) => {
      if (step !== "instructions") return;
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      const p = payload as Record<string, unknown> | null;
      if (p?.runtime_id && typeof p.runtime_id === "string") {
        newRuntimeIdRef.current = p.runtime_id;
      }
      setStep("success");
    },
    [step, qc, wsId],
  );
  useWSEvent("daemon:register", handleDaemonRegister);

  const handleGoToAgents = () => {
    onClose();
    if (slug) {
      navigation.push(paths.workspace(slug).agents());
    }
  };

  const handleGoToRuntime = () => {
    onClose();
    if (slug && newRuntimeIdRef.current) {
      navigation.push(
        paths.workspace(slug).runtimeDetail(newRuntimeIdRef.current),
      );
    }
  };

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-lg">
        {step === "instructions" && <InstructionsStep onClose={onClose} />}
        {step === "success" && (
          <SuccessStep
            onGoToAgents={handleGoToAgents}
            onGoToRuntime={
              newRuntimeIdRef.current ? handleGoToRuntime : undefined
            }
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Copy button + code row — mirrors onboarding/CliInstallInstructions
// ---------------------------------------------------------------------------

function CopyButton({ text, ariaLabel }: { text: string; ariaLabel: string }) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(t);
  }, [copied]);

  const handleCopy = () => {
    navigator.clipboard.writeText(text);
    setCopied(true);
  };

  return (
    <button
      type="button"
      onClick={handleCopy}
      aria-label={ariaLabel}
      className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      {copied ? (
        <Check className="h-3.5 w-3.5 text-success" aria-hidden />
      ) : (
        <Copy className="h-3.5 w-3.5" aria-hidden />
      )}
    </button>
  );
}

function CommandStep({
  n,
  label,
  cmd,
  copyAria,
}: {
  n: number;
  label: string;
  cmd: string;
  copyAria: string;
}) {
  return (
    <div>
      <p className="mb-1.5 text-xs font-medium text-foreground">
        {n}. {label}
      </p>
      <div className="flex items-start gap-2 rounded-lg bg-muted px-3 py-2.5 font-mono text-sm">
        <Terminal
          className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground"
          aria-hidden
        />
        <code
          className={cn(
            "min-w-0 flex-1 break-all whitespace-pre-wrap tabular-nums",
            CODE_LIGATURE_CLASS,
          )}
        >
          {cmd}
        </code>
        <CopyButton text={cmd} ariaLabel={copyAria} />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Step 1: Instructions
// ---------------------------------------------------------------------------

function InstructionsStep({ onClose }: { onClose: () => void }) {
  const { t } = useT("runtimes");
  const [mode, setMode] = useState<ConnectMode>("computer");
  const daemonServerUrl = useConfigStore((s) => s.daemonServerUrl);
  const daemonAppUrl = useConfigStore((s) => s.daemonAppUrl);
  const { setupCmd, tokenCmd } = daemonCommands(daemonServerUrl, daemonAppUrl);
  return (
    <>
      <DialogHeader className="px-6 pt-6 pb-2">
        <DialogTitle className="text-base text-balance">
          {t(($) => $.connect.title)}
        </DialogTitle>
        <DialogDescription className="text-xs text-balance">
          {t(($) => $.connect.description)}
        </DialogDescription>
        <div className="mt-3 inline-flex w-fit rounded-lg border bg-muted p-0.5">
          <Button
            type="button"
            size="sm"
            variant={mode === "computer" ? "secondary" : "ghost"}
            className="h-7 rounded-md px-2.5 text-xs"
            onClick={() => setMode("computer")}
          >
            {t(($) => $.connect.computer_tab)}
          </Button>
          <Button
            type="button"
            size="sm"
            variant={mode === "custom" ? "secondary" : "ghost"}
            className="h-7 rounded-md px-2.5 text-xs"
            onClick={() => setMode("custom")}
          >
            {t(($) => $.connect.custom_tab)}
          </Button>
        </div>
      </DialogHeader>

      <div className="min-h-0 flex-1 overflow-y-auto px-6 py-4">
        {mode === "computer" ? (
          <ComputerInstructions setupCmd={setupCmd} tokenCmd={tokenCmd} />
        ) : (
          <CustomRuntimeInstructions />
        )}
      </div>

      <DialogFooter className="m-0 rounded-b-xl border-t bg-muted/30 px-6 py-3">
        <Button variant="outline" size="sm" onClick={onClose}>
          {t(($) => $.connect.cancel)}
        </Button>
      </DialogFooter>
    </>
  );
}

function ComputerInstructions({
  setupCmd,
  tokenCmd,
}: {
  setupCmd: string;
  tokenCmd: string;
}) {
  const { t } = useT("runtimes");
  return (
    <div className="space-y-4">
      <CommandStep
        n={1}
        label={t(($) => $.connect.step1_label)}
        cmd={INSTALL_CMD}
        copyAria={t(($) => $.connect.copy_aria)}
      />

      <div>
        <CommandStep
          n={2}
          label={t(($) => $.connect.step2_label)}
          cmd={setupCmd}
          copyAria={t(($) => $.connect.copy_aria)}
        />
        <p className="mt-1.5 text-[11px] leading-[1.55] text-muted-foreground">
          {t(($) => $.connect.step2_hint)}
        </p>
      </div>

      <LiveListening />

      <TroubleshootingDetails tokenCmd={tokenCmd} />
    </div>
  );
}

function CustomRuntimeInstructions() {
  const { t } = useT("runtimes");
  const [provider, setProvider] = useState(DEFAULT_CUSTOM_PROVIDER);
  const [name, setName] = useState(DEFAULT_CUSTOM_NAME);
  const [path, setPath] = useState(DEFAULT_CUSTOM_PATH);
  const [argsText, setArgsText] = useState(DEFAULT_CUSTOM_ARGS);

  // Custom runtime V1 deliberately mirrors the daemon's environment JSON.
  // Web only helps build machine-local configuration here; the daemon still
  // owns executable lookup and argv execution because PATH and filesystem
  // access are properties of the runtime host, not workspace data.
  const customEnvJSON = useMemo(
    () =>
      customAgentEnvJSON({
        provider,
        name,
        path,
        args: customAgentArgsFromText(argsText),
      }),
    [argsText, name, path, provider],
  );
  const customEnvCommand = useMemo(
    () => customAgentEnvCommand(customEnvJSON),
    [customEnvJSON],
  );

  return (
    <div className="space-y-4">
      <div className="rounded-lg border bg-muted/30 px-3 py-2.5 text-xs leading-[1.55] text-muted-foreground">
        {t(($) => $.connect.custom_description)}
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

      <CommandStep
        n={1}
        label={t(($) => $.connect.custom_env_label)}
        cmd={customEnvCommand}
        copyAria={t(($) => $.connect.copy_aria)}
      />

      <LiveListening />
    </div>
  );
}

function TroubleshootingDetails({ tokenCmd }: { tokenCmd: string }) {
  const { t } = useT("runtimes");
  return (
    <details className="group rounded-lg border border-dashed">
      <summary className="flex cursor-pointer list-none items-center gap-1.5 px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <ChevronRight
          className="h-3 w-3 transition-transform group-open:rotate-90"
          aria-hidden
        />
        {t(($) => $.connect.troubleshooting)}
      </summary>
      <div className="space-y-2 border-t px-3 pt-2.5 pb-3 text-[11px] leading-[1.55] text-muted-foreground">
        <p>{t(($) => $.connect.trouble_intro)}</p>
        <CommandStep
          n={2}
          label={t(($) => $.connect.step2_label)}
          cmd={tokenCmd}
          copyAria={t(($) => $.connect.copy_aria)}
        />
        <p>
          {t(($) => $.connect.trouble_token_hint_prefix)}
          <span className="font-medium text-foreground">
            {t(($) => $.connect.trouble_token_hint_destination)}
          </span>
          {t(($) => $.connect.trouble_token_hint_suffix)}
        </p>
        <ul className="space-y-1">
          <li className="flex items-center gap-1.5">
            <span>{t(($) => $.connect.trouble_check_status)}</span>
            {/* CLI command — literal shell string, not i18n content. */}
            <code
              className={cn(
                "rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-foreground",
                CODE_LIGATURE_CLASS,
              )}
            >
              {"multica daemon status"}
            </code>
          </li>
          <li className="flex items-center gap-1.5">
            <span>{t(($) => $.connect.trouble_view_logs)}</span>
            {/* CLI command — literal shell string, not i18n content. */}
            <code
              className={cn(
                "rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-foreground",
                CODE_LIGATURE_CLASS,
              )}
            >
              {"multica daemon logs -f"}
            </code>
          </li>
        </ul>
      </div>
    </details>
  );
}

// ---------------------------------------------------------------------------
// Live-listening indicator
// ---------------------------------------------------------------------------

function LiveListening() {
  const { t } = useT("runtimes");
  return (
    <div
      className="flex items-center gap-2.5 rounded-lg border bg-muted/40 px-3 py-2.5 text-xs"
      role="status"
      aria-live="polite"
    >
      <span className="relative inline-flex shrink-0" aria-hidden>
        <span className="absolute inline-flex h-2 w-2 animate-ping rounded-full bg-success opacity-60 motion-reduce:hidden" />
        <span className="relative inline-flex h-2 w-2 rounded-full bg-success" />
      </span>
      <span className="font-medium text-foreground">
        {t(($) => $.connect.live_listening)}
      </span>
      <span className="text-muted-foreground">
        {t(($) => $.connect.live_listening_hint)}
      </span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Step 2: Success
// ---------------------------------------------------------------------------

function SuccessStep({
  onGoToAgents,
  onGoToRuntime,
}: {
  onGoToAgents: () => void;
  onGoToRuntime?: () => void;
}) {
  const { t } = useT("runtimes");
  return (
    <>
      <DialogHeader className="px-6 pt-6 pb-2">
        <DialogTitle className="text-base text-balance">
          {t(($) => $.connect.success_title)}
        </DialogTitle>
        <DialogDescription className="text-xs text-balance">
          {t(($) => $.connect.success_description)}
        </DialogDescription>
      </DialogHeader>

      <div className="flex flex-col items-center gap-3 px-6 py-8">
        <div
          className="flex h-12 w-12 items-center justify-center rounded-full bg-success/10"
          aria-hidden
        >
          <Check className="h-6 w-6 text-success" />
        </div>
      </div>

      <DialogFooter className="m-0 rounded-b-xl border-t bg-muted/30 px-6 py-3">
        {onGoToRuntime && (
          <Button variant="ghost" size="sm" onClick={onGoToRuntime}>
            {t(($) => $.connect.view_runtime)}
          </Button>
        )}
        <Button size="sm" onClick={onGoToAgents}>
          {t(($) => $.connect.create_agent)}
          <ChevronRight className="h-3.5 w-3.5" aria-hidden />
        </Button>
      </DialogFooter>
    </>
  );
}
