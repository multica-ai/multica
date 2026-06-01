"use client";

import { useState } from "react";
import { Download } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useIssueStore } from "@/features/issues";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/shared/api";
import type {
  WorkspaceImportPayload,
  WorkspaceImportResult,
  ManifestIssue,
  WorkspaceExportManifest,
} from "@/shared/types";

function downloadJson(filename: string, data: unknown) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
  const url = window.URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  window.URL.revokeObjectURL(url);
}

export function DataTab() {
  const [exporting, setExporting] = useState(false);
  const [importing, setImporting] = useState(false);
  const [importJson, setImportJson] = useState("");
  const [importResult, setImportResult] = useState<WorkspaceImportResult | null>(null);

  const handleExport = async () => {
    setExporting(true);
    try {
      const manifest = await api.exportWorkspaceData();
      downloadJson(`workspace-export-${manifest.workspace.slug}.json`, manifest);
      toast.success("Workspace export downloaded");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to export workspace data");
    } finally {
      setExporting(false);
    }
  };

  const buildImportPayload = (): WorkspaceImportPayload => {
    const parsed = JSON.parse(importJson) as Partial<WorkspaceImportPayload> & Partial<WorkspaceExportManifest>;
    if (parsed.schema_version && parsed.source_type && Array.isArray(parsed.issues)) {
      return parsed as WorkspaceImportPayload;
    }
    if (parsed.schema_version && parsed.workspace?.id && Array.isArray(parsed.data?.issues)) {
      return {
        schema_version: parsed.schema_version,
        source_type: "canonical-json",
        workspace_id: parsed.workspace.id,
        issues: parsed.data.issues as ManifestIssue[],
      };
    }
    throw new Error("Invalid import JSON format");
  };

  const handleDryRun = async () => {
    setImporting(true);
    setImportResult(null);
    try {
      const payload = buildImportPayload();
      const result = await api.dryRunWorkspaceImport(payload);
      setImportResult(result);
      if ((result.errors?.length ?? 0) > 0 || result.failed > 0) {
        toast.error("Dry-run blocked");
        return;
      }
      toast.success("Dry-run completed");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Dry-run failed");
    } finally {
      setImporting(false);
    }
  };

  const handleApply = async () => {
    setImporting(true);
    setImportResult(null);
    try {
      const payload = buildImportPayload();
      const result = await api.applyWorkspaceImport(payload);
      setImportResult(result);
      if ((result.errors?.length ?? 0) > 0 || result.failed > 0) {
        toast.error("Import apply failed");
        return;
      }
      await useIssueStore.getState().fetch();
      toast.success("Import apply completed");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Import apply failed");
    } finally {
      setImporting(false);
    }
  };

  return (
    <section className="space-y-4">
      <h2 className="text-sm font-semibold">Data</h2>
      <Card>
        <CardContent className="space-y-3">
          <div>
            <p className="text-sm font-medium">Export workspace data</p>
            <p className="text-xs text-muted-foreground">
              Download the canonical JSON manifest for this workspace.
            </p>
          </div>
          <div className="flex justify-end">
            <Button onClick={handleExport} disabled={exporting} size="sm">
              <Download className="h-3 w-3" />
              {exporting ? "Exporting..." : "Export JSON"}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-3">
          <div>
            <p className="text-sm font-medium">Import workspace data</p>
            <p className="text-xs text-muted-foreground">
              Paste canonical JSON manifest and run dry-run before apply.
            </p>
          </div>
          <div>
            <Label htmlFor="workspace-import-json" className="text-xs text-muted-foreground">Manifest JSON</Label>
            <Textarea
              id="workspace-import-json"
              value={importJson}
              onChange={(event) => setImportJson(event.target.value)}
              className="mt-1 min-h-[140px] font-mono text-xs"
              placeholder='{"schema_version":"2026-05-31","workspace":{"id":"..."},"data":{"issues":[]}}'
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={importing || importJson.trim() === ""}
              onClick={handleDryRun}
            >
              Dry Run
            </Button>
            <Button
              size="sm"
              disabled={importing || importJson.trim() === ""}
              onClick={handleApply}
            >
              Apply Import
            </Button>
          </div>
          {importResult ? (
            <div className="rounded-md border bg-muted/30 p-3 text-xs">
              <p className="font-medium">{importResult.summary}</p>
              <p className="mt-1 text-muted-foreground">
                Created: {importResult.created} | Skipped: {importResult.skipped} | Failed: {importResult.failed}
              </p>
              {importResult.errors && importResult.errors.length > 0 ? (
                <ul className="mt-2 list-disc space-y-1 pl-4 text-destructive">
                  {importResult.errors.map((error) => (
                    <li key={`${error.code}-${error.message}`}>{error.message}</li>
                  ))}
                </ul>
              ) : null}
            </div>
          ) : null}
        </CardContent>
      </Card>
    </section>
  );
}
