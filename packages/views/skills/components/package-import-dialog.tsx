"use client";

import { useState } from "react";
import { Loader2 } from "@multica/ui/icons";
import type { SkillPackagePreview } from "@multica/core/api/schemas";
import { api } from "@multica/core/api";
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
import { useT } from "../../i18n";
import { PackageApplyStage } from "./package-apply-stage";

type PreviewError = { kind: "fetch" | "malformed"; message?: string };

/**
 * "Import a skill package from a repository URL." URL → read-only preview →
 * checklist apply → per-item report. The single-skill URL import in
 * CreateSkillDialog is untouched; this is the package flow's only entry.
 */
export function ImportPackageDialog({
  wsId,
  open,
  onOpenChange,
}: {
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("skill-packages");
  const [url, setUrl] = useState("");
  const [previewing, setPreviewing] = useState(false);
  const [applying, setApplying] = useState(false);
  const [preview, setPreview] = useState<SkillPackagePreview | null>(null);
  const [error, setError] = useState<PreviewError | null>(null);

  const reset = () => {
    setUrl("");
    setPreview(null);
    setError(null);
    setPreviewing(false);
    setApplying(false);
  };

  // Same rule as the rescan dialog: no close path stays open mid-write.
  const handleOpenChange = (v: boolean) => {
    if (previewing || applying) return;
    if (!v) reset();
    onOpenChange(v);
  };

  const runPreview = async (targetUrl: string) => {
    setPreviewing(true);
    setError(null);
    try {
      const result = await api.previewSkillPackage(wsId, { url: targetUrl });
      if (result === null) {
        setPreview(null);
        setError({ kind: "malformed" });
      } else {
        setPreview(result);
      }
    } catch (err) {
      setPreview(null);
      setError({
        kind: "fetch",
        message:
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.preview.error_fallback),
      });
    } finally {
      setPreviewing(false);
    }
  };

  const handleRepreview = () => {
    setPreview(null);
    void runPreview(url.trim());
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="flex h-[32rem] max-h-[85svh] max-w-lg flex-col">
        <DialogHeader>
          <DialogTitle className="text-body">
            {t(($) => $.import.dialog_title)}
          </DialogTitle>
        </DialogHeader>

        {preview ? (
          <PackageApplyStage
            key={preview.preview_id}
            wsId={wsId}
            preview={preview}
            existingPackageName={preview.package?.owner_repo ?? null}
            applyRequest={(skills, onConflict) =>
              api.applySkillPackage(wsId, {
                url: preview.source.url,
                preview_id: preview.preview_id,
                skills,
                on_conflict: onConflict,
              })
            }
            onRepreview={handleRepreview}
            onClose={() => handleOpenChange(false)}
            onBusyChange={setApplying}
          />
        ) : (
          <>
            <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
              <div className="space-y-1.5">
                <Label htmlFor="package-url" className="text-caption text-muted-foreground">
                  {t(($) => $.import.url_label)}
                </Label>
                <Input
                  id="package-url"
                  autoFocus
                  value={url}
                  onChange={(e) => {
                    setUrl(e.target.value);
                    setError(null);
                  }}
                  placeholder={t(($) => $.import.url_placeholder)}
                  className="font-mono text-body"
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && url.trim() && !previewing) {
                      void runPreview(url.trim());
                    }
                  }}
                />
                <DialogDescription>
                  {t(($) => $.import.url_hint)}
                </DialogDescription>
              </div>
              {error && (
                <div role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-caption text-destructive">
                  {error.kind === "malformed"
                    ? t(($) => $.preview.malformed_description)
                    : error.message}
                </div>
              )}
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="ghost"
                onClick={() => handleOpenChange(false)}
                disabled={previewing}
              >
                {t(($) => $.import.cancel)}
              </Button>
              <Button
                type="button"
                onClick={() => void runPreview(url.trim())}
                disabled={!url.trim() || previewing}
                aria-busy={previewing}
              >
                {previewing ? (
                  <>
                    <Loader2 className="h-3 w-3 animate-spin" />
                    {t(($) => $.import.previewing)}
                  </>
                ) : (
                  t(($) => $.import.preview)
                )}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
