// FIR-1590 → Collections: the per-folder "Access" affordance dropped into a
// folder row on the Documents/Notes (surface 'artifact') and Autopilots/Skills
// (surface 'entity') interfaces. Renders a key button that opens the
// FolderAccessEditor. Gated by cerebro_collections — renders nothing when off,
// so the folder interfaces are untouched until the flag ships.
import * as React from "react";
import { KeyRound } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { FolderAccessEditor } from "./folder-access-editor";
import type { GrantSurface } from "../types";

export function FolderAccessColumn({
  surface,
  folderId,
  folderName,
  className,
}: {
  surface: GrantSurface;
  folderId: string;
  folderName: string;
  className?: string;
}) {
  const enabled = useFeatureFlag("cerebro_collections");
  const [open, setOpen] = React.useState(false);

  if (!enabled) return null;

  return (
    <>
      <Button
        size="icon-sm"
        variant="ghost"
        aria-label="Access"
        title="Access"
        className={cn(
          "shrink-0 text-muted-foreground opacity-0 focus:opacity-100 group-hover:opacity-100",
          open && "opacity-100",
          className,
        )}
        onClick={(e) => {
          e.stopPropagation();
          setOpen(true);
        }}
      >
        <KeyRound className="size-3.5" />
      </Button>
      {open && (
        <FolderAccessEditor
          surface={surface}
          folderId={folderId}
          folderName={folderName}
          open={open}
          onOpenChange={setOpen}
        />
      )}
    </>
  );
}
