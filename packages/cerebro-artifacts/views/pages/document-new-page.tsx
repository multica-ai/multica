"use client";

import * as React from "react";
import { ArrowLeft, Upload } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  useCreateArtifact,
  useUploadArtifactFile,
} from "@multica/cerebro-artifacts/core";
import { useWorkspacePaths } from "@multica/core/paths";
import type { ArtifactKind, ArtifactFormat } from "@multica/core/types";
import { useNavigation } from "@multica/views/navigation";
import { MobileSidebarTrigger } from "@multica/views/layout/page-header";
import { KIND_LABELS } from "../components/kind-icon";
import { KIND_TEMPLATES, KIND_HELP } from "../components/kind-templates";

const KINDS: ArtifactKind[] = ["report", "plan", "decision", "diagram", "note"];
const FORMATS: ArtifactFormat[] = ["md", "html", "pdf"];
const FORMAT_LABELS: Record<ArtifactFormat, string> = {
  md: "Markdown",
  html: "HTML",
  pdf: "PDF (upload)",
};

export interface DocumentNewPageProps {
  /** Pre-selected folder. Read by route from query string. */
  folderId?: string | null;
  /** Pre-selected scope. When set, the new document attaches to a project or issue. */
  projectId?: string | null;
  issueId?: string | null;
}

export function DocumentNewPage(props: DocumentNewPageProps) {
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const create = useCreateArtifact();
  const upload = useUploadArtifactFile();

  const [kind, setKind] = React.useState<ArtifactKind>("note");
  const [format, setFormat] = React.useState<ArtifactFormat>("md");
  const [title, setTitle] = React.useState("");
  const [body, setBody] = React.useState(KIND_TEMPLATES.note);
  const [bodyDirty, setBodyDirty] = React.useState(false);
  const [fileUrl, setFileUrl] = React.useState<string | null>(null);
  const [fileSize, setFileSize] = React.useState<number | null>(null);
  const fileInput = React.useRef<HTMLInputElement>(null);

  const handleKindChange = (next: ArtifactKind) => {
    setKind(next);
    if (!bodyDirty) setBody(KIND_TEMPLATES[next]);
  };

  const handleBodyChange = (value: string) => {
    setBody(value);
    setBodyDirty(value !== KIND_TEMPLATES[kind]);
  };

  const handleFile = async (file: File | undefined) => {
    if (!file) return;
    const result = await upload.mutateAsync(file);
    setFileUrl(result.url);
    setFileSize(result.size_bytes);
  };

  const canSubmit =
    title.trim().length > 0 &&
    !create.isPending &&
    (format !== "pdf" || fileUrl !== null);

  const handleCreate = async () => {
    const artifact = await create.mutateAsync({
      kind,
      format,
      title: title.trim(),
      body: format === "pdf" ? "" : body,
      ...(format === "pdf" && fileUrl
        ? { file_url: fileUrl, file_size_bytes: fileSize ?? undefined }
        : {}),
      ...(props.folderId ? { folder_id: props.folderId } : {}),
      ...(props.projectId ? { project_id: props.projectId } : {}),
      ...(props.issueId ? { issue_id: props.issueId } : {}),
    });
    router.push(wsPaths.documentDetail(artifact.id));
  };

  return (
    <div className="h-full overflow-y-auto">
    <div className="mx-auto w-full max-w-3xl px-4 py-4 md:px-8 md:py-6">
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <MobileSidebarTrigger className="mr-0" />
          <Button
            variant="ghost"
            size="sm"
            onClick={() => router.push(wsPaths.documents())}
          >
            <ArrowLeft className="mr-1 size-4" /> Documents
          </Button>
        </div>
        <Button size="sm" onClick={handleCreate} disabled={!canSubmit}>
          {create.isPending ? "Creating…" : "Create"}
        </Button>
      </div>

      <h1 className="mb-4 text-2xl font-semibold">New document</h1>

      <div className="flex flex-col gap-4">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="doc-kind">Kind</Label>
            <Select
              value={kind}
              onValueChange={(v) => handleKindChange(v as ArtifactKind)}
            >
              <SelectTrigger id="doc-kind">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {KIND_LABELS[k]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{KIND_HELP[kind]}</p>
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="doc-format">Format</Label>
            <Select
              value={format}
              onValueChange={(v) => setFormat(v as ArtifactFormat)}
            >
              <SelectTrigger id="doc-format">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {FORMATS.map((f) => (
                  <SelectItem key={f} value={f}>
                    {FORMAT_LABELS[f]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {format === "md"
                ? "Plain markdown — best for reports, plans, notes."
                : format === "html"
                  ? "Raw HTML — for branded reports or inline charts."
                  : "Upload an existing PDF to keep alongside other docs."}
            </p>
          </div>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="doc-title">Title</Label>
          <Input
            id="doc-title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Short, descriptive title"
            autoFocus
          />
        </div>

        {format === "pdf" ? (
          <div className="flex flex-col gap-2">
            <Label>PDF file</Label>
            <input
              ref={fileInput}
              type="file"
              accept="application/pdf"
              className="hidden"
              onChange={(e) => handleFile(e.target.files?.[0])}
            />
            <div className="flex items-center justify-between rounded border border-border bg-card/40 px-3 py-2 text-xs">
              <span className="text-muted-foreground">
                {fileUrl
                  ? fileUrl.split("/").pop()
                  : "No PDF uploaded yet"}
                {fileSize ? ` · ${(fileSize / 1024).toFixed(1)} KB` : ""}
              </span>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => fileInput.current?.click()}
                disabled={upload.isPending}
              >
                <Upload className="mr-1 size-4" />
                {upload.isPending ? "Uploading…" : fileUrl ? "Replace" : "Upload"}
              </Button>
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-1">
            <Label htmlFor="doc-body">
              Body ({format === "html" ? "HTML" : "Markdown"})
            </Label>
            <Textarea
              id="doc-body"
              value={body}
              onChange={(e) => handleBodyChange(e.target.value)}
              placeholder={
                format === "html" ? "<p>HTML</p>" : "Markdown content"
              }
              className="min-h-[50vh] font-mono text-sm"
            />
          </div>
        )}
      </div>
    </div>
    </div>
  );
}
