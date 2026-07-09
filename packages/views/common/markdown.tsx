"use client";

import * as React from "react";
import {
  Markdown as MarkdownBase,
  type MarkdownProps as MarkdownBaseProps,
  type RenderMode,
  CodeBlock,
  isIssueIdentifier,
} from "@multica/ui/markdown";
import { useConfigStore } from "@multica/core/config";
import type { Attachment as AttachmentRecord } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { FileCode, FileText, Eye, Download } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { CodeBlockIframe } from "../editor/code-block-iframe";
import { IssueMentionCard } from "../issues/components/issue-mention-card";
import { useResolveIssueIdentifier } from "../issues/hooks";
import { ProjectChip } from "../projects/components/project-chip";
import { AppLink } from "../navigation";
import {
  Attachment as AttachmentRenderer,
  AttachmentDownloadProvider,
} from "../editor";
import { useT } from "../i18n";


export type { RenderMode };

export interface MarkdownProps extends MarkdownBaseProps {
  /**
   * Attachments associated with the surrounding entity (chat message, skill
   * file). When passed, the renderer resolves inline image / file-card URLs
   * to full attachment records via AttachmentDownloadProvider, unlocking the
   * unified hover toolbar / lightbox / preview-modal behavior used in
   * editor surfaces.
   */
  attachments?: AttachmentRecord[];
}

/**
 * Default renderMention that delegates to entity chips for issue/project mentions
 * and renders a styled span for other mention types.
 */
function ProjectMentionCard({ projectId }: { projectId: string }): React.ReactNode {
  const p = useWorkspacePaths();
  return (
    <AppLink href={p.projectDetail(projectId)} className="project-mention not-prose inline-flex">
      <ProjectChip
        projectId={projectId}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </AppLink>
  );
}

/**
 * Autolinked bare identifier (e.g. `MUL-123`) routed through
 * `mention://issue/<identifier>`. Resolves the identifier to a real issue in
 * the current workspace; renders a navigable chip on a hit, plain text on a
 * miss / while loading / cross-workspace.
 */
function AutolinkedIssueMention({ identifier }: { identifier: string }): React.ReactNode {
  const issue = useResolveIssueIdentifier(identifier);
  if (!issue) return identifier;
  return <IssueMentionCard issueId={issue.id} fallbackLabel={identifier} />;
}

function defaultRenderMention({
  type,
  id,
}: {
  type: string;
  id: string;
}): React.ReactNode {
  if (type === "issue") {
    // A bare identifier (from the autolink preprocessor) is carried as the id
    // segment; a real mention carries a UUID. Dispatch on the id shape.
    if (isIssueIdentifier(id)) {
      return <AutolinkedIssueMention identifier={id} />;
    }
    return <IssueMentionCard issueId={id} />;
  }
  if (type === "project") {
    return <ProjectMentionCard projectId={id} />;
  }
  return null;
}

function renderImage({ src, alt }: { src: string; alt: string }): React.ReactNode {
  return (
    <AttachmentRenderer
      attachment={{
        kind: "url",
        url: src,
        filename: alt,
        // chat / skill markdown `![]()` is structurally an image. Without
        // forceKind, empty/descriptive alt strings would route to the
        // file-card chrome via getPreviewKind autodetect.
        forceKind: "image",
      }}
    />
  );
}

function renderFileCard({
  href,
  filename,
}: {
  href: string;
  filename: string;
}): React.ReactNode {
  return (
    <AttachmentRenderer
      attachment={{ kind: "url", url: href, filename }}
    />
  );
}

function getLanguage(identifier: string, type: string): string {
  if (type) {
    const mimeLang = type.split("/")[1];
    if (mimeLang) {
      if (mimeLang === "x-python") return "python";
      if (
        mimeLang === "javascript" ||
        mimeLang === "typescript" ||
        mimeLang === "json" ||
        mimeLang === "markdown" ||
        mimeLang === "html" ||
        mimeLang === "css"
      ) {
        return mimeLang;
      }
    }
  }
  if (identifier) {
    const ext = identifier.split(".").pop()?.toLowerCase();
    if (ext) {
      if (ext === "py") return "python";
      if (ext === "js") return "javascript";
      if (ext === "ts") return "typescript";
      if (ext === "tsx") return "tsx";
      if (ext === "jsx") return "jsx";
      if (ext === "html") return "html";
      if (ext === "css") return "css";
      if (ext === "json") return "json";
      if (ext === "md") return "markdown";
      if (ext === "sh" || ext === "bash") return "bash";
      return ext;
    }
  }
  return "text";
}

interface ArtifactCardProps {
  identifier: string;
  type: string;
  title: string;
  content: string;
}

function ArtifactCard({ identifier, type, title, content }: ArtifactCardProps) {
  const { t } = useT("editor");
  const [isPreviewOpen, setIsPreviewOpen] = React.useState(false);

  const handleDownload = () => {
    const blob = new Blob([content], { type: type || "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = identifier || "artifact";
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  const isHtml = type === "text/html" || identifier.endsWith(".html");
  const language = getLanguage(identifier, type);

  const getIcon = () => {
    if (isHtml || language !== "text") {
      return <FileCode className="size-6 text-indigo-500 dark:text-indigo-400" />;
    }
    return <FileText className="size-6 text-emerald-500 dark:text-emerald-400" />;
  };

  return (
    <div className="my-3 flex items-center justify-between rounded-xl border border-border bg-card/60 p-4 shadow-sm backdrop-blur-sm transition-all hover:bg-card hover:shadow-md dark:border-border/60">
      <div className="flex items-center gap-3 min-w-0">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-accent/50">
          {getIcon()}
        </div>
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold text-foreground leading-snug">
            {title || identifier}
          </p>
          <p className="truncate font-mono text-[11px] text-muted-foreground mt-0.5">
            {identifier}
          </p>
        </div>
      </div>

      <div className="flex items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={() => setIsPreviewOpen(true)}
          className="h-8 gap-1.5 px-3 text-xs font-medium hover:bg-accent/80 transition-colors"
        >
          <Eye className="size-3.5" />
          {t(($) => $.attachment.preview)}
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={handleDownload}
          className="size-8 text-muted-foreground hover:bg-accent/80 hover:text-foreground transition-all duration-200"
          title={t(($) => $.image.download)}
        >
          <Download className="size-4" />
        </Button>
      </div>

      <Dialog open={isPreviewOpen} onOpenChange={setIsPreviewOpen}>
        <DialogContent className="max-w-4xl w-[90vw] max-h-[85vh] flex flex-col p-6 gap-4">
          <div className="flex items-center justify-between border-b pb-3">
            <div>
              <DialogTitle className="text-lg font-bold text-foreground">
                {title || identifier}
              </DialogTitle>
              <p className="font-mono text-xs text-muted-foreground mt-1">
                {identifier} ({type || "text/plain"})
              </p>
            </div>
          </div>
          
          <div className="flex-1 overflow-auto rounded-lg border bg-muted/20 min-h-[400px]">
            {isHtml ? (
              <CodeBlockIframe
                html={content}
                title={title || identifier}
                className="border-0 bg-transparent h-[500px]"
              />
            ) : (
              <CodeBlock
                code={content}
                language={language}
                mode="full"
                className="border-0 mb-0 rounded-none bg-transparent"
              />
            )}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/**
 * App-level Markdown wrapper. Injects:
 *   - entity chips for issue/project mentions
 *   - cdnDomain from the config store (drives fileCard preprocessing)
 *   - unified <Attachment> as the image / file-card renderer
 *   - AttachmentDownloadProvider so url → record resolution works inside
 *     the injected <Attachment> components
 *   - renderArtifact to render interactive Artifact Cards
 */
export function Markdown(props: MarkdownProps): React.JSX.Element {
  const cdnDomain = useConfigStore((s) => s.cdnDomain);
  const { attachments, ...rest } = props;

  const renderArtifact = React.useCallback(
    (artifactProps: { identifier: string; type: string; title: string; content: string }) => (
      <ArtifactCard {...artifactProps} />
    ),
    []
  );

  return (
    <AttachmentDownloadProvider attachments={attachments}>
      <MarkdownBase
        renderMention={defaultRenderMention}
        renderImage={renderImage}
        renderFileCard={renderFileCard}
        renderArtifact={renderArtifact}
        cdnDomain={cdnDomain}
        autolinkIssueIdentifiers
        {...rest}
      />
    </AttachmentDownloadProvider>
  );
}

export const MemoizedMarkdown = React.memo(Markdown);
MemoizedMarkdown.displayName = "MemoizedMarkdown";
