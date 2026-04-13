"use client";

import { useState, useCallback, useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Terminal, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useWSEvent } from "@multica/core/realtime";
import { api } from "@multica/core/api";
import { ProviderLogo } from "../runtimes/components/provider-logo";
import {
  runtimeListOptions,
  runtimeKeys,
} from "@multica/core/runtimes/queries";

const CLOUD_HOST = "multica.ai";

const INSTALL_STEP = {
  label: "Install the Multica CLI",
  cmd: "curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash",
};

function isCloudEnvironment(): boolean {
  if (typeof window === "undefined") return true;
  return window.location.hostname.endsWith(CLOUD_HOST);
}

function buildSetupCommand(): string {
  if (isCloudEnvironment()) return "multica setup";

  const appUrl = typeof window !== "undefined" ? window.location.origin : "";
  const apiBaseUrl = api.getBaseUrl?.() ?? "";
  const serverUrl = apiBaseUrl || appUrl;

  if (!serverUrl || serverUrl === "http://localhost:8080") {
    // Default self-host — no flags needed
    return "multica setup self-host";
  }

  const parts = ["multica setup self-host"];
  parts.push(`--server-url ${serverUrl}`);
  if (appUrl && appUrl !== serverUrl) {
    parts.push(`--app-url ${appUrl}`);
  }
  return parts.join(" ");
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <button
      type="button"
      onClick={handleCopy}
      className="shrink-0 rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
    >
      {copied ? (
        <Check className="h-3.5 w-3.5 text-success" />
      ) : (
        <Copy className="h-3.5 w-3.5" />
      )}
    </button>
  );
}

export function StepRuntime({
  wsId,
  onNext,
}: {
  wsId: string;
  onNext: () => void;
}) {
  const qc = useQueryClient();

  const setupSteps = useMemo(
    () => [
      INSTALL_STEP,
      { label: "Set up and start the daemon", cmd: buildSetupCommand() },
    ],
    [],
  );

  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));

  const handleDaemonEvent = useCallback(() => {
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  }, [qc, wsId]);

  useWSEvent("daemon:register", handleDaemonEvent);

  const hasRuntimes = runtimes.length > 0;

  return (
    <div className="flex w-full max-w-xl flex-col items-center gap-8">
      <div className="text-center">
        <h1 className="text-3xl font-semibold tracking-tight">
          Connect a Runtime
        </h1>
        <p className="mt-2 text-muted-foreground">
          Install the CLI and run the setup command below to connect your
          machine. The daemon auto-detects agent CLIs (Claude Code, Codex,
          etc.) on your PATH.
        </p>
      </div>

      {/* Commands */}
      <Card className="w-full">
        <CardContent className="space-y-3 pt-4">
          {setupSteps.map((step, i) => (
            <div key={i}>
              <p className="mb-1.5 text-xs text-muted-foreground">
                {i + 1}. {step.label}
              </p>
              <div className="flex items-start gap-2 rounded-lg bg-muted px-3 py-2.5 font-mono text-sm">
                <Terminal className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <code className="min-w-0 flex-1 break-all whitespace-pre-wrap">
                  {step.cmd}
                </code>
                <CopyButton text={step.cmd} />
              </div>
            </div>
          ))}
          <p className="pt-1 text-xs text-muted-foreground">
            The setup command handles authentication, configuration, and daemon
            startup — all in one step.
          </p>
        </CardContent>
      </Card>

      {/* Connected runtimes */}
      <div className="w-full space-y-3">
        <div className="flex items-center gap-2 text-sm">
          {hasRuntimes ? (
            <>
              <div className="h-2 w-2 rounded-full bg-success" />
              <span className="font-medium">
                {runtimes.length} runtime{runtimes.length > 1 ? "s" : ""}{" "}
                connected
              </span>
            </>
          ) : (
            <>
              <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
              <span className="text-muted-foreground">
                Waiting for connection...
              </span>
            </>
          )}
        </div>

        {hasRuntimes && (
          <Card className="w-full">
            <CardContent className="divide-y pt-0">
              {runtimes.map((rt) => (
                <div
                  key={rt.id}
                  className="flex items-center gap-3 py-3 first:pt-4 last:pb-4"
                >
                  <span
                    className={`h-2 w-2 shrink-0 rounded-full ${
                      rt.status === "online"
                        ? "bg-success"
                        : "bg-muted-foreground/40"
                    }`}
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium">
                        {rt.name}
                      </span>
                      {rt.runtime_mode === "cloud" && (
                        <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                          Cloud
                        </span>
                      )}
                    </div>
                    <div className="truncate text-xs text-muted-foreground">
                      {rt.provider} · {rt.device_info}
                    </div>
                  </div>
                  <ProviderLogo
                    provider={rt.provider}
                    className="h-5 w-5 shrink-0"
                  />
                </div>
              ))}
            </CardContent>
          </Card>
        )}
      </div>

      {/* Actions */}
      <Button className="w-full" size="lg" onClick={onNext}>
        {hasRuntimes ? "Continue" : "Skip for now"}
      </Button>
    </div>
  );
}
