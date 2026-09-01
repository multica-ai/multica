"use client";

import { useRef, useState } from "react";
import { ArrowLeft, Check, FileUp, LayoutTemplate, Loader2, PackageOpen, Users } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { MarketplaceTemplateFileSchema } from "@multica/core/api/schemas";
import { marketplaceTemplateListOptions } from "@multica/core/templates";
import type { MarketplaceTemplateFile } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

type ImportMode = "file" | "gallery" | null;

export function ImportSquadTemplateDialog({
  open,
  onOpenChange,
  wsId,
  onMarketplaceTemplate,
  onFileTemplate,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  wsId: string;
  onMarketplaceTemplate: (templateId: string) => void;
  onFileTemplate: (manifest: MarketplaceTemplateFile) => void;
}) {
  const { t } = useT("squads");
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [mode, setMode] = useState<ImportMode>(null);
  const [selectedTemplateId, setSelectedTemplateId] = useState("");
  const [fileError, setFileError] = useState("");
  const { data, isPending } = useQuery(marketplaceTemplateListOptions(wsId, {
    source_type: "squad",
    scope: "all",
    sort: "popular",
    page_size: 100,
  }));

  const close = (next: boolean) => {
    if (!next) {
      setMode(null);
      setSelectedTemplateId("");
      setFileError("");
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
    onOpenChange(next);
  };

  const handleFile = async (file: File | undefined) => {
    if (!file) return;
    setFileError("");
    try {
      const raw: unknown = JSON.parse(await file.text());
      const parsed = MarketplaceTemplateFileSchema.safeParse(raw);
      if (!parsed.success || parsed.data.source_type !== "squad") {
        setFileError(t(($) => $.import_dialog.invalid_file));
        return;
      }
      close(false);
      onFileTemplate(parsed.data as MarketplaceTemplateFile);
    } catch {
      setFileError(t(($) => $.import_dialog.invalid_file));
    }
  };

  const templates = data?.templates ?? [];

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-h-[86vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.import_dialog.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.import_dialog.description)}</DialogDescription>
        </DialogHeader>

        {mode === null ? (
          <div className="grid gap-3 py-2 md:grid-cols-2">
            <button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              className="flex min-h-40 flex-col items-start rounded-xl border p-5 text-left transition-colors hover:bg-muted/40"
            >
              <span className="flex size-11 items-center justify-center rounded-xl bg-muted"><FileUp className="size-5" /></span>
              <span className="mt-5 text-body font-medium">{t(($) => $.import_dialog.choose_file)}</span>
              <span className="mt-1 text-caption text-muted-foreground">{t(($) => $.import_dialog.choose_file_description)}</span>
            </button>
            <button
              type="button"
              onClick={() => setMode("gallery")}
              className="flex min-h-40 flex-col items-start rounded-xl border p-5 text-left transition-colors hover:bg-muted/40"
            >
              <span className="flex size-11 items-center justify-center rounded-xl bg-muted"><PackageOpen className="size-5" /></span>
              <span className="mt-5 text-body font-medium">{t(($) => $.import_dialog.from_gallery)}</span>
              <span className="mt-1 text-caption text-muted-foreground">{t(($) => $.import_dialog.from_gallery_description)}</span>
            </button>
            <input
              ref={fileInputRef}
              type="file"
              accept=".multica-template.json,.json,application/json"
              className="hidden"
              onChange={(event) => void handleFile(event.target.files?.[0])}
            />
            {fileError ? <p className="col-span-full text-caption text-destructive">{fileError}</p> : null}
          </div>
        ) : null}

        {mode === "gallery" ? (
          <div className="py-2">
            <h3 className="text-body font-medium">{t(($) => $.import_dialog.gallery_title)}</h3>
            {isPending ? (
              <div className="flex justify-center py-12"><Loader2 className="size-5 animate-spin text-muted-foreground" /></div>
            ) : templates.length === 0 ? (
              <div className="mt-4 rounded-xl border border-dashed py-12 text-center text-body text-muted-foreground">{t(($) => $.import_dialog.no_templates)}</div>
            ) : (
              <div className="mt-4 grid max-h-[50vh] gap-2 overflow-y-auto pr-1">
                {templates.map((template) => {
                  const selected = selectedTemplateId === template.id;
                  return (
                    <button
                      key={template.id}
                      type="button"
                      onClick={() => setSelectedTemplateId(template.id)}
                      className={cn("flex items-start gap-3 rounded-xl border p-4 text-left hover:bg-muted/40", selected && "border-foreground ring-1 ring-foreground")}
                    >
                      <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted"><Users className="size-5" /></span>
                      <span className="min-w-0 flex-1">
                        <span className="block text-body font-medium">{template.name}</span>
                        <span className="mt-1 block line-clamp-2 text-caption text-muted-foreground">{template.description}</span>
                        <span className="mt-2 block text-caption text-muted-foreground">{template.agent_count} · {template.skill_count}</span>
                      </span>
                      {selected ? <Check className="mt-2 size-4 shrink-0" /> : null}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        ) : null}

        <DialogFooter className="justify-between sm:justify-between">
          <Button type="button" variant="ghost" disabled={mode === null} onClick={() => setMode(null)}>
            <ArrowLeft className="size-4" />
            {t(($) => $.import_dialog.back)}
          </Button>
          <Button
            type="button"
            disabled={mode !== "gallery" || !selectedTemplateId}
            onClick={() => {
              close(false);
              onMarketplaceTemplate(selectedTemplateId);
            }}
          >
            <LayoutTemplate className="size-4" />
            {t(($) => $.page.import_button)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
