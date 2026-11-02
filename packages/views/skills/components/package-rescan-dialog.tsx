"use client";

import { useState } from "react";
import { Loader2, Package } from "@multica/ui/icons";
import type { SkillPackage, SkillPackagePreview } from "@multica/core/api/schemas";
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
 * Rescan an imported package against its source. Only changed imported items
 * are pre-selected (server defaults); new candidates need an explicit check,
 * removed paths stay retained. An optional URL may move the ref — never the
 * repository or directory.
 */
export function RescanPackageDialog({
  wsId,
  pkg,
  open,
  onOpenChange,
}: {
  wsId: string;
  pkg: SkillPackage;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("skill-packages");
  const [refUrl, setRefUrl] = useState("");
  const [previewing, setPreviewing] = useState(false);
  const [applying, setApplying] = useState(false);
  const [preview, setPreview] = useState<SkillPackagePreview | null>(null);
  const [error, setError] = useState<PreviewError | null>(null);

  const reset = () => {
    setRefUrl("");
    setPreview(null);
    setError(null);
    setPreviewing(false);
    setApplying(false);
  };

  // Previewing or applying blocks every close path (X, Escape, outside
  // click all funnel through onOpenChange) so the per-item report — the
  // only place failure diagnostics live — is never lost mid-write.
  const handleOpenChange = (v: boolean) => {
    if (previewing || applying) return;
    if (!v) reset();
    onOpenChange(v);
  };

  const runPreview = async () => {
    setPreviewing(true);
    setError(null);
    try {
      const override = refUrl.trim();
      const result = await api.rescanSkillPackage(
        wsId,
        pkg.id,
        override ? { url: override } : undefined,
      );
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
    void runPreview();
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="flex h-[32rem] max-h-[85svh] max-w-lg flex-col">
        <DialogHeader>
          <DialogTitle className="text-body">
            {t(($) => $.rescan.dialog_title)}
          </DialogTitle>
        </DialogHeader>

        {preview ? (
          <PackageApplyStage
            key={preview.preview_id}
            wsId={wsId}
            preview={preview}
            existingPackageName={pkg.owner_repo}
            allowEmptyApply
            applyRequest={(skills, onConflict) =>
              api.applySkillPackageRescan(wsId, pkg.id, {
                // The preview's canonical URL carries any ref override;
                // without it the server falls back to the saved URL and
                // the fingerprint check fails as preview_stale.
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
              <div className="flex items-center gap-1.5 text-body">
                <Package className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <span className="truncate font-medium">{pkg.owner_repo}</span>
                <span className="shrink-0 text-caption text-muted-foreground">
                  {pkg.subdirectory ? `${pkg.subdirectory} · ` : ""}
                  {pkg.ref}
                </span>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="package-ref-url" className="text-caption text-muted-foreground">
                  {t(($) => $.rescan.ref_label)}
                </Label>
                <Input
                  id="package-ref-url"
                  value={refUrl}
                  onChange={(e) => {
                    setRefUrl(e.target.value);
                    setError(null);
                  }}
                  placeholder={t(($) => $.rescan.ref_placeholder)}
                  className="font-mono text-body"
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && !previewing) void runPreview();
                  }}
                />
                <DialogDescription>
                  {t(($) => $.rescan.ref_hint)}
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
                onClick={() => void runPreview()}
                disabled={previewing}
                aria-busy={previewing}
              >
                {previewing ? (
                  <>
                    <Loader2 className="h-3 w-3 animate-spin" />
                    {t(($) => $.rescan.previewing)}
                  </>
                ) : (
                  t(($) => $.rescan.preview)
                )}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
