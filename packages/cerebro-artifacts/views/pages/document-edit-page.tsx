"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Upload } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { artifactDetailOptions } from "@multica/cerebro-artifacts/core";
import {
  useUpdateArtifact,
  useUploadArtifactFile,
} from "@multica/cerebro-artifacts/core";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { MobileSidebarTrigger } from "@multica/views/layout/page-header";
import { ContentEditor, type ContentEditorRef } from "@multica/views/editor";

export function DocumentEditPage({ artifactId }: { artifactId: string }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const { data: artifact, isLoading } = useQuery(
    artifactDetailOptions(wsId, artifactId),
  );
  const userId = useAuthStore((s) => s.user?.id);
  const update = useUpdateArtifact();
  const upload = useUploadArtifactFile();

  const [title, setTitle] = React.useState("");
  const [body, setBody] = React.useState("");
  const [fileUrl, setFileUrl] = React.useState<string | null>(null);
  const [fileSize, setFileSize] = React.useState<number | null>(null);
  const [hydrated, setHydrated] = React.useState(false);
  const fileInput = React.useRef<HTMLInputElement>(null);
  const editorRef = React.useRef<ContentEditorRef>(null);

  React.useEffect(() => {
    if (artifact && !hydrated) {
      setTitle(artifact.title);
      setBody(artifact.body);
      setFileUrl(artifact.file_url);
      setFileSize(artifact.file_size_bytes);
      setHydrated(true);
    }
  }, [artifact, hydrated]);

  const canEdit =
    !!artifact &&
    artifact.author_type === "member" &&
    artifact.author_id === userId;

  React.useEffect(() => {
    if (artifact && !canEdit) {
      router.push(wsPaths.documentDetail(artifact.id));
    }
  }, [artifact, canEdit, router, wsPaths]);

  if (isLoading || !artifact || !canEdit) {
    return (
      <div className="flex h-full flex-col">
        <div className="flex h-12 shrink-0 items-center border-b px-4">
          <MobileSidebarTrigger className="mr-0" />
        </div>
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          Loading…
        </div>
      </div>
    );
  }

  const handleFile = async (file: File | undefined) => {
    if (!file) return;
    const result = await upload.mutateAsync(file);
    setFileUrl(result.url);
    setFileSize(result.size_bytes);
  };

  const handleSave = async () => {
    const nextBody =
      artifact.format === "md"
        ? editorRef.current?.getMarkdown() ?? body
        : body;
    await update.mutateAsync({
      id: artifact.id,
      data: {
        title: title.trim(),
        body: nextBody,
        file_url: artifact.format === "pdf" ? fileUrl : undefined,
        file_size_bytes: artifact.format === "pdf" ? fileSize : undefined,
      },
    });
    router.push(wsPaths.documentDetail(artifact.id));
  };

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto w-full max-w-5xl px-4 py-4 md:px-8 md:py-6">
        <div className="mb-3 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <MobileSidebarTrigger className="mr-0" />
            <Button
              variant="ghost"
              size="sm"
              onClick={() => router.push(wsPaths.documentDetail(artifact.id))}
            >
              <ArrowLeft className="mr-1 size-4" /> Cancel
            </Button>
          </div>
          <Button
            size="sm"
            onClick={handleSave}
            disabled={update.isPending || !title.trim()}
          >
            {update.isPending ? "Saving…" : "Save"}
          </Button>
        </div>

        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1">
            <Label htmlFor="doc-title">Title</Label>
            <Input
              id="doc-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Title"
            />
          </div>

          {artifact.format === "pdf" ? (
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
                    : "No PDF uploaded"}
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
          ) : artifact.format === "md" ? (
            <div className="flex flex-col gap-1">
              <Label>Body</Label>
              <div className="rounded-md border border-input bg-background px-3 py-2 min-h-[60vh]">
                {hydrated && (
                  <ContentEditor
                    ref={editorRef}
                    defaultValue={artifact.body}
                    placeholder="Write something…"
                  />
                )}
              </div>
            </div>
          ) : (
            <div className="flex flex-col gap-1">
              <Label htmlFor="doc-body">Body (HTML)</Label>
              <Textarea
                id="doc-body"
                value={body}
                onChange={(e) => setBody(e.target.value)}
                placeholder="<p>HTML</p>"
                className="min-h-[60vh] font-mono text-sm"
              />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
