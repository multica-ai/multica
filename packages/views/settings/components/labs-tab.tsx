"use client";

import { useEffect, useState } from "react";
import { FolderOpen, GitCommitHorizontal } from "lucide-react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Switch } from "@multica/ui/components/ui/switch";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import { useProductCapabilities } from "@multica/core/platform";
import type { Workspace } from "@multica/core/types";

export function LabsTab() {
  const workspace = useCurrentWorkspace();
  const capabilities = useProductCapabilities();
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);

  const coAuthoredByEnabled =
    (workspace?.settings as Record<string, unknown>)?.co_authored_by_enabled !== false;

  const handleToggle = async (checked: boolean) => {
    if (!workspace || saving) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        settings: {
          ...((workspace.settings as Record<string, unknown>) ?? {}),
          co_authored_by_enabled: checked,
        },
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Failed to update setting",
      );
    } finally {
      setSaving(false);
    }
  };

  if (!workspace) return null;

  return (
    <div className="space-y-4">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">Git</h2>

        <Card>
          <CardContent>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
                  <GitCommitHorizontal className="h-4 w-4" />
                </div>
                <div className="space-y-1">
                  <Label
                    htmlFor="co-authored-by"
                    className="text-sm font-medium"
                  >
                    Co-authored-by trailer
                  </Label>
                  <p className="text-sm text-muted-foreground">
                    Automatically add{" "}
                    <code className="rounded bg-muted px-1 py-0.5 text-xs">
                      Co-authored-by: multica-agent &lt;github@multica.ai&gt;
                    </code>{" "}
                    to commits made by agents.
                  </p>
                </div>
              </div>
              <Switch
                id="co-authored-by"
                checked={coAuthoredByEnabled}
                onCheckedChange={handleToggle}
                disabled={saving}
              />
            </div>
          </CardContent>
        </Card>
      </section>

      {capabilities.settings.showDiagnostics && <LocalDiagnosticsSection />}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Local diagnostics — surfaced only when capabilities allow (i.e. local
// product). Reads a fresh snapshot from the desktop bridge on mount; lets the
// user open each well-known data folder in the OS file manager and copy a
// pre-formatted diagnostics report to the clipboard. There is no "send" path —
// the local-only product has no remote diagnostics submission.
// ---------------------------------------------------------------------------

type LocalDiagnosticsBridgeShape = {
  get: () => Promise<{
    paths: Record<string, string>;
  }>;
  formatAsText: () => Promise<string>;
  openPath: (
    key: string,
  ) => Promise<{ ok: boolean; error?: string }>;
};

function getDiagnosticsBridge(): LocalDiagnosticsBridgeShape | null {
  if (typeof window === "undefined") return null;
  const candidate = (window as unknown as {
    localDiagnosticsAPI?: LocalDiagnosticsBridgeShape;
  }).localDiagnosticsAPI;
  return candidate ?? null;
}

const PATH_LABELS: Record<string, string> = {
  root: "User data root",
  postgresData: "Postgres data",
  postgresLogs: "Postgres logs",
  daemonLogs: "Daemon logs",
  appLogs: "App logs",
  appConfig: "App config",
};

function LocalDiagnosticsSection() {
  const [paths, setPaths] = useState<Record<string, string> | null>(null);
  const [copying, setCopying] = useState(false);

  useEffect(() => {
    const bridge = getDiagnosticsBridge();
    if (!bridge) return;
    let cancelled = false;
    void bridge.get().then((diagnostics) => {
      if (cancelled) return;
      setPaths(diagnostics.paths);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const handleOpen = async (key: string) => {
    const bridge = getDiagnosticsBridge();
    if (!bridge) return;
    const result = await bridge.openPath(key);
    if (!result.ok) {
      toast.error(result.error ?? "Could not open folder");
    }
  };

  const handleCopy = async () => {
    const bridge = getDiagnosticsBridge();
    if (!bridge || copying) return;
    setCopying(true);
    try {
      const text = await bridge.formatAsText();
      await navigator.clipboard.writeText(text);
      toast.success("Diagnostics copied to clipboard");
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to copy diagnostics",
      );
    } finally {
      setCopying(false);
    }
  };

  return (
    <section className="space-y-4">
      <h2 className="text-sm font-semibold">Local diagnostics</h2>

      <Card>
        <CardContent className="space-y-4">
          <div className="flex items-start justify-between gap-4">
            <p className="text-sm text-muted-foreground">
              Inspect the folders Multica owns on this machine and copy a
              snapshot of the local stack status. Nothing is sent off-device.
            </p>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handleCopy}
              disabled={copying}
            >
              Copy diagnostics
            </Button>
          </div>

          {paths && (
            <div className="space-y-2">
              {Object.entries(paths).map(([key, value]) => (
                <div
                  key={key}
                  className="flex items-center justify-between gap-3 rounded-md border bg-muted/20 px-3 py-2"
                >
                  <div className="min-w-0 flex-1 space-y-0.5">
                    <div className="text-xs font-medium text-muted-foreground">
                      {PATH_LABELS[key] ?? key}
                    </div>
                    <div className="truncate font-mono text-xs">{value}</div>
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => handleOpen(key)}
                  >
                    <FolderOpen className="h-3.5 w-3.5" />
                    Open
                  </Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </section>
  );
}
