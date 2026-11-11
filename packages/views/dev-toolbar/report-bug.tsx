"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { DevRecording } from "@multica/core/diagnostics";
import { paths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Bug, Camera, Copy, ExternalLink, Loader2 } from "@multica/ui/icons";
import { copyText } from "@multica/ui/lib/clipboard";
import { apiHost } from "./metrics";
import { buildReportMarkdown, reportCounts, type ReportSections } from "./report";

export interface ScreenshotCapture {
  capture: () => Promise<Blob>;
  /** Take one each time the report opens. Off where capturing asks the user first. */
  automatic: boolean;
}

interface CreatedIssue {
  identifier: string;
  path: string;
}

const ALL_SECTIONS: ReportSections = { trail: true, errors: true, failedRequests: true };

function errorMessage(err: unknown): string {
  return err instanceof Error && err.message ? err.message : String(err);
}

function nextFrame(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
}

function useObjectUrl(blob: Blob | null): string | null {
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    if (!blob) return setUrl(null);
    const next = URL.createObjectURL(blob);
    setUrl(next);
    return () => URL.revokeObjectURL(next);
  }, [blob]);
  return url;
}

export function ReportBug({
  recording,
  build,
  apiUrl,
  route,
  workspaceSlug,
  role,
  screenshot,
  openPath,
}: {
  recording: DevRecording;
  build: string;
  apiUrl: string;
  route: string;
  workspaceSlug: string | null;
  role: string | null;
  screenshot?: ScreenshotCapture;
  openPath: (path: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [sections, setSections] = useState(ALL_SECTIONS);
  const [image, setImage] = useState<Blob | null>(null);
  const [includeImage, setIncludeImage] = useState(true);
  const [capturing, setCapturing] = useState(false);
  const [captureError, setCaptureError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [created, setCreated] = useState<CreatedIssue | null>(null);
  const [openedAt, setOpenedAt] = useState(() => Date.now());
  const imageUrl = useObjectUrl(image);

  // The report must show the page, not itself: close, let the popover leave
  // the screen, capture, then reopen.
  const capture = async () => {
    if (!screenshot) return;
    setOpen(false);
    setCapturing(true);
    setCaptureError(null);
    try {
      await nextFrame();
      setImage(await screenshot.capture());
      setIncludeImage(true);
    } catch (err) {
      setCaptureError(errorMessage(err));
    } finally {
      setCapturing(false);
      setOpen(true);
    }
  };

  const onOpenChange = (next: boolean) => {
    if (!next) return setOpen(false);
    setOpenedAt(Date.now());
    setCreated(null);
    if (screenshot?.automatic) void capture();
    else setOpen(true);
  };

  const environment = () => ({
    build,
    apiUrl,
    route,
    workspaceSlug,
    role,
    userAgent: navigator.userAgent,
    capturedAt: Date.now(),
  });

  const copyMarkdown = async () => {
    const markdown = buildReportMarkdown({
      description,
      environment: environment(),
      recording,
      sections,
      screenshotUrl: null,
    });
    const text = title.trim() ? `# ${title.trim()}\n\n${markdown}` : markdown;
    if (await copyText(text)) toast.success("Copied the report as Markdown");
    else toast.error("Could not copy the report");
  };

  const copyImage = async () => {
    if (!image) return;
    try {
      await navigator.clipboard.write([new ClipboardItem({ [image.type]: image })]);
      toast.success("Copied the screenshot");
    } catch (err) {
      toast.error(`Could not copy the screenshot: ${errorMessage(err)}`);
    }
  };

  const createIssue = async () => {
    if (!workspaceSlug || !title.trim()) return;
    setSubmitting(true);
    try {
      const attachment =
        image && includeImage
          ? await api.uploadFile(new File([image], `screenshot-${Date.now()}.png`, { type: image.type }))
          : null;
      const issue = await api.createIssue(
        {
          title: title.trim(),
          description: buildReportMarkdown({
            description,
            environment: environment(),
            recording,
            sections,
            screenshotUrl: attachment?.markdown_url ?? null,
          }),
          attachment_ids: attachment ? [attachment.id] : undefined,
        },
        workspaceSlug,
      );
      setCreated({ identifier: issue.identifier, path: paths.workspace(workspaceSlug).issueDetail(issue.id) });
      setTitle("");
      setDescription("");
      setImage(null);
      setSections(ALL_SECTIONS);
    } catch (err) {
      toast.error(`Could not create the issue: ${errorMessage(err)}`);
    } finally {
      setSubmitting(false);
    }
  };

  const counts = reportCounts(recording, openedAt);
  const checklist: { key: keyof ReportSections; label: string; count: number }[] = [
    { key: "trail", label: "Route trail", count: counts.trail },
    { key: "errors", label: "Errors", count: counts.errors },
    { key: "failedRequests", label: "Failed requests", count: counts.failedRequests },
  ];

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger
        data-testid="dev-toolbar-report"
        className="flex h-5 items-center gap-1 rounded-md px-1.5 text-foreground hover:bg-accent data-[popup-open]:bg-accent"
      >
        {capturing ? <Loader2 className="size-3 animate-spin" /> : <Bug className="size-3" />}
        <span>Report bug</span>
      </PopoverTrigger>
      <PopoverContent side="top" align="end" sideOffset={6} className="w-[440px] gap-3 p-3 text-caption">
        <div className="flex items-baseline justify-between gap-2">
          <span className="font-medium text-foreground">Report a bug</span>
          <span className="truncate text-micro text-muted-foreground">
            {workspaceSlug ? `files into ${workspaceSlug} on ${apiHost(apiUrl)}` : "open a workspace to file it"}
          </span>
        </div>

        {created && (
          <div className="flex items-center justify-between gap-2 rounded-md bg-success/10 px-2 py-1.5">
            <span>Created {created.identifier}</span>
            <Button size="xs" variant="outline" onClick={() => {
                setOpen(false);
                openPath(created.path);
              }}>
              <ExternalLink />
              Open
            </Button>
          </div>
        )}

        <Input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="What went wrong?"
          data-testid="dev-toolbar-report-title"
        />
        <Textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Steps to reproduce, what you expected, what happened"
          className="max-h-40 min-h-20"
        />

        <div className="flex gap-3">
          <div className="flex w-36 shrink-0 flex-col gap-1">
            <div className="grid h-20 w-full place-items-center overflow-hidden rounded-md border border-border bg-muted">
              {imageUrl ? (
                <img src={imageUrl} alt="" className="size-full object-cover object-top" />
              ) : (
                <span className="px-2 text-center text-micro text-muted-foreground">
                  {captureError
                    ? `Screenshot failed: ${captureError}`
                    : screenshot
                      ? "No screenshot"
                      : "Screenshots are not available here"}
                </span>
              )}
            </div>
            <div className="flex">
              {screenshot && (
                <Button size="xs" variant="ghost" onClick={() => void capture()} disabled={capturing}>
                  <Camera />
                  {image ? "Retake" : "Take"}
                </Button>
              )}
              {image && (
                <Button size="xs" variant="ghost" onClick={() => void copyImage()}>
                  <Copy />
                  Copy
                </Button>
              )}
            </div>
          </div>
          <div className="flex min-w-0 flex-1 flex-col gap-1.5">
            <label className="flex items-center gap-2">
              <Checkbox checked={image !== null && includeImage} disabled={!image} onCheckedChange={setIncludeImage} />
              <span>Screenshot</span>
            </label>
            {checklist.map((item) => (
              <label key={item.key} className="flex items-center gap-2">
                <Checkbox
                  checked={sections[item.key]}
                  onCheckedChange={(checked) => setSections((s) => ({ ...s, [item.key]: checked }))}
                />
                <span>{item.label}</span>
                <span className="text-muted-foreground tabular-nums">{item.count}</span>
              </label>
            ))}
            <span className="text-micro text-muted-foreground">Environment and route are always included.</span>
          </div>
        </div>

        <div className="flex items-center gap-1">
          <Button size="xs" variant="ghost" onClick={() => void copyMarkdown()}>
            <Copy />
            Copy as Markdown
          </Button>
          <Button
            size="xs"
            className="ml-auto"
            disabled={!workspaceSlug || !title.trim() || submitting}
            onClick={() => void createIssue()}
            data-testid="dev-toolbar-report-create"
          >
            {submitting && <Loader2 className="animate-spin" />}
            Create issue
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
