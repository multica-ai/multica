"use client";

import { useRef, useState, type DragEvent } from "react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { FilePlus, Link as LinkIcon } from "@multica/ui/icons";
import {
  useCreateKnowledgeDocument,
  useUploadKnowledgeDocument,
  type KnowledgeBase,
} from "@multica/core/knowledge";
import { useT } from "../i18n";

const KIND_PATTERN = /^[a-z][a-z0-9_]{1,63}$/;

/**
 * Adds documents to the library: a dropped or picked file, or a web page by
 * URL. A person's upload is published at once (the person is the curator), so
 * there is no draft step to follow. The kind is required; the server checks it
 * against the taxonomy.
 */
export function KnowledgeUploadPanel({
  wsId,
  knowledgeBases,
}: {
  wsId: string;
  knowledgeBases: KnowledgeBase[];
}) {
  const { t } = useT("knowledge");
  const upload = useUploadKnowledgeDocument(wsId);
  const importUrl = useCreateKnowledgeDocument(wsId);
  const inputRef = useRef<HTMLInputElement>(null);

  const [kb, setKb] = useState(knowledgeBases[0]?.key ?? "");
  const [kind, setKind] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [dragging, setDragging] = useState(false);
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");

  const kindValid = KIND_PATTERN.test(kind.trim());
  const canUpload = kb !== "" && kindValid && file !== null && !upload.isPending;
  const canImport = kb !== "" && url.trim() !== "" && !importUrl.isPending;

  const pickFile = (candidate: File | undefined) => {
    if (candidate) setFile(candidate);
  };

  const onDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    setDragging(false);
    pickFile(event.dataTransfer.files[0]);
  };

  const submitUpload = () => {
    if (!file) return;
    upload.mutate(
      { kb, kind: kind.trim(), file },
      {
        onSuccess: () => {
          toast.success(t(($) => $.upload.toast.uploaded, { name: file.name }));
          setFile(null);
          if (inputRef.current) inputRef.current.value = "";
        },
        onError: (error) => toast.error(error.message || t(($) => $.upload.toast.failed)),
      },
    );
  };

  const submitImport = () => {
    const page = url.trim();
    importUrl.mutate(
      { kb, title: title.trim() || page, url: page, publish: true },
      {
        onSuccess: () => {
          toast.success(t(($) => $.import.toast.imported));
          setUrl("");
          setTitle("");
        },
        onError: (error) => toast.error(error.message || t(($) => $.import.toast.failed)),
      },
    );
  };

  return (
    <section className="grid gap-4 md:grid-cols-2" aria-label={t(($) => $.upload.title)}>
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t(($) => $.upload.title)}</h3>
        <div
          onDragOver={(event) => {
            event.preventDefault();
            setDragging(true);
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={onDrop}
          className={`flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed p-6 text-center text-sm ${
            dragging ? "border-primary bg-accent" : "border-border"
          }`}
        >
          <FilePlus className="size-5 text-muted-foreground" />
          <p>{file ? file.name : t(($) => $.upload.drop_hint)}</p>
          <Button variant="outline" size="sm" onClick={() => inputRef.current?.click()}>
            {t(($) => $.upload.choose)}
          </Button>
          <input
            ref={inputRef}
            type="file"
            className="hidden"
            aria-label={t(($) => $.upload.choose)}
            onChange={(event) => pickFile(event.target.files?.[0])}
          />
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="knowledge-upload-kb">{t(($) => $.upload.base_label)}</Label>
            <Select
              items={knowledgeBases.map((b) => ({ value: b.key, label: b.name }))}
              value={kb}
              onValueChange={(value) => value && setKb(value)}
            >
              <SelectTrigger id="knowledge-upload-kb" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {knowledgeBases.map((b) => (
                  <SelectItem key={b.key} value={b.key}>
                    {b.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="knowledge-upload-kind">{t(($) => $.upload.kind_label)}</Label>
            <Input
              id="knowledge-upload-kind"
              value={kind}
              // eslint-disable-next-line no-restricted-syntax -- a technical kind key, not copy
              placeholder="market_report"
              aria-invalid={kind !== "" && !kindValid}
              onChange={(event) => setKind(event.target.value)}
            />
          </div>
        </div>
        {kind !== "" && !kindValid ? (
          <p className="text-xs text-destructive">{t(($) => $.upload.kind_hint)}</p>
        ) : null}
        <div>
          <Button onClick={submitUpload} disabled={!canUpload} aria-busy={upload.isPending}>
            {t(($) => $.upload.submit)}
          </Button>
        </div>
      </div>

      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t(($) => $.import.title)}</h3>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="knowledge-import-url">{t(($) => $.import.url_label)}</Label>
          <Input
            id="knowledge-import-url"
            type="url"
            placeholder="https://"
            value={url}
            onChange={(event) => setUrl(event.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="knowledge-import-title">{t(($) => $.import.title_label)}</Label>
          <Input
            id="knowledge-import-title"
            value={title}
            onChange={(event) => setTitle(event.target.value)}
          />
        </div>
        <div>
          <Button variant="outline" onClick={submitImport} disabled={!canImport} aria-busy={importUrl.isPending}>
            <LinkIcon />
            {t(($) => $.import.submit)}
          </Button>
        </div>
      </div>
    </section>
  );
}
