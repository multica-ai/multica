"use client";

import { useState, useRef } from "react";
import { ChevronRight, X as XIcon } from "lucide-react";
import { useCreateMemoryArtifact, MEMORY_KINDS, MEMORY_KIND_LABELS } from "@multica/core/memory";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import type { MemoryArtifactKind } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { ContentEditor, type ContentEditorRef, TitleEditor } from "../editor";
import { useNavigation } from "../navigation";

// Smaller-surface modal than CreateProjectModal — memory artifacts only
// require kind + title + content. Tags / anchor / parent_id are deferred
// to the detail page edit flow; this modal is the minimum viable create.
export function CreateMemoryArtifactModal({ onClose }: { onClose: () => void }) {
  const router = useNavigation();
  const workspace = useCurrentWorkspace();
  const wsPaths = useWorkspacePaths();
  const createArtifact = useCreateMemoryArtifact();

  const [kind, setKind] = useState<MemoryArtifactKind>("wiki_page");
  const [title, setTitle] = useState("");
  const contentRef = useRef<ContentEditorRef>(null);
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async () => {
    if (!title.trim() || submitting) return;
    setSubmitting(true);
    try {
      const created = await createArtifact.mutateAsync({
        kind,
        title: title.trim(),
        // Empty string is valid (server allows it) — pass through verbatim.
        content: contentRef.current?.getMarkdown() ?? "",
      });
      onClose();
      toast.success("Created");
      router.push(wsPaths.memoryDetail(created.id));
    } catch {
      toast.error("Failed to create artifact");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent
        showCloseButton={false}
        className={cn(
          "p-0 gap-0 flex flex-col overflow-hidden",
          "!top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2",
          "!max-w-2xl !w-full !h-96",
        )}
      >
        <DialogTitle className="sr-only">New Memory Artifact</DialogTitle>

        <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
          <div className="flex items-center gap-1.5 text-xs">
            <span className="text-muted-foreground">{workspace?.name}</span>
            <ChevronRight className="size-3 text-muted-foreground/50" />
            <span className="font-medium">New {MEMORY_KIND_LABELS[kind].toLowerCase()}</span>
          </div>
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  onClick={onClose}
                  className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                >
                  <XIcon className="size-4" />
                </button>
              }
            />
            <TooltipContent side="bottom">Close</TooltipContent>
          </Tooltip>
        </div>

        {/* Kind selector — segmented pills. Active fills with accent so
            the choice is obvious without taking up vertical space. */}
        <div className="flex items-center gap-1 px-5 pb-2 shrink-0">
          {MEMORY_KINDS.map((k) => (
            <button
              key={k}
              type="button"
              onClick={() => setKind(k)}
              className={cn(
                "rounded-full px-2.5 py-1 text-xs transition-colors cursor-pointer",
                kind === k
                  ? "bg-accent text-accent-foreground"
                  : "text-muted-foreground hover:bg-accent/40",
              )}
            >
              {MEMORY_KIND_LABELS[k]}
            </button>
          ))}
        </div>

        <div className="px-5 pb-2 shrink-0">
          <TitleEditor
            autoFocus
            placeholder="Title"
            className="text-lg font-semibold"
            onChange={setTitle}
            onSubmit={handleSubmit}
          />
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto px-5">
          <ContentEditor
            ref={contentRef}
            placeholder="Start writing... (markdown supported)"
            debounceMs={500}
          />
        </div>

        <div className="flex items-center justify-end gap-2 px-4 py-3 border-t shrink-0">
          <Button
            size="sm"
            onClick={handleSubmit}
            disabled={!title.trim() || submitting}
          >
            {submitting ? "Creating..." : "Create"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
